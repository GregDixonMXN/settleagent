package gateway_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/GregDixonMXN/settleagent/internal/actions"
	"github.com/GregDixonMXN/settleagent/internal/api"
	"github.com/GregDixonMXN/settleagent/internal/domain"
	"github.com/GregDixonMXN/settleagent/internal/gateway"
	"github.com/GregDixonMXN/settleagent/internal/policies"
	"github.com/GregDixonMXN/settleagent/internal/store"
)

func policySetup(t *testing.T) (*store.MemoryStore, *gateway.Service, domain.Agent, *domain.Transaction, string) {
	t.Helper()
	s := store.New()
	org, principal := s.SeedOrg("Policy Co")
	s.SetPolicies(org.ID, policies.DefaultSupportPolicies(org.ID))
	g := gateway.NewService(s, actions.DefaultRegistry())
	ag := s.CreateAgent(org.ID, principal.ID, "support-agent-14", "production", []string{"support"}, "hash")
	tx := s.CreateTxn(domain.Transaction{OrgID: org.ID, AgentID: ag.ID, PrincipalID: principal.ID, SessionID: "s", Objective: "o", Status: domain.TxnPlanning})
	return s, g, ag, tx, org.ID
}

func TestPolicyVersioningAndHistory(t *testing.T) {
	s, g, ag, tx, org := policySetup(t)
	ctx := context.Background()

	a, err := g.ProposeAction(ctx, org, tx.ID, ag.ID, "stripe", "refund", map[string]any{"amount_cents": 8000}, "pv-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.ExecuteAllowed(ctx, org, a.ID); err != nil {
		t.Fatal(err)
	}
	before := s.ReceiptsForTxn(org, tx.ID)
	if len(before) != 1 || before[0].PolicyVersion != 1 {
		t.Fatalf("receipt should stamp version 1: %+v", before)
	}

	// New version denying everything stripe; history must not rewrite.
	denyAll := []domain.PolicyRule{{
		ID: "deny-all", OrgID: org, Name: "deny-all", Priority: 1,
		Match:  domain.PolicyMatch{Tool: "stripe"},
		Effect: domain.EffectDeny, Explanation: "v2 freeze",
	}}
	set := s.CreatePolicySet(org, denyAll, "tester")
	if set.Version != 2 || set.Status != "draft" {
		t.Fatalf("set: %+v", set)
	}
	if !s.ActivatePolicySet(org, 2) {
		t.Fatal("activate failed")
	}
	sets := s.PolicySets(org)
	if len(sets) != 2 || sets[0].Status != "disabled" || sets[1].Status != "active" {
		t.Fatalf("sets: %+v", sets)
	}
	b, err := g.ProposeAction(ctx, org, tx.ID, ag.ID, "stripe", "refund", map[string]any{"amount_cents": 100}, "pv-2")
	if err != nil {
		t.Fatal(err)
	}
	if b.Status != "denied" || b.Decision.PolicyVersion != 2 {
		t.Fatalf("v2 not enforced: %+v", b.Decision)
	}
	after := s.ReceiptsForTxn(org, tx.ID)
	if after[0].PolicyVersion != 1 {
		t.Fatal("history rewritten by policy change")
	}
}

func TestSimulationIsSideEffectFree(t *testing.T) {
	s, g, ag, tx, org := policySetup(t)
	ctx := context.Background()
	actsBefore := len(s.ActionsForTxn(org, tx.ID))
	auditBefore := len(s.Audit(org, tx.ID, 1000))

	out, err := g.Simulate(ctx, org, ag.ID, "stripe", "refund", map[string]any{"amount_cents": 30000})
	if err != nil {
		t.Fatal(err)
	}
	dec := out["decision"].(domain.PolicyDecision)
	if dec.Effect != domain.EffectRequireApproval {
		t.Fatalf("simulate: %+v", dec)
	}
	if out["would_execute"] != false {
		t.Fatalf("simulate: %+v", out)
	}
	if len(s.ActionsForTxn(org, tx.ID)) != actsBefore || len(s.Audit(org, tx.ID, 1000)) != auditBefore {
		t.Fatal("simulation persisted side effects")
	}
	// Unknown agent still errors, not default-allows.
	if _, err := g.Simulate(ctx, org, "nope", "stripe", "refund", nil); err == nil {
		t.Fatal("simulate unknown agent allowed")
	}
}

func TestV2Conditions(t *testing.T) {
	s, g, ag, tx, org := policySetup(t)
	ctx := context.Background()

	now := time.Now().UTC()
	wide := &domain.TimeWindow{StartHour: 0, EndHour: 24}
	narrow := &domain.TimeWindow{StartHour: now.Hour(), EndHour: now.Hour()}
	_ = narrow
	// Window covering the current hour matches; empty hour never matches.
	outside := &domain.TimeWindow{StartHour: (now.Hour() + 2) % 24, EndHour: (now.Hour() + 3) % 24}
	rules := []domain.PolicyRule{
		{ID: "destr", OrgID: org, Name: "destr", Priority: 1,
			Match:  domain.PolicyMatch{Classifications: []string{"DESTRUCTIVE"}},
			Effect: domain.EffectDeny, Explanation: "no destructive"},
		{ID: "prod-only", OrgID: org, Name: "prod-only", Priority: 2,
			Match:  domain.PolicyMatch{Tool: "crm", Environments: []string{"staging"}},
			Effect: domain.EffectDeny, Explanation: "staging only rule never fires in prod"},
		{ID: "res", OrgID: org, Name: "res", Priority: 3,
			Match:  domain.PolicyMatch{Tool: "crm", ResourcePrefix: "cust:"},
			Effect: domain.EffectAllow, Explanation: "customer resources"},
		{ID: "hours", OrgID: org, Name: "hours", Priority: 4,
			Match:  domain.PolicyMatch{Tool: "email", TimeWindow: wide},
			Effect: domain.EffectAllow, Explanation: "business hours"},
		{ID: "never", OrgID: org, Name: "never", Priority: 5,
			Match:  domain.PolicyMatch{Tool: "http", TimeWindow: outside},
			Effect: domain.EffectAllow, Explanation: "unreachable window"},
	}
	s.SetPolicies(org, rules)

	prop := func(tool, action string, args map[string]any, key string) *domain.TxnAction {
		a, err := g.ProposeAction(ctx, org, tx.ID, ag.ID, tool, action, args, key)
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	// postgres.delete_database is DESTRUCTIVE+PRIVILEGED+IRREVERSIBLE.
	if a := prop("postgres", "delete_database", map[string]any{}, "c-1"); a.Status != "denied" {
		t.Fatalf("classification: %+v", a.Decision)
	}
	// Agent is in production; staging-only rule does not fire → default approval.
	if a := prop("crm", "update_record", map[string]any{}, "c-2"); a.Decision.Effect != domain.EffectRequireApproval {
		t.Fatalf("environment: %+v", a.Decision)
	}
	if a := prop("crm", "update_record", map[string]any{"resource": "cust:9182"}, "c-3"); a.Decision.Effect != domain.EffectAllow {
		t.Fatalf("resource: %+v", a.Decision)
	}
	if a := prop("email", "send", map[string]any{"to": "x"}, "c-4"); a.Decision.Effect != domain.EffectAllow {
		t.Fatalf("time wide: %+v", a.Decision)
	}
	if a := prop("http", "fetch", map[string]any{}, "c-5"); a.Decision.RuleID != "" {
		t.Fatalf("narrow window should not match any rule: %+v", a.Decision)
	}
}

func TestPolicyAPIAndChangeAudit(t *testing.T) {
	s, org, _, agentSecret, opSecret, agentID := authSetup(t)
	s.SetPolicies(org, policies.DefaultSupportPolicies(org))
	h := api.New(s).Handler()

	// Agents cannot draft policy.
	rec := doReq(t, h, "POST", "/v1/policies/sets", agentSecret, `{"rules":[]}`)
	if rec.Code != 403 {
		t.Fatalf("agent draft: %d", rec.Code)
	}
	// Draft then activate as operator.
	draft := `{"rules":[{"id":"t1","org_id":"` + org + `","name":"t1","priority":1,"match":{"tool":"email"},"effect":"ALLOW","explanation":"open"}]}`
	rec = doReq(t, h, "POST", "/v1/policies/sets", opSecret, draft)
	if rec.Code != 201 {
		t.Fatalf("draft: %d %s", rec.Code, rec.Body.String())
	}
	var set map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &set)
	if set["status"] != "draft" {
		t.Fatalf("not draft: %v", set)
	}
	rec = doReq(t, h, "POST", "/v1/policies/sets/2/activate", opSecret, "")
	if rec.Code != 200 {
		t.Fatalf("activate: %d %s", rec.Code, rec.Body.String())
	}
	// Simulation endpoint: decision without persistence.
	rec = doReq(t, h, "POST", "/v1/policies/evaluate", agentSecret,
		`{"agent_id":"`+agentID+`","tool":"email","action":"send","arguments":{}}`)
	if rec.Code != 200 {
		t.Fatalf("simulate: %d %s", rec.Code, rec.Body.String())
	}
	var sim map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &sim)
	if sim["would_execute"] != true {
		t.Fatalf("simulate: %v", sim)
	}
	// Change audit recorded both mutations.
	found := map[string]bool{}
	for _, e := range s.Audit(org, "", 100) {
		found[e.Type] = true
	}
	if !found["policy.set_created"] || !found["policy.activated"] {
		t.Fatalf("change audit missing: %v", found)
	}
}

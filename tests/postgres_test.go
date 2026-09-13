package gateway_test

import (
	"context"
	"os"
	"testing"

	"github.com/agentguard/agentguard/internal/actions"
	"github.com/agentguard/agentguard/internal/auth"
	"github.com/agentguard/agentguard/internal/domain"
	"github.com/agentguard/agentguard/internal/gateway"
	"github.com/agentguard/agentguard/internal/policies"
	"github.com/agentguard/agentguard/internal/store"
)

func pgURL() string {
	if u := os.Getenv("TEST_DATABASE_URL"); u != "" {
		return u
	}
	return "postgres://agentguard:agentguard@127.0.0.1:15433/agentguard?sslmode=disable"
}

// TestPostgresBackend runs the core invariants against the real PG store:
// allow/deny/approval-gate, idempotent replay, receipt-chain verification,
// tenant isolation, and crash recovery reporting.
func TestPostgresBackend(t *testing.T) {
	ctx := context.Background()
	pg, err := store.Open(ctx, pgURL())
	if err != nil {
		t.Skipf("no postgres available: %v", err)
	}
	defer pg.Close()
	if err := store.Migrate(ctx, pg.Pool()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	org, principal := pg.SeedOrg("PG Test Co")
	pg.SetPolicies(org.ID, policies.DefaultSupportPolicies(org.ID))
	g := gateway.NewService(pg, actions.DefaultRegistry())
	ag := pg.CreateAgent(org.ID, principal.ID, "support-agent-14", "production", []string{"support"}, "hash")
	tx := pg.CreateTxn(domain.Transaction{OrgID: org.ID, AgentID: ag.ID, PrincipalID: principal.ID, SessionID: "s1", Objective: "o", Status: domain.TxnPlanning})

	// $80 refund allowed and executable.
	a80, err := g.ProposeAction(ctx, org.ID, tx.ID, ag.ID, "stripe", "refund", map[string]any{"amount_cents": 8000}, "pg-ref80")
	if err != nil {
		t.Fatal(err)
	}
	if a80.Decision.Effect != domain.EffectAllow {
		t.Fatalf("expected ALLOW got %v", a80.Decision)
	}
	if _, err := g.ExecuteAllowed(ctx, org.ID, a80.ID); err != nil {
		t.Fatalf("execute: %v", err)
	}

	// Idempotent replay returns the same executed action, no duplicate.
	dup, isDup := pg.AddAction(domain.TxnAction{OrgID: org.ID, TransactionID: tx.ID, Tool: "stripe", Action: "refund", Arguments: map[string]any{"amount_cents": 8000}, IdempotencyKey: "pg-ref80", Status: "allowed"})
	if !isDup || dup.ID != a80.ID {
		t.Fatal("idempotency replay did not return original action")
	}

	// $2000 denied and never executable.
	aBig, _ := g.ProposeAction(ctx, org.ID, tx.ID, ag.ID, "stripe", "refund", map[string]any{"amount_cents": 200000}, "pg-ref2000")
	if aBig.Decision.Effect != domain.EffectDeny {
		t.Fatalf("expected DENY got %v", aBig.Decision)
	}
	if _, err := g.ExecuteAllowed(ctx, org.ID, aBig.ID); err == nil {
		t.Fatal("denied action executed")
	}

	// Approval gate.
	aMid, _ := g.ProposeAction(ctx, org.ID, tx.ID, ag.ID, "stripe", "refund", map[string]any{"amount_cents": 30000}, "pg-ref300")
	if aMid.Decision.Effect != domain.EffectRequireApproval {
		t.Fatalf("expected REQUIRE_APPROVAL got %v", aMid.Decision)
	}
	if _, err := g.ExecuteAllowed(ctx, org.ID, aMid.ID); err == nil {
		t.Fatal("executed before approval")
	}

	// Receipt chain verifies after round-trip through PG timestamptz.
	if idx := pg.VerifyChain(org.ID); idx != -1 {
		t.Fatalf("chain broken at %d", idx)
	}

	// Tenant isolation.
	o2, _ := pg.SeedOrg("Other Co")
	if _, ok := pg.GetTxn(o2.ID, tx.ID); ok {
		t.Fatal("cross-tenant read")
	}

	// Recovery reports the still-open transaction.
	counts := pg.Recover()
	if counts["EXECUTING"]+counts["PLANNING"]+counts["AWAITING_APPROVAL"] == 0 {
		t.Fatalf("recovery missed open txn: %v", counts)
	}

	// Authority grants round-trip (migration 006).
	max := int64(10000)
	gr := pg.CreateGrant(domain.AuthorityGrant{OrgID: org.ID, AgentID: ag.ID,
		Scope: []string{"stripe.refund"}, Constraints: domain.GrantConstraints{MaxAmountCents: &max}})
	if gr.ID == "" {
		t.Fatal("grant not persisted")
	}
	list := pg.GrantsForAgent(org.ID, ag.ID)
	if len(list) != 1 || list[0].Constraints.MaxAmountCents == nil || *list[0].Constraints.MaxAmountCents != 10000 {
		t.Fatalf("grant round-trip: %+v", list)
	}
	if !pg.RevokeGrant(org.ID, gr.ID) {
		t.Fatal("revoke failed")
	}
	// Revoked credential stops authenticating.
	kid, _, hash, err := auth.NewAgentSecret()
	if err != nil {
		t.Fatal(err)
	}
	pg.StoreCredential(org.ID, ag.ID, kid, hash)
	if _, _, _, ok := pg.GetCredential(kid); !ok {
		t.Fatal("credential lookup failed")
	}
	if !pg.RevokeCredential(kid) {
		t.Fatal("credential revoke failed")
	}
	if _, _, _, ok := pg.GetCredential(kid); ok {
		t.Fatal("revoked credential still valid")
	}

	// Versioned policy sets (migration 008): draft, activate, history kept.
	v2 := pg.CreatePolicySet(org.ID, []domain.PolicyRule{{
		Name: "v2", Priority: 1, Match: domain.PolicyMatch{Tool: "email"},
		Effect: domain.EffectDeny, Explanation: "v2",
	}}, "tester")
	if v2.Version < 2 || v2.Status != "draft" {
		t.Fatalf("set: %+v", v2)
	}
	if !pg.ActivatePolicySet(org.ID, v2.Version) {
		t.Fatal("activate failed")
	}
	active, ok := pg.ActivePolicySet(org.ID)
	if !ok || active.Version != v2.Version {
		t.Fatalf("active: %+v", active)
	}
	if n := len(pg.PolicySets(org.ID)); n < 2 {
		t.Fatalf("history lost: %d sets", n)
	}
}

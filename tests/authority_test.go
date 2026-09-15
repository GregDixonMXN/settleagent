package gateway_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/settleagent/settleagent/internal/api"
	"github.com/settleagent/settleagent/internal/domain"
	"github.com/settleagent/settleagent/internal/policies"
)

func TestAuthorityEnforcement(t *testing.T) {
	s, org, _, agentSecret, opSecret, agentID := authSetup(t)
	s.SetPolicies(org, policies.DefaultSupportPolicies(org))
	h := api.New(s).Handler()

	mkTxn := func() string {
		rec := doReq(t, h, "POST", "/v1/transactions", agentSecret,
			`{"agent_id":"`+agentID+`","principal_id":"p","session_id":"s","objective":"o"}`)
		if rec.Code != 201 {
			t.Fatalf("txn: %d", rec.Code)
		}
		var txn map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &txn)
		return txn["id"].(string)
	}
	propose := func(tx, tool, action, args, key string) map[string]any {
		rec := doReq(t, h, "POST", "/v1/actions", agentSecret,
			`{"transaction_id":"`+tx+`","agent_id":"`+agentID+`","tool":"`+tool+`","action":"`+action+`","arguments":`+args+`,"idempotency_key":"`+key+`"}`)
		if rec.Code != 201 {
			t.Fatalf("propose: %d %s", rec.Code, rec.Body.String())
		}
		var a map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &a)
		return a
	}

	// No grants: policy-only mode, reason ALLOWED.
	a := propose(mkTxn(), "stripe", "refund", `{"amount_cents":8000}`, "g-1")
	dec := a["decision"].(map[string]any)
	if dec["effect"] != "ALLOW" || dec["reason_code"] != "ALLOWED" {
		t.Fatalf("policy-only: %v", dec)
	}

	// Grant capped at $100: $80 covered, $300 exceeds authority (policy
	// would only require approval — authority denies first).
	max := int64(10000)
	s.CreateGrant(domain.AuthorityGrant{OrgID: org, AgentID: agentID,
		Scope: []string{"stripe.refund", "crm.*"}, Constraints: domain.GrantConstraints{MaxAmountCents: &max}})
	a = propose(mkTxn(), "stripe", "refund", `{"amount_cents":8000}`, "g-2")
	if a["status"] != "allowed" {
		t.Fatalf("covered: %v", a)
	}
	a = propose(mkTxn(), "stripe", "refund", `{"amount_cents":30000}`, "g-3")
	dec = a["decision"].(map[string]any)
	if a["status"] != "denied" || dec["reason_code"] != "AUTHORITY_EXCEEDED" {
		t.Fatalf("over-cap: %v", dec)
	}
	if !containsStr(dec["explanation"].(string), "not authorized") {
		t.Fatalf("why not human: %v", dec)
	}

	// Out-of-scope tool denied even though policy would allow reads.
	a = propose(mkTxn(), "email", "send", `{"to":"x"}`, "g-4")
	if a["status"] != "denied" {
		t.Fatalf("out-of-scope: %v", a)
	}

	// Revoke the grant: everything denies (fail closed once adopted).
	for _, g := range s.GrantsForAgent(org, agentID) {
		s.RevokeGrant(org, g.ID)
	}
	a = propose(mkTxn(), "stripe", "refund", `{"amount_cents":100}`, "g-5")
	if a["status"] != "denied" {
		t.Fatalf("revoked: %v", a)
	}

	// Expired grant alone also denies.
	past := time.Now().UTC().Add(-time.Hour)
	s.CreateGrant(domain.AuthorityGrant{OrgID: org, AgentID: agentID,
		Scope: []string{"*"}, ExpiresAt: &past})
	a = propose(mkTxn(), "stripe", "refund", `{"amount_cents":100}`, "g-6")
	if a["status"] != "denied" {
		t.Fatalf("expired: %v", a)
	}
	_ = opSecret
}

func TestGrantAPIAndCredentialLifecycle(t *testing.T) {
	s, _, _, agentSecret, opSecret, agentID := authSetup(t)
	h := api.New(s).Handler()

	// Agent cannot create grants.
	rec := doReq(t, h, "POST", "/v1/grants", agentSecret,
		`{"agent_id":"`+agentID+`","scope":["crm.*"]}`)
	if rec.Code != 403 {
		t.Fatalf("agent grant create: %d", rec.Code)
	}
	// Operator creates, agent lists own, revoke works.
	rec = doReq(t, h, "POST", "/v1/grants", opSecret,
		`{"agent_id":"`+agentID+`","scope":["crm.*"],"constraints":{"max_amount_cents":5000}}`)
	if rec.Code != 201 {
		t.Fatalf("grant create: %d %s", rec.Code, rec.Body.String())
	}
	var g map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &g)
	rec = doReq(t, h, "GET", "/v1/grants?agent_id="+agentID, agentSecret, "")
	if rec.Code != 200 {
		t.Fatalf("list own: %d", rec.Code)
	}
	rec = doReq(t, h, "POST", "/v1/grants/"+g["id"].(string)+"/revoke", opSecret, "")
	if rec.Code != 200 {
		t.Fatalf("revoke: %d %s", rec.Code, rec.Body.String())
	}

	// Rotation issues a working second credential.
	rec = doReq(t, h, "POST", "/v1/agents/"+agentID+"/credentials", opSecret, "")
	if rec.Code != 201 {
		t.Fatalf("rotate: %d %s", rec.Code, rec.Body.String())
	}
	var rot struct {
		KeyID  string `json:"key_id"`
		Secret string `json:"api_secret"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &rot)
	rec = doReq(t, h, "GET", "/v1/transactions", rot.Secret, "")
	if rec.Code != 200 {
		t.Fatalf("new credential: %d", rec.Code)
	}
	// Revocation kills it.
	rec = doReq(t, h, "DELETE", "/v1/credentials/"+rot.KeyID, opSecret, "")
	if rec.Code != 200 {
		t.Fatalf("revoke cred: %d %s", rec.Code, rec.Body.String())
	}
	rec = doReq(t, h, "GET", "/v1/transactions", rot.Secret, "")
	if rec.Code != 401 {
		t.Fatalf("revoked still works: %d", rec.Code)
	}
}

func containsStr(hay, needle string) bool {
	return len(hay) >= len(needle) && (func() bool {
		for i := 0; i+len(needle) <= len(hay); i++ {
			if hay[i:i+len(needle)] == needle {
				return true
			}
		}
		return false
	}())
}

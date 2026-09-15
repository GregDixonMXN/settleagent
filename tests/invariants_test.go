package gateway_test

import (
	"context"
	"testing"

	"github.com/settleagent/settleagent/internal/actions"
	"github.com/settleagent/settleagent/internal/domain"
	"github.com/settleagent/settleagent/internal/gateway"
	"github.com/settleagent/settleagent/internal/policies"
	"github.com/settleagent/settleagent/internal/store"
	"github.com/settleagent/settleagent/internal/transactions"
)

func setup(t *testing.T) (*store.MemoryStore, *gateway.Service, domain.Agent, domain.Transaction) {
	t.Helper()
	s := store.New()
	org, principal := s.SeedOrg("Acme")
	s.SetPolicies(org.ID, policies.DefaultSupportPolicies(org.ID))
	a := s.CreateAgent(org.ID, principal.ID, "support-agent-14", "production", []string{"support"}, "hash")
	tx := s.CreateTxn(domain.Transaction{OrgID: org.ID, AgentID: a.ID, PrincipalID: principal.ID, SessionID: "sess_1", Objective: "resolve_ticket_9182", Status: domain.TxnPlanning})
	return s, gateway.NewService(s, actions.DefaultRegistry()), a, *tx
}

func TestDeniedNeverExecutes(t *testing.T) {
	s, g, ag, tx := setup(t)
	ctx := context.Background()
	a, err := g.ProposeAction(ctx, tx.OrgID, tx.ID, ag.ID, "stripe", "refund", map[string]any{"amount_cents": 200000}, "k-deny-1")
	if err != nil {
		t.Fatal(err)
	}
	if a.Decision.Effect != domain.EffectDeny {
		t.Fatalf("expected DENY got %v", a.Decision)
	}
	if _, err := g.ExecuteAllowed(ctx, tx.OrgID, a.ID); err == nil {
		t.Fatal("denied action executed")
	}
	_ = s
}

func TestApprovalGate(t *testing.T) {
	s, g, ag, tx := setup(t)
	ctx := context.Background()
	a, _ := g.ProposeAction(ctx, tx.OrgID, tx.ID, ag.ID, "stripe", "refund", map[string]any{"amount_cents": 30000}, "k-appr-1")
	if a.Decision.Effect != domain.EffectRequireApproval {
		t.Fatalf("expected REQUIRE_APPROVAL got %v", a.Decision)
	}
	if _, err := g.ExecuteAllowed(ctx, tx.OrgID, a.ID); err == nil {
		t.Fatal("executed before approval")
	}
	pend := s.PendingApprovals(tx.OrgID)
	if len(pend) != 1 {
		t.Fatalf("expected 1 approval, got %d", len(pend))
	}
	ap, ok := s.DecideApproval(tx.OrgID, pend[0].ID, "manager", true)
	if !ok || ap.Status != "approved" {
		t.Fatal("approval failed")
	}
	aa, _ := s.GetAction(tx.OrgID, a.ID)
	aa.Status = "approved"
	s.UpdateAction(aa)
	if _, err := g.RunApproved(ctx, tx.OrgID, a.ID); err != nil {
		t.Fatalf("approved run failed: %v", err)
	}
}

func TestIdempotencyNoDoubleExecute(t *testing.T) {
	_, g, ag, tx := setup(t)
	ctx := context.Background()
	a1, _ := g.ProposeAction(ctx, tx.OrgID, tx.ID, ag.ID, "crm", "update_record", map[string]any{"ticket": 9182}, "k-idem-1")
	a2, _ := g.ProposeAction(ctx, tx.OrgID, tx.ID, ag.ID, "crm", "update_record", map[string]any{"ticket": 9182}, "k-idem-1")
	if a1.ID != a2.ID {
		t.Fatal("idempotency key returned different action")
	}
	if _, err := g.ExecuteAllowed(ctx, tx.OrgID, a1.ID); err != nil {
		t.Fatal(err)
	}
	r1, _ := g.ExecuteAllowed(ctx, tx.OrgID, a1.ID)
	if r1.Status != "executed" {
		t.Fatal("replay should return executed")
	}
}

func TestIdempotencyScopedToTransaction(t *testing.T) {
	s, _, ag, tx := setup(t)
	// Same key in a NEW transaction is a different action, never a replay.
	tx2 := s.CreateTxn(domain.Transaction{OrgID: tx.OrgID, AgentID: ag.ID, PrincipalID: tx.PrincipalID, SessionID: "s2", Objective: "o2", Status: domain.TxnPlanning})
	base := domain.TxnAction{OrgID: tx.OrgID, Tool: "crm", Action: "update_record", Arguments: map[string]any{"x": 1}, IdempotencyKey: "k-scope-1", Status: "allowed"}
	first := base
	first.TransactionID = tx.ID
	a1, dup1 := s.AddAction(first)
	second := base
	second.TransactionID = tx2.ID
	a2, dup2 := s.AddAction(second)
	if dup1 || dup2 || a1.ID == a2.ID {
		t.Fatal("same key across transactions must create distinct actions")
	}
	if a1.TransactionID != tx.ID || a2.TransactionID != tx2.ID {
		t.Fatal("replay leaked across transaction boundary")
	}
}

func TestTerminalStates(t *testing.T) {
	tx := domain.Transaction{Status: domain.TxnCommitted}
	if transactions.CanTransition(tx.Status, domain.TxnExecuting) {
		t.Fatal("committed returned to executing")
	}
	ir := domain.TxnAction{Classes: []domain.ActionClass{domain.ClassIrreversible}}
	if actions.IsCompensable(ir) {
		t.Fatal("irreversible claimed compensable")
	}
}

func TestCommitRequiresResolvedActions(t *testing.T) {
	s, g, ag, tx := setup(t)
	ctx := context.Background()
	// PLANNING with no execution path cannot jump to COMMITTED.
	if _, err := g.Commit(ctx, tx.OrgID, tx.ID); err == nil {
		t.Fatal("committed from PLANNING without execution")
	}
	tx2 := s.CreateTxn(domain.Transaction{OrgID: tx.OrgID, AgentID: ag.ID, PrincipalID: tx.PrincipalID, SessionID: "c", Objective: "c", Status: domain.TxnPlanning})
	a, err := g.ProposeAction(ctx, tx.OrgID, tx2.ID, ag.ID, "stripe", "refund", map[string]any{"amount_cents": 8000}, "c-1")
	if err != nil {
		t.Fatal(err)
	}
	// Allowed but unexecuted blocks commit.
	if _, err := g.Commit(ctx, tx.OrgID, tx2.ID); err == nil {
		t.Fatal("committed with unexecuted action")
	}
	if _, err := g.ExecuteAllowed(ctx, tx.OrgID, a.ID); err != nil {
		t.Fatal(err)
	}
	done, err := g.Commit(ctx, tx.OrgID, tx2.ID)
	if err != nil {
		t.Fatalf("commit: %v", err)
	}
	if done.Status != domain.TxnCommitted {
		t.Fatalf("status %s", done.Status)
	}
	// Committed is terminal.
	if _, err := g.Commit(ctx, tx.OrgID, tx2.ID); err == nil {
		t.Fatal("re-commit allowed")
	}
}

func TestReceiptChainVerifies(t *testing.T) {
	s, g, ag, tx := setup(t)
	ctx := context.Background()
	a, _ := g.ProposeAction(ctx, tx.OrgID, tx.ID, ag.ID, "crm", "update_record", map[string]any{"x": 1}, "k-chain-1")
	if _, err := g.ExecuteAllowed(ctx, tx.OrgID, a.ID); err != nil {
		t.Fatal(err)
	}
	a2, _ := g.ProposeAction(ctx, tx.OrgID, tx.ID, ag.ID, "stripe", "refund", map[string]any{"amount_cents": 8000}, "k-chain-2")
	if _, err := g.ExecuteAllowed(ctx, tx.OrgID, a2.ID); err != nil {
		t.Fatal(err)
	}
	if idx := s.VerifyChain(tx.OrgID); idx != -1 {
		t.Fatalf("chain broken at %d", idx)
	}
}

func TestTenantIsolation(t *testing.T) {
	s, _, _, tx := setup(t)
	o2, _ := s.SeedOrg("Other")
	if _, ok := s.GetTxn(o2.ID, tx.ID); ok {
		t.Fatal("cross-tenant read")
	}
}

func TestCompensationPartialOnIrreversible(t *testing.T) {
	s, g, ag, tx := setup(t)
	ctx := context.Background()
	a, _ := g.ProposeAction(ctx, tx.OrgID, tx.ID, ag.ID, "crm", "update_record", map[string]any{"x": 1}, "k-comp-1")
	if _, err := g.ExecuteAllowed(ctx, tx.OrgID, a.ID); err != nil {
		t.Fatal(err)
	}
	// Force an irreversible executed action directly (simulates email sent before failure).
	e, _ := g.ProposeAction(ctx, tx.OrgID, tx.ID, ag.ID, "email", "send", map[string]any{"to": "c@x.com"}, "k-comp-2")
	ea, _ := s.GetAction(tx.OrgID, e.ID)
	ea.Status = "approved"
	s.UpdateAction(ea)
	if _, err := g.RunApproved(ctx, tx.OrgID, e.ID); err != nil {
		t.Fatal(err)
	}
	final, _ := g.CompensateTransaction(ctx, tx.OrgID, tx.ID)
	if final != domain.TxnPartiallyCompensated {
		t.Fatalf("expected PARTIALLY_COMPENSATED got %s", final)
	}
	_ = s
}

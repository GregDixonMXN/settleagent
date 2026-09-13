package gateway_test

import (
	"context"
	"encoding/hex"
	"errors"
	"testing"

	"github.com/agentguard/agentguard/internal/actions"
	"github.com/agentguard/agentguard/internal/domain"
	"github.com/agentguard/agentguard/internal/gateway"
	"github.com/agentguard/agentguard/internal/keys"
	"github.com/agentguard/agentguard/internal/policies"
	"github.com/agentguard/agentguard/internal/store"
)

func reconcileSetup(t *testing.T) (*store.MemoryStore, *gateway.Service, domain.Agent, *domain.Transaction) {
	t.Helper()
	s := store.New()
	org, principal := s.SeedOrg("Reconcile Co")
	s.SetPolicies(org.ID, policies.DefaultSupportPolicies(org.ID))
	g := gateway.NewService(s, actions.DefaultRegistry())
	g.Tools().Register("probe", "ping", []domain.ActionClass{domain.ClassReadOnly},
		func(ctx context.Context, a domain.TxnAction) (map[string]any, error) {
			return nil, actions.Uncertain(errors.New("connection lost after send"))
		}, nil)
	ag := s.CreateAgent(org.ID, principal.ID, "probe-agent", "production", []string{"support"}, "hash")
	tx := s.CreateTxn(domain.Transaction{OrgID: org.ID, AgentID: ag.ID, PrincipalID: principal.ID, SessionID: "s", Objective: "o", Status: domain.TxnPlanning})
	return s, g, ag, tx
}

func TestUncertainParksForReconciliation(t *testing.T) {
	s, g, ag, tx := reconcileSetup(t)
	ctx := context.Background()
	a, err := g.ProposeAction(ctx, tx.OrgID, tx.ID, ag.ID, "probe", "ping", map[string]any{}, "rec-1")
	if err != nil {
		t.Fatal(err)
	}
	out, err := g.ExecuteAllowed(ctx, tx.OrgID, a.ID)
	if err == nil {
		t.Fatal("expected uncertain error")
	}
	var ue *actions.UncertainError
	if !errors.As(err, &ue) {
		t.Fatalf("wrong error type: %T", err)
	}
	if out.Status != "unknown" {
		t.Fatalf("status %s, want unknown", out.Status)
	}
	if len(s.ReceiptsForTxn(tx.OrgID, tx.ID)) != 0 {
		t.Fatal("unknown execution must not produce a receipt")
	}
	// Retry without reconciling is refused.
	if _, err := g.ExecuteAllowed(ctx, tx.OrgID, a.ID); err == nil {
		t.Fatal("blind retry allowed")
	}
	// No reconciler registered: stays unknown, visibly.
	if _, err := g.Reconcile(ctx, tx.OrgID, a.ID); err == nil {
		t.Fatal("reconcile without reconciler succeeded")
	}
	stored, _ := s.GetAction(tx.OrgID, a.ID)
	if stored.Status != "unknown" {
		t.Fatal("action abandoned or moved without evidence")
	}
}

func TestReconcileConfirmedAndAbsent(t *testing.T) {
	s, g, ag, tx := reconcileSetup(t)
	ctx := context.Background()

	g.Tools().RegisterReconciler("probe", "ping",
		func(ctx context.Context, a domain.TxnAction) (bool, map[string]any, error) {
			return true, map[string]any{"confirmed": "upstream-log-123"}, nil
		})
	a, _ := g.ProposeAction(ctx, tx.OrgID, tx.ID, ag.ID, "probe", "ping", map[string]any{}, "rec-2")
	if _, err := g.ExecuteAllowed(ctx, tx.OrgID, a.ID); err == nil {
		t.Fatal("expected uncertain")
	}
	done, err := g.Reconcile(ctx, tx.OrgID, a.ID)
	if err != nil {
		t.Fatalf("reconcile: %v", err)
	}
	if done.Status != "executed" {
		t.Fatalf("status %s", done.Status)
	}
	receipts := s.ReceiptsForTxn(tx.OrgID, tx.ID)
	if len(receipts) != 1 {
		t.Fatalf("confirmed execution needs exactly one receipt, got %d", len(receipts))
	}

	g.Tools().Register("probe", "pong", []domain.ActionClass{domain.ClassReadOnly},
		func(ctx context.Context, a domain.TxnAction) (map[string]any, error) {
			return nil, actions.Uncertain(errors.New("timeout"))
		}, nil)
	g.Tools().RegisterReconciler("probe", "pong",
		func(ctx context.Context, a domain.TxnAction) (bool, map[string]any, error) {
			return false, nil, nil
		})
	b, _ := g.ProposeAction(ctx, tx.OrgID, tx.ID, ag.ID, "probe", "pong", map[string]any{}, "rec-3")
	if _, err := g.ExecuteAllowed(ctx, tx.OrgID, b.ID); err == nil {
		t.Fatal("expected uncertain")
	}
	absent, err := g.Reconcile(ctx, tx.OrgID, b.ID)
	if err != nil {
		t.Fatalf("reconcile absent: %v", err)
	}
	if absent.Status != "failed" {
		t.Fatalf("absent upstream must become failed, got %s", absent.Status)
	}
}

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	b, err := hex.DecodeString(s)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestReceiptSignaturesVerify(t *testing.T) {
	s := store.New()
	org, principal := s.SeedOrg("Sig Co")
	s.SetPolicies(org.ID, policies.DefaultSupportPolicies(org.ID))
	g := gateway.NewService(s, actions.DefaultRegistry())
	signer, _, err := keys.Load()
	if err != nil {
		t.Fatal(err)
	}
	g.SetSigner(signer)
	ag := s.CreateAgent(org.ID, principal.ID, "a", "production", []string{"support"}, "hash")
	tx := s.CreateTxn(domain.Transaction{OrgID: org.ID, AgentID: ag.ID, PrincipalID: principal.ID, SessionID: "s", Objective: "o", Status: domain.TxnPlanning})
	ctx := context.Background()
	a, _ := g.ProposeAction(ctx, tx.OrgID, tx.ID, ag.ID, "crm", "lookup_customer", map[string]any{"c": 1}, "sig-1")
	if _, err := g.ExecuteAllowed(ctx, tx.OrgID, a.ID); err != nil {
		t.Fatal(err)
	}
	receipts := s.ReceiptsForTxn(tx.OrgID, tx.ID)
	if len(receipts) != 1 || receipts[0].Signature == "" || receipts[0].KeyID == "" {
		t.Fatalf("unsigned receipt: %+v", receipts)
	}
	if !signer.Verify([]byte(receipts[0].Hash), mustHex(t, receipts[0].Signature)) {
		t.Fatal("valid signature rejected")
	}
	tampered := receipts[0]
	tampered.Hash += "00"
	if signer.Verify([]byte(tampered.Hash), mustHex(t, receipts[0].Signature)) {
		t.Fatal("tampered hash verified")
	}
}

package gateway

import (
	"context"
	"fmt"
	"time"

	"github.com/agentguard/agentguard/internal/actions"
	"github.com/agentguard/agentguard/internal/domain"
	"github.com/agentguard/agentguard/internal/observe"
	"github.com/agentguard/agentguard/internal/policies"
	"github.com/agentguard/agentguard/internal/receipts"
	"github.com/agentguard/agentguard/internal/store"
	"github.com/agentguard/agentguard/internal/transactions"
)

type Service struct {
	store store.Store
	tools *actions.Registry
}

func NewService(s store.Store, t *actions.Registry) *Service {
	return &Service{store: s, tools: t}
}

func (g *Service) Store() store.Store { return g.store }

// ProposeAction evaluates policy BEFORE execution and persists the decision.
// Denied actions are recorded and never executed. Approval-gated actions
// create an approval and pause; execution requires DecideApproval + RunApproved.
func (g *Service) ProposeAction(ctx context.Context, orgID, txnID, agentID string, tool, action string, args map[string]any, idemKey string) (*domain.TxnAction, error) {
	t, ok := g.store.GetTxn(orgID, txnID)
	if !ok {
		return nil, fmt.Errorf("transaction not found")
	}
	if t.Status == domain.TxnCommitted || t.Status == domain.TxnRolledBack || t.Status == domain.TxnPartiallyCompensated || t.Status == domain.TxnFailed {
		return nil, fmt.Errorf("transaction is terminal (%s)", t.Status)
	}
	ag, ok := g.store.GetAgent(orgID, agentID)
	if !ok {
		return nil, fmt.Errorf("agent not found")
	}
	amt := extractCents(args)
	a := domain.TxnAction{
		OrgID: orgID, TransactionID: txnID, Tool: tool, Action: action,
		Arguments: args, Classes: g.tools.Classes(tool, action),
		AmountCents: amt, IdempotencyKey: idemKey, Status: "proposed",
	}
	stored, dup := g.store.AddAction(a)
	if dup {
		return stored, nil // idempotent replay: no duplicate side effects
	}
	decision := policies.Evaluate(g.store.Policies(orgID), policies.EvalInput{Agent: ag, Action: *stored})
	_, evalSpan := observe.Start(ctx, "policy.evaluate", map[string]string{
		"tool": stored.Tool, "action": stored.Action,
		"agent": ag.Name, "effect": string(decision.Effect),
	})
	evalSpan.End()
	stored.Decision = &decision
	switch decision.Effect {
	case domain.EffectDeny:
		stored.Status = "denied"
		g.store.UpdateAction(stored)
		g.store.Emit(domain.AuditEvent{OrgID: orgID, TransactionID: txnID, ActorType: "policy", ActorID: decision.RuleID, Type: "policy.evaluated", Payload: map[string]any{"effect": "DENY", "why": decision.Explanation}})
		g.store.Emit(domain.AuditEvent{OrgID: orgID, TransactionID: txnID, ActorType: "policy", ActorID: decision.RuleID, Type: "action.denied", Payload: map[string]any{"action_id": stored.ID}})
		return stored, nil
	case domain.EffectRequireApproval:
		stored.Status = "awaiting_approval"
		g.store.UpdateAction(stored)
		g.store.Emit(domain.AuditEvent{OrgID: orgID, TransactionID: txnID, ActorType: "policy", ActorID: decision.RuleID, Type: "policy.evaluated", Payload: map[string]any{"effect": "REQUIRE_APPROVAL", "why": decision.Explanation}})
		g.store.CreateApproval(domain.Approval{OrgID: orgID, TransactionID: txnID, ActionID: &stored.ID, RequestedBy: agentID, Reason: decision.Explanation, ExposureCents: amt})
		g.store.Emit(domain.AuditEvent{OrgID: orgID, TransactionID: txnID, ActorType: "system", ActorID: agentID, Type: "approval.requested", Payload: map[string]any{"action_id": stored.ID}})
		_ = transactions.MustTransition(t, domain.TxnAwaitingApproval)
		_ = g.store.SetTxnStatus(orgID, txnID, t.Status)
		return stored, nil
	default:
		stored.Status = "allowed"
		g.store.UpdateAction(stored)
		g.store.Emit(domain.AuditEvent{OrgID: orgID, TransactionID: txnID, ActorType: "policy", ActorID: decision.RuleID, Type: "policy.evaluated", Payload: map[string]any{"effect": string(decision.Effect), "why": decision.Explanation}})
		return stored, nil
	}
}

// ExecuteAllowed runs an ALLOW-decided action. Denied/approval-gated actions are refused.
func (g *Service) ExecuteAllowed(ctx context.Context, orgID, actionID string) (*domain.TxnAction, error) {
	a, ok := g.store.GetAction(orgID, actionID)
	if !ok {
		return nil, fmt.Errorf("action not found")
	}
	if a.Status == "executed" {
		return a, nil // idempotent: already ran under this key
	}
	if a.Decision == nil || (a.Decision.Effect != domain.EffectAllow && a.Decision.Effect != domain.EffectAllowWithConstraints) {
		return nil, fmt.Errorf("action %s is not executable (status=%s decision=%v): approval required or denied", a.ID, a.Status, a.Decision)
	}
	if a.Status != "allowed" && a.Status != "approved" {
		return nil, fmt.Errorf("action %s not in executable state (%s)", a.ID, a.Status)
	}
	return g.run(ctx, orgID, a)
}

// RunApproved executes an action whose approval was granted.
func (g *Service) RunApproved(ctx context.Context, orgID, actionID string) (*domain.TxnAction, error) {
	a, ok := g.store.GetAction(orgID, actionID)
	if !ok {
		return nil, fmt.Errorf("action not found")
	}
	if a.Status == "executed" {
		return a, nil
	}
	if a.Status != "approved" {
		return nil, fmt.Errorf("action %s has no granted approval (status=%s)", a.ID, a.Status)
	}
	return g.run(ctx, orgID, a)
}

func (g *Service) run(ctx context.Context, orgID string, a *domain.TxnAction) (*domain.TxnAction, error) {
	t, _ := g.store.GetTxn(orgID, a.TransactionID)
	if t != nil && (t.Status == domain.TxnCreated || t.Status == domain.TxnPlanning || t.Status == domain.TxnAwaitingApproval) {
		_ = g.store.SetTxnStatus(orgID, t.ID, domain.TxnExecuting)
	}
	start := time.Now().UTC()
	ctx, span := observe.Start(ctx, "action.execute", map[string]string{
		"tool": a.Tool, "action": a.Action, "txn": a.TransactionID,
	})
	defer span.End()
	traceID := observe.TraceID(ctx)
	g.store.Emit(domain.AuditEvent{OrgID: orgID, TransactionID: a.TransactionID, ActorType: "agent", ActorID: "", Type: "action.executing", Payload: map[string]any{"action_id": a.ID, "trace_id": traceID}})
	res, err := g.tools.Execute(ctx, *a)
	a.LatencyMS = time.Since(start).Milliseconds()
	if err != nil {
		a.Status = "failed"
		a.Error = err.Error()
		g.store.UpdateAction(a)
		span.RecordError(err)
		g.store.Emit(domain.AuditEvent{OrgID: orgID, TransactionID: a.TransactionID, ActorType: "tool", ActorID: a.Tool, Type: "action.failed", Payload: map[string]any{"action_id": a.ID, "error": err.Error(), "trace_id": traceID}})
		return a, err
	}
	a.Status = "executed"
	a.Result = res
	g.store.UpdateAction(a)
	end := time.Now().UTC()
	r := g.store.AppendReceipt(domain.Receipt{
		OrgID: orgID, TransactionID: a.TransactionID, ActionID: a.ID,
		AgentID: "", PrincipalID: "", Tool: a.Tool, Action: a.Action,
		ArgumentsHash: a.ArgumentsHash, Decision: a.Decision.Effect,
		ResultHash:     receipts.Canonical(res),
		FinancialCents: a.AmountCents, Compensation: "none",
		StartedAt: start, CompletedAt: end,
	})
	g.store.Emit(domain.AuditEvent{OrgID: orgID, TransactionID: a.TransactionID, ActorType: "tool", ActorID: a.Tool, Type: "action.executed", Payload: map[string]any{"action_id": a.ID, "receipt_id": r.ID, "trace_id": traceID}})
	return a, nil
}

// CompensateTransaction reverses compensable executed actions in reverse order.
// Irreversible actions are never claimed as rolled back; partial failures
// yield PARTIALLY_COMPENSATED with full evidence preserved.
func (g *Service) CompensateTransaction(ctx context.Context, orgID, txnID string) (domain.TxnStatus, error) {
	ctx, span := observe.Start(ctx, "transaction.compensate", map[string]string{"txn": txnID})
	defer span.End()
	t, ok := g.store.GetTxn(orgID, txnID)
	if !ok {
		return "", fmt.Errorf("transaction not found")
	}
	acts := g.store.ActionsForTxn(orgID, txnID)
	// reverse order
	for i, j := 0, len(acts)-1; i < j; i, j = i+1, j-1 {
		acts[i], acts[j] = acts[j], acts[i]
	}
	_ = g.store.SetTxnStatus(orgID, txnID, domain.TxnCompensating)
	g.store.Emit(domain.AuditEvent{OrgID: orgID, TransactionID: txnID, ActorType: "system", ActorID: "gateway", Type: "compensation.started"})
	failed := false
	for _, a := range acts {
		if a.Status != "executed" {
			continue
		}
		if actions.IsIrreversible(a) && !actions.IsCompensable(a) {
			g.store.Emit(domain.AuditEvent{OrgID: orgID, TransactionID: txnID, ActorType: "system", ActorID: "gateway", Type: "compensation.skipped_irreversible", Payload: map[string]any{"action_id": a.ID}})
			failed = true
			continue
		}
		if err := g.tools.Compensate(ctx, a); err != nil {
			failed = true
			ac, _ := g.store.GetAction(orgID, a.ID)
			ac.Status = "compensation_failed"
			g.store.UpdateAction(ac)
			g.store.Emit(domain.AuditEvent{OrgID: orgID, TransactionID: txnID, ActorType: "system", ActorID: "gateway", Type: "compensation.failed", Payload: map[string]any{"action_id": a.ID, "error": err.Error()}})
			continue
		}
		ac, _ := g.store.GetAction(orgID, a.ID)
		ac.Status = "compensated"
		g.store.UpdateAction(ac)
		g.store.Emit(domain.AuditEvent{OrgID: orgID, TransactionID: txnID, ActorType: "system", ActorID: "gateway", Type: "compensation.succeeded", Payload: map[string]any{"action_id": a.ID}})
	}
	final := domain.TxnRolledBack
	if failed {
		final = domain.TxnPartiallyCompensated
	}
	_ = g.store.SetTxnStatus(orgID, txnID, final)
	_ = t
	return final, nil
}

func extractCents(args map[string]any) *int64 {
	if args == nil {
		return nil
	}
	for _, k := range []string{"amount_cents", "amountCents"} {
		if v, ok := args[k]; ok {
			var n int64
			switch x := v.(type) {
			case int64:
				n = x
			case int:
				n = int64(x)
			case float64:
				n = int64(x)
			default:
				continue
			}
			return &n
		}
	}
	// dollars convenience: {"amount": 80} -> 8000
	if v, ok := args["amount"]; ok {
		switch x := v.(type) {
		case int:
			n := int64(x) * 100
			return &n
		case float64:
			n := int64(x * 100)
			return &n
		}
	}
	return nil
}

package gateway

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/agentguard/agentguard/internal/actions"
	"github.com/agentguard/agentguard/internal/domain"
	"github.com/agentguard/agentguard/internal/keys"
	"github.com/agentguard/agentguard/internal/observe"
	"github.com/agentguard/agentguard/internal/policies"
	"github.com/agentguard/agentguard/internal/receipts"
	"github.com/agentguard/agentguard/internal/store"
	"github.com/agentguard/agentguard/internal/transactions"
)

type Service struct {
	store  store.Store
	tools  *actions.Registry
	signer keys.Provider
}

func NewService(s store.Store, t *actions.Registry) *Service {
	return &Service{store: s, tools: t}
}

func (g *Service) Store() store.Store { return g.store }

// Tools exposes the tool registry for runtime registration (MCP proxy).
func (g *Service) Tools() *actions.Registry { return g.tools }

// SetSigner enables receipt signatures. Nil signer means unsigned receipts
// (chain hashes still apply).
func (g *Service) SetSigner(p keys.Provider) { g.signer = p }

func (g *Service) signReceipt(r *domain.Receipt) {
	if g.signer == nil {
		return
	}
	r.KeyID = g.signer.KeyID()
	r.Signature = hex.EncodeToString(g.signer.Sign([]byte(r.Hash)))
}

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
		// Idempotent replay: no duplicate side effects. Labeled in audit so
		// operators can distinguish replay from fresh execution.
		g.store.Emit(domain.AuditEvent{OrgID: orgID, TransactionID: txnID, ActorType: "system", ActorID: agentID, Type: "action.replayed", Payload: map[string]any{"action_id": stored.ID, "status": stored.Status}})
		return stored, nil
	}
	// Step 4 of authorization: delegated authority BEFORE policy.
	// Agents with no grants run policy-only (audit-logged migration path).
	covered, hasGrants, authWhy := CheckAuthority(g.store.GrantsForAgent(orgID, agentID), ag, tool, action, amt)
	if hasGrants {
		g.store.Emit(domain.AuditEvent{OrgID: orgID, TransactionID: txnID, ActorType: "authority", ActorID: agentID, Type: "authority.evaluated", Payload: map[string]any{"action_id": stored.ID, "covered": covered, "why": authWhy, "trace_id": observe.TraceID(ctx)}})
	}
	if hasGrants && !covered {
		denied := domain.PolicyDecision{Effect: domain.EffectDeny, Explanation: authWhy, ReasonCode: domain.ReasonAuthorityExceeded}
		stored.Decision = &denied
		stored.Status = "denied"
		g.store.UpdateAction(stored)
		g.store.Emit(domain.AuditEvent{OrgID: orgID, TransactionID: txnID, ActorType: "authority", ActorID: agentID, Type: "action.denied", Payload: map[string]any{"action_id": stored.ID, "reason_code": domain.ReasonAuthorityExceeded}})
		return stored, nil
	}
	rules, version := g.store.Policies(orgID), 0
	if set, ok := g.store.ActivePolicySet(orgID); ok {
		rules, version = set.Rules, set.Version
	}
	decision := policies.Evaluate(rules, version, policies.EvalInput{Agent: ag, Action: *stored})
	_, evalSpan := observe.Start(ctx, "policy.evaluate", map[string]string{
		"tool": stored.Tool, "action": stored.Action,
		"agent": ag.Name, "effect": string(decision.Effect),
	})
	evalSpan.End()
	switch decision.Effect {
	case domain.EffectDeny:
		decision.ReasonCode = domain.ReasonPolicyDenied
	case domain.EffectRequireApproval:
		decision.ReasonCode = domain.ReasonApprovalRequired
	default:
		decision.ReasonCode = domain.ReasonAllowed
	}
	stored.Decision = &decision
	switch decision.Effect {
	case domain.EffectDeny:
		stored.Status = "denied"
		g.store.UpdateAction(stored)
		g.store.Emit(domain.AuditEvent{OrgID: orgID, TransactionID: txnID, ActorType: "policy", ActorID: decision.RuleID, Type: "policy.evaluated", Payload: map[string]any{"effect": "DENY", "why": decision.Explanation, "reason_code": decision.ReasonCode}})
		g.store.Emit(domain.AuditEvent{OrgID: orgID, TransactionID: txnID, ActorType: "policy", ActorID: decision.RuleID, Type: "action.denied", Payload: map[string]any{"action_id": stored.ID}})
		return stored, nil
	case domain.EffectRequireApproval:
		stored.Status = "awaiting_approval"
		g.store.UpdateAction(stored)
		g.store.Emit(domain.AuditEvent{OrgID: orgID, TransactionID: txnID, ActorType: "policy", ActorID: decision.RuleID, Type: "policy.evaluated", Payload: map[string]any{"effect": "REQUIRE_APPROVAL", "why": decision.Explanation, "reason_code": decision.ReasonCode}})
		g.store.CreateApproval(domain.Approval{OrgID: orgID, TransactionID: txnID, ActionID: &stored.ID, RequestedBy: agentID, Reason: decision.Explanation, ExposureCents: amt})
		g.store.Emit(domain.AuditEvent{OrgID: orgID, TransactionID: txnID, ActorType: "system", ActorID: agentID, Type: "approval.requested", Payload: map[string]any{"action_id": stored.ID}})
		_ = transactions.MustTransition(t, domain.TxnAwaitingApproval)
		_ = g.store.SetTxnStatus(orgID, txnID, t.Status)
		return stored, nil
	default:
		stored.Status = "allowed"
		g.store.UpdateAction(stored)
		g.store.Emit(domain.AuditEvent{OrgID: orgID, TransactionID: txnID, ActorType: "policy", ActorID: decision.RuleID, Type: "policy.evaluated", Payload: map[string]any{"effect": string(decision.Effect), "why": decision.Explanation, "reason_code": decision.ReasonCode}})
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
	if a.Status == "unknown" {
		return nil, fmt.Errorf("action %s is in EXECUTION_UNKNOWN: reconcile it before retrying, or it may duplicate the side effect", a.ID)
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
		var ue *actions.UncertainError
		if errors.As(err, &ue) {
			// Side effects unconfirmed: park as unknown for reconciliation.
			// Never auto-retry — a retry could duplicate the side effect.
			a.Status = "unknown"
			a.Error = ue.Error()
			g.store.UpdateAction(a)
			span.RecordError(err)
			g.store.Emit(domain.AuditEvent{OrgID: orgID, TransactionID: a.TransactionID, ActorType: "tool", ActorID: a.Tool, Type: "action.unknown", Payload: map[string]any{"action_id": a.ID, "error": ue.Error(), "trace_id": traceID, "hint": "reconcile before retrying"}})
			return a, ue
		}
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
		PolicyVersion: a.Decision.PolicyVersion,
		StartedAt:     start, CompletedAt: end,
	})
	g.signReceipt(r)
	g.store.UpdateReceipt(r)
	g.store.Emit(domain.AuditEvent{OrgID: orgID, TransactionID: a.TransactionID, ActorType: "tool", ActorID: a.Tool, Type: "action.executed", Payload: map[string]any{"action_id": a.ID, "receipt_id": r.ID, "trace_id": traceID}})
	return a, nil
}

// Reconcile settles an uncertain action by asking the provider what really
// happened (keyed by idempotency key). Confirmed executions get a receipt;
// confirmed absences become failed. Unresolvable stays unknown — visible,
// never silently abandoned or blindly retried.
func (g *Service) Reconcile(ctx context.Context, orgID, actionID string) (*domain.TxnAction, error) {
	ctx, span := observe.Start(ctx, "action.reconcile", map[string]string{"action": actionID})
	defer span.End()
	a, ok := g.store.GetAction(orgID, actionID)
	if !ok {
		return nil, fmt.Errorf("action not found")
	}
	if a.Status != "unknown" {
		return nil, fmt.Errorf("action %s is %s, nothing to reconcile", a.ID, a.Status)
	}
	rec, ok := g.tools.ReconcilerFor(a.Tool, a.Action)
	if !ok {
		g.store.Emit(domain.AuditEvent{OrgID: orgID, TransactionID: a.TransactionID, ActorType: "system", ActorID: "gateway", Type: "reconcile.no_reconciler", Payload: map[string]any{"action_id": a.ID, "tool": a.Tool}})
		return nil, fmt.Errorf("no reconciler for %s.%s; action remains unknown", a.Tool, a.Action)
	}
	found, result, err := rec(ctx, *a)
	if err != nil {
		span.RecordError(err)
		g.store.Emit(domain.AuditEvent{OrgID: orgID, TransactionID: a.TransactionID, ActorType: "tool", ActorID: a.Tool, Type: "reconcile.error", Payload: map[string]any{"action_id": a.ID, "error": err.Error()}})
		return nil, fmt.Errorf("reconciliation failed: %w", err)
	}
	traceID := observe.TraceID(ctx)
	if !found {
		a.Status = "failed"
		a.Error = "reconciliation confirmed the action never executed upstream"
		g.store.UpdateAction(a)
		g.store.Emit(domain.AuditEvent{OrgID: orgID, TransactionID: a.TransactionID, ActorType: "tool", ActorID: a.Tool, Type: "action.reconciled", Payload: map[string]any{"action_id": a.ID, "outcome": "absent", "trace_id": traceID}})
		return a, nil
	}
	now := time.Now().UTC()
	a.Status = "executed"
	a.Result = result
	a.Error = ""
	g.store.UpdateAction(a)
	r := g.store.AppendReceipt(domain.Receipt{
		OrgID: orgID, TransactionID: a.TransactionID, ActionID: a.ID,
		AgentID: "", PrincipalID: "", Tool: a.Tool, Action: a.Action,
		ArgumentsHash: a.ArgumentsHash, Decision: a.Decision.Effect,
		ResultHash:     receipts.Canonical(result),
		FinancialCents: a.AmountCents, Compensation: "reconciled",
		PolicyVersion: a.Decision.PolicyVersion,
		StartedAt:     a.CreatedAt, CompletedAt: now,
	})
	g.signReceipt(r)
	g.store.UpdateReceipt(r)
	g.store.Emit(domain.AuditEvent{OrgID: orgID, TransactionID: a.TransactionID, ActorType: "tool", ActorID: a.Tool, Type: "action.reconciled", Payload: map[string]any{"action_id": a.ID, "outcome": "confirmed", "receipt_id": r.ID, "trace_id": traceID}})
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

package gateway

import (
	"context"
	"fmt"

	"github.com/GregDixonMXN/settleagent/internal/domain"
	"github.com/GregDixonMXN/settleagent/internal/observe"
	"github.com/GregDixonMXN/settleagent/internal/policies"
	"github.com/GregDixonMXN/settleagent/internal/receipts"
)

// Simulation answers "what would happen if this agent attempted this
// action" without persisting anything: no actions, approvals, receipts,
// or audit events result. Trace spans still emit (read-only).
func (g *Service) Simulate(ctx context.Context, orgID, agentID, tool, action string, args map[string]any) (map[string]any, error) {
	ctx, span := observe.Start(ctx, "policy.simulate", map[string]string{"tool": tool, "action": action})
	defer span.End()
	ag, ok := g.store.GetAgent(orgID, agentID)
	if !ok {
		return nil, fmt.Errorf("agent not found")
	}
	amt := extractCents(args)
	act := domain.TxnAction{
		OrgID: orgID, Tool: tool, Action: action, Arguments: args,
		Classes: g.tools.Classes(tool, action), AmountCents: amt,
	}
	act.ArgumentsHash = receipts.Canonical(args)

	covered, hasGrants, authWhy := CheckAuthority(g.store.GrantsForAgent(orgID, agentID), ag, tool, action, amt)
	if hasGrants && !covered {
		return map[string]any{
			"authority":     map[string]any{"covered": false, "enforced": true, "why": authWhy},
			"decision":      domain.PolicyDecision{Effect: domain.EffectDeny, Explanation: authWhy, ReasonCode: domain.ReasonAuthorityExceeded},
			"classes":       act.Classes,
			"would_execute": false,
		}, nil
	}
	rules, version := g.store.Policies(orgID), 0
	if set, ok := g.store.ActivePolicySet(orgID); ok {
		rules, version = set.Rules, set.Version
	}
	decision := policies.Evaluate(rules, version, policies.EvalInput{Agent: ag, Action: act})
	switch decision.Effect {
	case domain.EffectDeny:
		decision.ReasonCode = domain.ReasonPolicyDenied
	case domain.EffectRequireApproval:
		decision.ReasonCode = domain.ReasonApprovalRequired
	default:
		decision.ReasonCode = domain.ReasonAllowed
	}
	return map[string]any{
		"authority":      map[string]any{"covered": covered, "enforced": hasGrants, "why": authWhy},
		"decision":       decision,
		"classes":        act.Classes,
		"policy_version": version,
		"would_execute":  decision.Effect == domain.EffectAllow || decision.Effect == domain.EffectAllowWithConstraints,
	}, nil
}

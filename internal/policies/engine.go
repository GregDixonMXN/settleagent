package policies

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/GregDixonMXN/settleagent/internal/domain"
)

type EvalInput struct {
	Agent  domain.Agent
	Action domain.TxnAction
}

func amountOf(a domain.TxnAction) int64 {
	if a.AmountCents != nil {
		return *a.AmountCents
	}
	if v, ok := a.Arguments["amount_cents"]; ok {
		switch n := v.(type) {
		case int64:
			return n
		case int:
			return int64(n)
		case float64:
			return int64(n)
		}
	}
	return 0
}

func recipientsOf(a domain.TxnAction) int {
	if v, ok := a.Arguments["recipients"]; ok {
		if s, ok := v.([]any); ok {
			return len(s)
		}
	}
	if v, ok := a.Arguments["recipient_count"]; ok {
		switch n := v.(type) {
		case int:
			return n
		case float64:
			return int(n)
		}
	}
	return 0
}

func ruleMatches(rule domain.PolicyRule, in EvalInput) bool {
	m := rule.Match
	if m.Tool != "" && m.Tool != "*" && m.Tool != in.Action.Tool {
		return false
	}
	if m.Action != "" && m.Action != "*" && m.Action != in.Action.Action {
		return false
	}
	if len(m.AgentGroups) > 0 {
		hit := false
		for _, g := range m.AgentGroups {
			for _, ag := range in.Agent.Groups {
				if g == ag {
					hit = true
				}
			}
		}
		if !hit {
			return false
		}
	}
	if m.Environment != "" && m.Environment != in.Agent.Environment {
		return false
	}
	amt := amountOf(in.Action)
	if m.MaxAmountCents != nil && amt > *m.MaxAmountCents {
		return false
	}
	if m.MinAmountCents != nil && amt < *m.MinAmountCents {
		return false
	}
	if m.MinRecipients != nil && recipientsOf(in.Action) < *m.MinRecipients {
		return false
	}
	if len(m.Classifications) > 0 {
		hit := false
		for _, want := range m.Classifications {
			for _, c := range in.Action.Classes {
				if string(c) == want {
					hit = true
				}
			}
		}
		if !hit {
			return false
		}
	}
	if len(m.Environments) > 0 {
		hit := false
		for _, e := range m.Environments {
			if strings.EqualFold(e, in.Agent.Environment) {
				hit = true
			}
		}
		if !hit {
			return false
		}
	}
	if m.ResourcePrefix != "" {
		res, _ := in.Action.Arguments["resource"].(string)
		if !strings.HasPrefix(res, m.ResourcePrefix) {
			return false
		}
	}
	if m.TimeWindow != nil && !inWindow(m.TimeWindow, time.Now().UTC()) {
		return false
	}
	return true
}

func inWindow(w *domain.TimeWindow, now time.Time) bool {
	if len(w.Weekdays) > 0 {
		hit := false
		for _, d := range w.Weekdays {
			if int(now.Weekday()) == d {
				hit = true
			}
		}
		if !hit {
			return false
		}
	}
	h := now.Hour()
	if w.StartHour <= w.EndHour {
		return h >= w.StartHour && h < w.EndHour
	}
	return h >= w.StartHour || h < w.EndHour
}

// Evaluate returns the highest-priority matching rule. Rules sorted by
// Priority ascending (lower = evaluated first); first match wins.
// No match defaults to REQUIRE_APPROVAL for non-read-only actions and
// ALLOW for pure READ_ONLY (fail-closed except reads).
func Evaluate(rules []domain.PolicyRule, version int, in EvalInput) domain.PolicyDecision {
	sorted := append([]domain.PolicyRule(nil), rules...)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Priority < sorted[j].Priority })
	for _, r := range sorted {
		if ruleMatches(r, in) {
			return domain.PolicyDecision{Effect: r.Effect, RuleID: r.ID, Explanation: r.Explanation, PolicyVersion: version}
		}
	}
	for _, c := range in.Action.Classes {
		if c != domain.ClassReadOnly {
			return domain.PolicyDecision{
				Effect:      domain.EffectRequireApproval,
				Explanation: fmt.Sprintf("%s.%s has no matching policy; default require approval.", in.Action.Tool, in.Action.Action),
			}
		}
	}
	return domain.PolicyDecision{Effect: domain.EffectAllow, Explanation: "Pure read-only action with no matching policy; allowed."}
}

// DefaultSupportPolicies seeds the MVP demo policy set for one org.
func DefaultSupportPolicies(orgID string) []domain.PolicyRule {
	i64 := func(n int64) *int64 { return &n }
	return []domain.PolicyRule{
		{ID: "pol-no-iam", OrgID: orgID, Name: "deny-iam", Priority: 1,
			Match:       domain.PolicyMatch{Tool: "iam"},
			Effect:      domain.EffectDeny,
			Explanation: "Agents cannot modify IAM permissions."},
		{ID: "pol-prod-db", OrgID: orgID, Name: "prod-db-delete-approval", Priority: 2,
			Match:       domain.PolicyMatch{Tool: "postgres", Action: "delete_database"},
			Effect:      domain.EffectRequireApproval,
			Explanation: "Production database deletion always requires human approval."},
		{ID: "pol-refund-auto", OrgID: orgID, Name: "refund-auto", Priority: 10,
			Match:       domain.PolicyMatch{AgentGroups: []string{"support"}, Tool: "stripe", Action: "refund", MaxAmountCents: i64(10000)},
			Effect:      domain.EffectAllow,
			Explanation: "Support agents may refund up to $100 automatically."},
		{ID: "pol-refund-approval", OrgID: orgID, Name: "refund-approval", Priority: 11,
			Match:       domain.PolicyMatch{AgentGroups: []string{"support"}, Tool: "stripe", Action: "refund", MinAmountCents: i64(10001), MaxAmountCents: i64(50000)},
			Effect:      domain.EffectRequireApproval,
			Explanation: "Refunds between $100-$500 require manager approval."},
		{ID: "pol-refund-deny", OrgID: orgID, Name: "refund-deny", Priority: 12,
			Match:       domain.PolicyMatch{AgentGroups: []string{"support"}, Tool: "stripe", Action: "refund", MinAmountCents: i64(50001)},
			Effect:      domain.EffectDeny,
			Explanation: "Refunds above $500 are prohibited for support agents."},
		{ID: "pol-crm", OrgID: orgID, Name: "crm-allow", Priority: 20,
			Match:       domain.PolicyMatch{Tool: "crm", Action: "update_record"},
			Effect:      domain.EffectAllow,
			Explanation: "CRM record updates are reversible and allowed."},
		{ID: "pol-email-bulk", OrgID: orgID, Name: "email-bulk-approval", Priority: 30,
			Match:       domain.PolicyMatch{Tool: "email", MinRecipients: intPtr(51)},
			Effect:      domain.EffectRequireApproval,
			Explanation: "Sending email to more than 50 recipients requires approval."},
		{ID: "pol-email", OrgID: orgID, Name: "email-allow", Priority: 31,
			Match:       domain.PolicyMatch{Tool: "email"},
			Effect:      domain.EffectAllow,
			Explanation: "Single-recipient customer email allowed."},
		{ID: "pol-deploy-staging", OrgID: orgID, Name: "deploy-staging", Priority: 40,
			Match:       domain.PolicyMatch{Tool: "deploy", Action: "deploy", Environment: "staging"},
			Effect:      domain.EffectAllow,
			Explanation: "Agents may deploy to staging automatically."},
		{ID: "pol-deploy-prod", OrgID: orgID, Name: "deploy-prod", Priority: 41,
			Match:       domain.PolicyMatch{Tool: "deploy", Action: "deploy", Environment: "production"},
			Effect:      domain.EffectRequireApproval,
			Explanation: "Production deployment requires approval."},
	}
}

func intPtr(n int) *int { return &n }

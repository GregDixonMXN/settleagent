package gateway

import (
	"fmt"
	"strings"
	"time"

	"github.com/agentguard/agentguard/internal/domain"
)

// ScopeCovers reports whether a scope list permits tool.action.
// Entries: "tool.action", "tool.*", or global "*".
func ScopeCovers(scope []string, tool, action string) bool {
	for _, s := range scope {
		if s == "*" || s == tool+".*" || s == tool+"."+action {
			return true
		}
	}
	return false
}

// GrantCovers checks one grant against an attempted operation. The second
// return explains the first uncovered dimension for human-readable denials.
func GrantCovers(g domain.AuthorityGrant, tool, action string, amount *int64, env string) (bool, string) {
	now := time.Now().UTC()
	if g.RevokedAt != nil {
		return false, "grant revoked"
	}
	if g.ExpiresAt != nil && now.After(*g.ExpiresAt) {
		return false, "grant expired"
	}
	if !ScopeCovers(g.Scope, tool, action) {
		return false, fmt.Sprintf("scope does not cover %s.%s", tool, action)
	}
	if max := g.Constraints.MaxAmountCents; max != nil && amount != nil && *amount > *max {
		return false, fmt.Sprintf("amount %d exceeds grant limit %d (cents)", *amount, *max)
	}
	if envs := g.Constraints.Environments; len(envs) > 0 {
		hit := false
		for _, e := range envs {
			if strings.EqualFold(e, env) {
				hit = true
			}
		}
		if !hit {
			return false, fmt.Sprintf("environment %q outside granted %v", env, envs)
		}
	}
	if g.Environment != "" && !strings.EqualFold(g.Environment, env) {
		return false, fmt.Sprintf("grant is for environment %q", g.Environment)
	}
	return true, ""
}

// CheckAuthority enforces step 4 of authorization: the operation must fall
// within the agent's delegated authority. hasGrants distinguishes an agent
// that never adopted grants (policy-only mode, audit-logged) from one whose
// grants fail to cover the attempt (deny).
func CheckAuthority(grants []domain.AuthorityGrant, agent domain.Agent, tool, action string, amount *int64) (covered bool, hasGrants bool, why string) {
	if len(grants) == 0 {
		return true, false, "no authority grants configured; policy-only mode"
	}
	var reasons []string
	for _, g := range grants {
		ok, reason := GrantCovers(g, tool, action, amount, agent.Environment)
		if ok {
			return true, true, "covered by grant " + g.ID
		}
		if reason != "" {
			reasons = append(reasons, reason)
		}
	}
	detail := strings.Join(reasons, "; ")
	if detail == "" {
		detail = "no grant covers this operation"
	}
	return false, true, fmt.Sprintf("Agent %s is not authorized for %s.%s: %s.", agent.Name, tool, action, detail)
}

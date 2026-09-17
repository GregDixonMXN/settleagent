package gateway

import (
	"fmt"

	"github.com/GregDixonMXN/settleagent/internal/domain"
	"github.com/GregDixonMXN/settleagent/internal/jev"
)

// jevScreenRisk reports whether an ALLOW should escalate to REQUIRE_APPROVAL.
// High risk, torn judgment, or an unreachable judge all escalate (fail
// closed); only a confident low-risk score passes. Disabled without
// SETTLE_JEV=1, in which case nothing calls out.
func (g *Service) jevScreenRisk(orgID, txnID string, ag domain.Agent, a *domain.TxnAction) (bool, string) {
	if !jev.Enabled() {
		return false, ""
	}
	var amount int64
	if a.AmountCents != nil {
		amount = *a.AmountCents
	}
	classes := make([]string, 0, len(a.Classes))
	for _, c := range a.Classes {
		classes = append(classes, string(c))
	}
	var sibCount int
	var sibTotal int64
	for _, s := range g.store.ActionsForTxn(orgID, txnID) {
		if s.ID == a.ID {
			continue
		}
		sibCount++
		if s.AmountCents != nil {
			sibTotal += *s.AmountCents
		}
	}
	risk, err := jev.ScreenRisk(jev.Action{
		Tool: a.Tool, Name: a.Action, AmountCents: amount,
		Arguments: a.Arguments, Classes: classes,
		AgentName: ag.Name, AgentGroups: ag.Groups, AgentEnv: ag.Environment,
		SiblingCount: sibCount, SiblingTotal: sibTotal,
	})
	if err != nil {
		return true, fmt.Sprintf("risk screen unavailable: %v", err)
	}
	if risk.Confidence < 0.5 {
		return true, fmt.Sprintf("uncertain risk (score %d, confidence %.2f)", risk.Score100, risk.Confidence)
	}
	if risk.Score100 >= jev.Threshold() {
		return true, fmt.Sprintf("risk score %d >= threshold %d", risk.Score100, jev.Threshold())
	}
	return false, ""
}

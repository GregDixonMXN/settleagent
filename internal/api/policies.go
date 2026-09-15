package api

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/GregDixonMXN/settleagent/internal/domain"
)

func (s *Server) handleSimulatePolicy(w http.ResponseWriter, r *http.Request) {
	orgID := orgOf(r)
	var body struct {
		AgentID   string         `json:"agent_id"`
		Tool      string         `json:"tool"`
		Action    string         `json:"action"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Tool == "" {
		errJSON(w, 400, "bad_request", "Body needs agent_id, tool, action, arguments. Nothing is persisted or executed.")
		return
	}
	if !callerIsOperator(r) && body.AgentID != callerAgentID(r) {
		errJSON(w, 403, "agent_mismatch", "A credential may only simulate its own agent identity.")
		return
	}
	out, err := s.svc.Simulate(r.Context(), orgID, body.AgentID, body.Tool, body.Action, body.Arguments)
	if err != nil {
		errJSON(w, 400, "simulate_failed", err.Error())
		return
	}
	writeJSON(w, 200, out)
}

func (s *Server) handleCreatePolicySet(w http.ResponseWriter, r *http.Request) {
	orgID := orgOf(r)
	if !callerIsOperator(r) {
		errJSON(w, 403, "operator_required", "Only a human operator token can draft policies.")
		return
	}
	var body struct {
		Rules []domain.PolicyRule `json:"rules"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Rules) == 0 {
		errJSON(w, 400, "bad_request", "Body needs rules (array of {name, match, effect, explanation}). Created as draft; activate separately.")
		return
	}
	for i, rule := range body.Rules {
		if rule.Effect != domain.EffectAllow && rule.Effect != domain.EffectDeny &&
			rule.Effect != domain.EffectRequireApproval && rule.Effect != domain.EffectAllowWithConstraints {
			errJSON(w, 400, "bad_rule", "Rule "+strconv.Itoa(i)+" ("+rule.Name+"): effect must be ALLOW, DENY, REQUIRE_APPROVAL, or ALLOW_WITH_CONSTRAINTS.")
			return
		}
	}
	set := s.store.CreatePolicySet(orgID, body.Rules, operatorName(r))
	s.store.Emit(domain.AuditEvent{OrgID: orgID, ActorType: "human", ActorID: operatorName(r), Type: "policy.set_created", Payload: map[string]any{"version": set.Version, "rules": len(set.Rules)}})
	writeJSON(w, 201, set)
}

func (s *Server) handleListPolicySets(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.store.PolicySets(orgOf(r)))
}

func (s *Server) handleActivatePolicySet(w http.ResponseWriter, r *http.Request) {
	orgID := orgOf(r)
	if !callerIsOperator(r) {
		errJSON(w, 403, "operator_required", "Only a human operator token can activate policies.")
		return
	}
	version, err := strconv.Atoi(r.PathValue("version"))
	if err != nil {
		errJSON(w, 400, "bad_request", "Version must be an integer.")
		return
	}
	if !s.store.ActivatePolicySet(orgID, version) {
		errJSON(w, 404, "policy_set_not_found", "No such draft version in this organization.")
		return
	}
	s.store.Emit(domain.AuditEvent{OrgID: orgID, ActorType: "human", ActorID: operatorName(r), Type: "policy.activated", Payload: map[string]any{"version": version}})
	writeJSON(w, 200, map[string]any{"active": version})
}

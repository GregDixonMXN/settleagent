package api

import (
	"encoding/json"
	"net/http"
	"regexp"
	"time"

	"github.com/settleagent/settleagent/internal/auth"
	"github.com/settleagent/settleagent/internal/domain"
)

var scopeEntry = regexp.MustCompile(`^([a-z0-9:_-]+)\.([a-z0-9:_*-]+)$`)

func validScope(scope []string) bool {
	if len(scope) == 0 {
		return false
	}
	for _, s := range scope {
		if s == "*" {
			continue
		}
		if !scopeEntry.MatchString(s) {
			return false
		}
	}
	return true
}

func operatorName(r *http.Request) string {
	if id, ok := auth.IdentityFrom(r.Context()); ok && id.Name != "" {
		return id.Name
	}
	return "operator"
}

func (s *Server) handleCreateGrant(w http.ResponseWriter, r *http.Request) {
	orgID := orgOf(r)
	if !callerIsOperator(r) {
		errJSON(w, 403, "operator_required", "Only a human operator token can grant authority.")
		return
	}
	var body struct {
		AgentID     string                  `json:"agent_id"`
		PrincipalID string                  `json:"principal_id"`
		Scope       []string                `json:"scope"`
		Constraints domain.GrantConstraints `json:"constraints"`
		Environment string                  `json:"environment"`
		ExpiresAt   *time.Time              `json:"expires_at"`
		Bootstrap   bool                    `json:"bootstrap"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.AgentID == "" {
		errJSON(w, 400, "bad_request", "Body needs agent_id, scope (e.g. [\"stripe.refund\", \"crm.*\"]), and optional constraints/environment/expires_at.")
		return
	}
	if _, ok := s.store.GetAgent(orgID, body.AgentID); !ok {
		errJSON(w, 404, "agent_not_found", "Agent is not in this organization.")
		return
	}
	if !validScope(body.Scope) {
		errJSON(w, 400, "bad_scope", "Scope entries must be \"*\", \"tool.*\", or \"tool.action\" (lowercase, digits, _ - :).")
		return
	}
	g := s.store.CreateGrant(domain.AuthorityGrant{
		OrgID: orgID, PrincipalID: body.PrincipalID, AgentID: body.AgentID,
		Scope: body.Scope, Constraints: body.Constraints, Environment: body.Environment,
		IssuedBy: operatorName(r), ExpiresAt: body.ExpiresAt, Bootstrap: body.Bootstrap,
	})
	s.store.Emit(domain.AuditEvent{OrgID: orgID, ActorType: "human", ActorID: operatorName(r), Type: "grant.issued", Payload: map[string]any{"grant_id": g.ID, "agent_id": g.AgentID, "scope": g.Scope}})
	writeJSON(w, 201, g)
}

func (s *Server) handleListGrants(w http.ResponseWriter, r *http.Request) {
	orgID := orgOf(r)
	agentID := r.URL.Query().Get("agent_id")
	if !callerIsOperator(r) {
		if agentID == "" || agentID != callerAgentID(r) {
			errJSON(w, 403, "agent_mismatch", "Agents may only inspect their own grants.")
			return
		}
	}
	if agentID == "" {
		errJSON(w, 400, "bad_request", "Pass ?agent_id= (operators may list per agent).")
		return
	}
	writeJSON(w, 200, s.store.GrantsForAgent(orgID, agentID))
}

func (s *Server) handleRevokeGrant(w http.ResponseWriter, r *http.Request) {
	orgID := orgOf(r)
	if !callerIsOperator(r) {
		errJSON(w, 403, "operator_required", "Only a human operator token can revoke authority.")
		return
	}
	id := r.PathValue("id")
	if !s.store.RevokeGrant(orgID, id) {
		errJSON(w, 404, "grant_not_found", "Grant is missing, already revoked, or in another organization.")
		return
	}
	s.store.Emit(domain.AuditEvent{OrgID: orgID, ActorType: "human", ActorID: operatorName(r), Type: "grant.revoked", Payload: map[string]any{"grant_id": id}})
	writeJSON(w, 200, map[string]any{"revoked": id})
}

func (s *Server) handleRotateCredential(w http.ResponseWriter, r *http.Request) {
	orgID := orgOf(r)
	if !callerIsOperator(r) {
		errJSON(w, 403, "operator_required", "Only a human operator token can issue credentials.")
		return
	}
	agentID := r.PathValue("id")
	ag, ok := s.store.GetAgent(orgID, agentID)
	if !ok {
		errJSON(w, 404, "agent_not_found", "Agent is not in this organization.")
		return
	}
	keyID, secret, hash, err := auth.NewAgentSecret()
	if err != nil {
		errJSON(w, 500, "secret_failed", "Could not issue credential.")
		return
	}
	s.store.StoreCredential(orgID, ag.ID, keyID, hash)
	s.store.Emit(domain.AuditEvent{OrgID: orgID, ActorType: "human", ActorID: operatorName(r), Type: "credential.issued", Payload: map[string]any{"agent_id": ag.ID, "key_id": keyID}})
	writeJSON(w, 201, map[string]any{"agent_id": ag.ID, "key_id": keyID, "api_secret": secret, "warning": "Store this secret; it is never shown again."})
}

func (s *Server) handleRevokeCredential(w http.ResponseWriter, r *http.Request) {
	orgID := orgOf(r)
	if !callerIsOperator(r) {
		errJSON(w, 403, "operator_required", "Only a human operator token can revoke credentials.")
		return
	}
	keyID := r.PathValue("keyID")
	// Confirm the credential belongs to this org before revoking.
	cOrg, _, _, ok := s.store.GetCredential(keyID)
	if !ok || cOrg != orgID {
		errJSON(w, 404, "credential_not_found", "Credential is missing, already revoked, or in another organization.")
		return
	}
	s.store.RevokeCredential(keyID)
	s.store.Emit(domain.AuditEvent{OrgID: orgID, ActorType: "human", ActorID: operatorName(r), Type: "credential.revoked", Payload: map[string]any{"key_id": keyID}})
	writeJSON(w, 200, map[string]any{"revoked": keyID})
}

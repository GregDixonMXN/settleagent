package api

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/settleagent/settleagent/internal/domain"
	"github.com/settleagent/settleagent/internal/integrations"
)

func (s *Server) handleListIntegrations(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, map[string]any{
		"credentials": s.store.ListIntegrationCredentials(orgOf(r)),
		"mcp_servers": len(s.store.ListMCPServers(orgOf(r))),
	})
}

func (s *Server) handleSetIntegrationCredential(w http.ResponseWriter, r *http.Request) {
	orgID := orgOf(r)
	if !callerIsOperator(r) {
		errJSON(w, 403, "operator_required", "Only a human operator token can register third-party credentials.")
		return
	}
	name := r.PathValue("name")
	if !integrations.CredentialNames[name] {
		errJSON(w, 400, "unknown_integration", "Known integrations: stripe, github, postgres.")
		return
	}
	var body struct {
		Secret string `json:"secret"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Secret == "" {
		errJSON(w, 400, "bad_request", "Body needs a secret. It is sealed at rest and never returned.")
		return
	}
	switch name {
	case "stripe":
		if strings.HasPrefix(body.Secret, "sk_live") {
			errJSON(w, 400, "live_key_refused", "Live Stripe keys are never accepted; use a test-mode key (sk_test_).")
			return
		}
		if !strings.HasPrefix(body.Secret, "sk_test_") {
			errJSON(w, 400, "bad_secret", "Not a Stripe test-mode key.")
			return
		}
	case "postgres":
		if !strings.HasPrefix(body.Secret, "postgres://") && !strings.HasPrefix(body.Secret, "postgresql://") {
			errJSON(w, 400, "bad_secret", "Postgres credential must be a postgres:// DSN.")
			return
		}
	}
	if err := s.store.SetIntegrationCredential(orgID, name, body.Secret); err != nil {
		errJSON(w, 500, "store_failed", "Could not store credential.")
		return
	}
	s.store.Emit(domain.AuditEvent{OrgID: orgID, ActorType: "human", ActorID: operatorName(r), Type: "integration.credential_set", Payload: map[string]any{"integration": name}})
	writeJSON(w, 201, map[string]any{"configured": name})
}

func (s *Server) handleSetHTTPDomains(w http.ResponseWriter, r *http.Request) {
	orgID := orgOf(r)
	if !callerIsOperator(r) {
		errJSON(w, 403, "operator_required", "Only a human operator token can set the HTTP allowlist.")
		return
	}
	var body struct {
		Domains []string `json:"domains"`
		Methods []string `json:"methods"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || len(body.Domains) == 0 {
		errJSON(w, 400, "bad_request", "Body needs domains (e.g. [\"api.example.com\"]) and optional methods (default GET, HEAD).")
		return
	}
	clean := []string{}
	for _, d := range body.Domains {
		d = strings.ToLower(strings.TrimSpace(d))
		if u, err := url.Parse("https://" + d); err != nil || u.Hostname() != d || strings.Contains(d, "/") {
			errJSON(w, 400, "bad_domain", d+" is not a bare hostname.")
			return
		}
		clean = append(clean, d)
	}
	methods := body.Methods
	if len(methods) == 0 {
		methods = []string{"GET", "HEAD"}
	}
	for _, m := range methods {
		switch strings.ToUpper(m) {
		case "GET", "HEAD", "POST", "PUT", "PATCH", "DELETE":
		default:
			errJSON(w, 400, "bad_method", m+" is not a supported method.")
			return
		}
	}
	s.store.SetIntegrationConfig(orgID, "http", map[string]any{"domains": clean, "methods": methods})
	s.store.Emit(domain.AuditEvent{OrgID: orgID, ActorType: "human", ActorID: operatorName(r), Type: "integration.http_allowlist_set", Payload: map[string]any{"domains": clean, "methods": methods}})
	writeJSON(w, 201, map[string]any{"domains": clean, "methods": methods})
}

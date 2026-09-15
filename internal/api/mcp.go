package api

import (
	"encoding/json"
	"net/http"
	"regexp"

	"github.com/GregDixonMXN/settleagent/internal/domain"
	"github.com/GregDixonMXN/settleagent/internal/integrations"
)

var mcpName = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

func validClasses(in map[string][]string) (map[string][]domain.ActionClass, error) {
	known := map[string]bool{
		"READ_ONLY": true, "REVERSIBLE": true, "COMPENSABLE": true,
		"IRREVERSIBLE": true, "FINANCIAL": true, "DESTRUCTIVE": true,
		"EXTERNAL_COMMUNICATION": true, "PRIVILEGED": true, "UNKNOWN": true,
	}
	out := map[string][]domain.ActionClass{}
	for tool, list := range in {
		var cls []domain.ActionClass
		for _, c := range list {
			if !known[c] {
				return nil, errBadClass(c)
			}
			cls = append(cls, domain.ActionClass(c))
		}
		out[tool] = cls
	}
	return out, nil
}

type badClassError string

func errBadClass(c string) error { return badClassError(c) }

func (e badClassError) Error() string { return "unknown action class " + string(e) }

func (s *Server) handleRegisterMCPServer(w http.ResponseWriter, r *http.Request) {
	orgID := orgOf(r)
	if !callerIsOperator(r) {
		errJSON(w, 403, "operator_required", "Only a human operator token can register upstream MCP servers.")
		return
	}
	var body struct {
		Name      string              `json:"name"`
		URL       string              `json:"url"`
		AuthToken string              `json:"auth_token"`
		Classes   map[string][]string `json:"classes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" || body.URL == "" {
		errJSON(w, 400, "bad_request", "Body needs name (lowercase letters, digits, dashes), url, and optional auth_token and classes.")
		return
	}
	if !mcpName.MatchString(body.Name) {
		errJSON(w, 400, "bad_name", "Server name must match ^[a-z0-9][a-z0-9-]{0,63}$ so policy rules can target mcp:<name> unambiguously.")
		return
	}
	if err := integrations.ValidateURL(body.URL); err != nil {
		errJSON(w, 400, "bad_url", err.Error())
		return
	}
	classes, err := validClasses(body.Classes)
	if err != nil {
		errJSON(w, 400, "bad_class", err.Error()+". Valid: READ_ONLY REVERSIBLE COMPENSABLE IRREVERSIBLE FINANCIAL DESTRUCTIVE EXTERNAL_COMMUNICATION PRIVILEGED UNKNOWN.")
		return
	}
	// Live tools/list proves reachability and populates the cached catalog.
	tools, err := integrations.ListTools(r.Context(), body.URL, body.AuthToken)
	if err != nil {
		errJSON(w, 502, "upstream_unreachable", "Could not fetch tools/list from upstream: "+err.Error())
		return
	}
	srv := s.store.UpsertMCPServer(domain.MCPServer{
		OrgID: orgID, Name: body.Name, URL: body.URL, Tools: tools, Classes: classes,
	}, body.AuthToken)
	for _, t := range tools {
		s.svc.Tools().Register(integrations.ToolName(body.Name), t.Name,
			classes[t.Name], integrations.HandlerFor(body.URL, body.AuthToken, t.Name), nil)
	}
	s.store.Emit(domain.AuditEvent{OrgID: orgID, TransactionID: "", ActorType: "human", ActorID: callerName(r), Type: "mcp.server_registered", Payload: map[string]any{"server": body.Name, "url": integrations.RedactURL(body.URL), "tools": len(tools)}})
	writeJSON(w, 201, srv)
}

func callerName(r *http.Request) string {
	if id, ok := authIdentity(r); ok && id.Name != "" {
		return id.Name
	}
	return "operator"
}

func (s *Server) handleListMCPServers(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.store.ListMCPServers(orgOf(r)))
}

func (s *Server) handleListMCPTools(w http.ResponseWriter, r *http.Request) {
	orgID := orgOf(r)
	if name := r.URL.Query().Get("server"); name != "" {
		srv, _, ok := s.store.GetMCPServer(orgID, name)
		if !ok {
			errJSON(w, 404, "server_not_found", "No MCP server "+name+" in this organization.")
			return
		}
		writeJSON(w, 200, srv)
		return
	}
	writeJSON(w, 200, s.store.ListMCPServers(orgID))
}

func (s *Server) handleMCPCall(w http.ResponseWriter, r *http.Request) {
	orgID := orgOf(r)
	var body struct {
		TransactionID  string         `json:"transaction_id"`
		AgentID        string         `json:"agent_id"`
		Server         string         `json:"server"`
		Tool           string         `json:"tool"`
		Arguments      map[string]any `json:"arguments"`
		IdempotencyKey string         `json:"idempotency_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.TransactionID == "" || body.Server == "" || body.Tool == "" {
		errJSON(w, 400, "bad_request", "Body needs transaction_id, agent_id, server, tool, arguments, idempotency_key.")
		return
	}
	if body.IdempotencyKey == "" {
		errJSON(w, 400, "idempotency_required", "Every proxied call needs an idempotency_key.")
		return
	}
	if !callerIsOperator(r) && body.AgentID != callerAgentID(r) {
		errJSON(w, 403, "agent_mismatch", "A credential may only act as its own agent identity.")
		return
	}
	srv, token, ok := s.store.GetMCPServer(orgID, body.Server)
	if !ok {
		errJSON(w, 404, "server_not_found", "No MCP server "+body.Server+" in this organization.")
		return
	}
	listed := false
	for _, t := range srv.Tools {
		if t.Name == body.Tool {
			listed = true
		}
	}
	if !listed {
		errJSON(w, 400, "tool_not_listed", "Tool "+body.Tool+" is not in "+body.Server+"'s cached catalog; re-register the server to refresh it.")
		return
	}
	// Ensure the engine can evaluate and execute this upstream tool.
	s.svc.Tools().Register(integrations.ToolName(body.Server), body.Tool,
		srv.Classes[body.Tool], integrations.HandlerFor(srv.URL, token, body.Tool), nil)
	a, err := s.svc.ProposeAction(r.Context(), orgID, body.TransactionID, body.AgentID,
		integrations.ToolName(body.Server), body.Tool, body.Arguments, body.IdempotencyKey)
	if err != nil {
		errJSON(w, 400, "propose_failed", err.Error())
		return
	}
	writeJSON(w, 201, a)
}

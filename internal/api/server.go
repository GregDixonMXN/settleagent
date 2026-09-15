package api

import (
	"encoding/json"
	"net/http"
	"reflect"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/settleagent/settleagent/internal/actions"
	"github.com/settleagent/settleagent/internal/auth"
	"github.com/settleagent/settleagent/internal/domain"
	"github.com/settleagent/settleagent/internal/gateway"
	"github.com/settleagent/settleagent/internal/integrations"
	"github.com/settleagent/settleagent/internal/keys"
	"github.com/settleagent/settleagent/internal/observe"
	"github.com/settleagent/settleagent/internal/store"
	"github.com/settleagent/settleagent/internal/transactions"
)

type Server struct {
	store    store.Store
	svc      *gateway.Service
	mux      *http.ServeMux
	limiter  *auth.Limiter
	verifier keys.Provider
}

// SetSigner enables receipt signatures on execution and signature
// verification on the verify endpoints. Nil means unsigned (chain only).
func (s *Server) SetSigner(p keys.Provider) {
	s.svc.SetSigner(p)
	s.verifier = p
}

// EnableRealIntegrations swaps mock handlers for live ones backed by
// per-org credentials. Tests and explicit mock setups skip this.
func (s *Server) EnableRealIntegrations() {
	integrations.RegisterReal(s.svc.Tools(), s.store)
}

func New(s store.Store) *Server {
	srv := &Server{store: s, svc: gateway.NewService(s, actions.DefaultRegistry()), mux: http.NewServeMux(), limiter: auth.NewLimiter(120, time.Minute)}
	srv.routes()
	return srv
}

func (s *Server) Handler() http.Handler {
	return corsMiddleware(observe.Middleware(auth.Middleware(s.store, s.limiter, s.mux)))
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	// Normalize top-level nil slices to [] so list endpoints never emit null.
	if v != nil {
		rv := reflect.ValueOf(v)
		if rv.Kind() == reflect.Slice && rv.IsNil() {
			_, _ = w.Write([]byte("[]"))
			return
		}
	}
	_ = json.NewEncoder(w).Encode(v)
}

func errJSON(w http.ResponseWriter, code int, msg, why string) {
	writeJSON(w, code, map[string]any{"error": msg, "why": why})
}

func (s *Server) routes() {
	s.mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"ok": true})
	})
	s.mux.HandleFunc("POST /v1/agents", s.handleRegisterAgent)
	s.mux.HandleFunc("POST /v1/transactions", s.handleCreateTxn)
	s.mux.HandleFunc("GET /v1/transactions", s.handleListTxns)
	s.mux.HandleFunc("GET /v1/transactions/", s.handleGetTxn)
	s.mux.HandleFunc("POST /v1/actions", s.handleProposeAction)
	s.mux.HandleFunc("POST /v1/actions/", s.handleActionSub)
	s.mux.HandleFunc("GET /v1/approvals", s.handleListApprovals)
	s.mux.HandleFunc("POST /v1/approvals/", s.handleDecideApproval)
	s.mux.HandleFunc("GET /v1/receipts", s.handleListReceipts)
	s.mux.HandleFunc("GET /v1/audit", s.handleAudit)
	s.mux.HandleFunc("GET /v1/policies", s.handleListPolicies)
	s.mux.HandleFunc("POST /v1/mcp/servers", s.handleRegisterMCPServer)
	s.mux.HandleFunc("GET /v1/mcp/servers", s.handleListMCPServers)
	s.mux.HandleFunc("GET /v1/mcp/tools", s.handleListMCPTools)
	s.mux.HandleFunc("POST /v1/mcp/call", s.handleMCPCall)
	s.mux.HandleFunc("POST /v1/grants", s.handleCreateGrant)
	s.mux.HandleFunc("GET /v1/grants", s.handleListGrants)
	s.mux.HandleFunc("POST /v1/grants/{id}/revoke", s.handleRevokeGrant)
	s.mux.HandleFunc("POST /v1/agents/{id}/credentials", s.handleRotateCredential)
	s.mux.HandleFunc("DELETE /v1/credentials/{keyID}", s.handleRevokeCredential)
	s.mux.HandleFunc("POST /v1/actions/{id}/reconcile", s.handleReconcile)
	s.mux.HandleFunc("GET /v1/receipts/verify", s.handleVerifyReceipts)
	s.mux.HandleFunc("POST /v1/receipts/verify", s.handleVerifyReceipt)
	s.mux.HandleFunc("POST /v1/policies/evaluate", s.handleSimulatePolicy)
	s.mux.HandleFunc("POST /v1/policies/sets", s.handleCreatePolicySet)
	s.mux.HandleFunc("GET /v1/policies/sets", s.handleListPolicySets)
	s.mux.HandleFunc("POST /v1/policies/sets/{version}/activate", s.handleActivatePolicySet)
	s.mux.HandleFunc("GET /v1/integrations", s.handleListIntegrations)
	s.mux.HandleFunc("POST /v1/integrations/{name}/credentials", s.handleSetIntegrationCredential)
	s.mux.HandleFunc("POST /v1/transactions/{id}/commit", s.handleCommitTxn)
	s.mux.HandleFunc("POST /v1/integrations/http/domains", s.handleSetHTTPDomains)
	s.mux.HandleFunc("GET /openapi.json", s.handleOpenAPI)
}

// orgOf returns the tenant from the verified credential. The tenant comes
// from authentication, never from headers or body parameters.
func orgOf(r *http.Request) string {
	if id, ok := auth.IdentityFrom(r.Context()); ok {
		return id.OrgID
	}
	return ""
}

func callerIsOperator(r *http.Request) bool {
	if id, ok := auth.IdentityFrom(r.Context()); ok {
		return id.Operator
	}
	return false
}

func callerAgentID(r *http.Request) string {
	if id, ok := auth.IdentityFrom(r.Context()); ok {
		return id.AgentID
	}
	return ""
}

func authIdentity(r *http.Request) (auth.Identity, bool) {
	return auth.IdentityFrom(r.Context())
}

func (s *Server) handleRegisterAgent(w http.ResponseWriter, r *http.Request) {
	orgID := orgOf(r)
	if orgID == "" {
		errJSON(w, 401, "unauthenticated", "Valid bearer credential required.")
		return
	}
	if !callerIsOperator(r) {
		errJSON(w, 403, "operator_required", "Only a human operator token can register agents.")
		return
	}
	var body struct {
		PrincipalID string   `json:"principal_id"`
		Name        string   `json:"name"`
		Environment string   `json:"environment"`
		Groups      []string `json:"groups"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Name == "" {
		errJSON(w, 400, "bad_request", "Body needs name, principal_id, environment, groups.")
		return
	}
	if _, err := uuid.Parse(body.PrincipalID); err != nil {
		errJSON(w, 400, "bad_principal", "principal_id must be a UUID of a principal in this organization.")
		return
	}
	keyID, secret, hash, err := auth.NewAgentSecret()
	if err != nil {
		errJSON(w, 500, "secret_failed", "Could not issue credential.")
		return
	}
	a := s.store.CreateAgent(orgID, body.PrincipalID, body.Name, body.Environment, body.Groups, hash)
	s.store.StoreCredential(orgID, a.ID, keyID, hash)
	s.store.Emit(domain.AuditEvent{OrgID: orgID, ActorType: "system", ActorID: "api", Type: "agent.registered", Payload: map[string]any{"agent_id": a.ID}})
	writeJSON(w, 201, map[string]any{"agent": a, "api_secret": secret, "warning": "Store this secret; it is never shown again."})
}

func (s *Server) handleCreateTxn(w http.ResponseWriter, r *http.Request) {
	orgID := orgOf(r)
	var body struct {
		AgentID     string `json:"agent_id"`
		PrincipalID string `json:"principal_id"`
		SessionID   string `json:"session_id"`
		Objective   string `json:"objective"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.AgentID == "" {
		errJSON(w, 400, "bad_request", "Body needs agent_id, principal_id, session_id, objective.")
		return
	}
	if _, ok := s.store.GetAgent(orgID, body.AgentID); !ok {
		errJSON(w, 404, "agent_not_found", "Agent is not in this organization.")
		return
	}
	if !callerIsOperator(r) && body.AgentID != callerAgentID(r) {
		errJSON(w, 403, "agent_mismatch", "A credential may only act as its own agent identity.")
		return
	}
	t := s.store.CreateTxn(domain.Transaction{OrgID: orgID, AgentID: body.AgentID, PrincipalID: body.PrincipalID, SessionID: body.SessionID, Objective: body.Objective, Status: domain.TxnPlanning})
	writeJSON(w, 201, t)
}

func (s *Server) handleListTxns(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.store.ListTxns(orgOf(r)))
}

func (s *Server) handleGetTxn(w http.ResponseWriter, r *http.Request) {
	orgID := orgOf(r)
	id := strings.TrimPrefix(r.URL.Path, "/v1/transactions/")
	t, ok := s.store.GetTxn(orgID, id)
	if !ok {
		errJSON(w, 404, "not_found", "Transaction not found in this organization.")
		return
	}
	writeJSON(w, 200, map[string]any{
		"transaction": t,
		"actions":     nonNullActions(s.store.ActionsForTxn(orgID, id)),
		"receipts":    nonNullReceipts(s.store.ReceiptsForTxn(orgID, id)),
		"audit":       nonNullAudit(s.store.Audit(orgID, id, 100)),
	})
}

func nonNullActions(v []domain.TxnAction) any {
	if v == nil {
		return []domain.TxnAction{}
	}
	return v
}

func nonNullReceipts(v []domain.Receipt) any {
	if v == nil {
		return []domain.Receipt{}
	}
	return v
}

func nonNullAudit(v []domain.AuditEvent) any {
	if v == nil {
		return []domain.AuditEvent{}
	}
	return v
}

func (s *Server) handleProposeAction(w http.ResponseWriter, r *http.Request) {
	orgID := orgOf(r)
	var body struct {
		TransactionID  string         `json:"transaction_id"`
		AgentID        string         `json:"agent_id"`
		Tool           string         `json:"tool"`
		Action         string         `json:"action"`
		Arguments      map[string]any `json:"arguments"`
		IdempotencyKey string         `json:"idempotency_key"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.TransactionID == "" || body.Tool == "" {
		errJSON(w, 400, "bad_request", "Body needs transaction_id, agent_id, tool, action, arguments, idempotency_key.")
		return
	}
	if body.IdempotencyKey == "" {
		errJSON(w, 400, "idempotency_required", "Every action needs an idempotency_key so retries never double-execute payments, emails, or deletions.")
		return
	}
	if !callerIsOperator(r) && body.AgentID != callerAgentID(r) {
		errJSON(w, 403, "agent_mismatch", "A credential may only act as its own agent identity.")
		return
	}
	a, err := s.svc.ProposeAction(r.Context(), orgID, body.TransactionID, body.AgentID, body.Tool, body.Action, body.Arguments, body.IdempotencyKey)
	if err != nil {
		errJSON(w, 400, "propose_failed", err.Error())
		return
	}
	writeJSON(w, 201, a)
}

func (s *Server) handleActionSub(w http.ResponseWriter, r *http.Request) {
	orgID := orgOf(r)
	rest := strings.TrimPrefix(r.URL.Path, "/v1/actions/")
	parts := strings.Split(rest, "/")
	if len(parts) != 2 || parts[1] != "execute" || r.Method != "POST" {
		errJSON(w, 404, "not_found", "Use POST /v1/actions/:id/execute.")
		return
	}
	id := parts[0]
	// Prefer approved path: if action is approved, run it; else run allowed.
	if a, ok := s.store.GetAction(orgID, id); ok && a.Status == "approved" {
		out, err := s.svc.RunApproved(r.Context(), orgID, id)
		if err != nil {
			errJSON(w, 400, "execute_failed", err.Error())
			return
		}
		writeJSON(w, 200, out)
		return
	}
	out, err := s.svc.ExecuteAllowed(r.Context(), orgID, id)
	if err != nil {
		errJSON(w, 400, "execute_refused", err.Error())
		return
	}
	writeJSON(w, 200, out)
}

func (s *Server) handleListApprovals(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.store.PendingApprovals(orgOf(r)))
}

func (s *Server) handleDecideApproval(w http.ResponseWriter, r *http.Request) {
	orgID := orgOf(r)
	if !callerIsOperator(r) {
		errJSON(w, 403, "operator_required", "Approvals are human decisions; an operator token is required.")
		return
	}
	rest := strings.TrimPrefix(r.URL.Path, "/v1/approvals/")
	parts := strings.Split(rest, "/")
	if len(parts) != 2 || parts[1] != "decide" || r.Method != "POST" {
		errJSON(w, 404, "not_found", "Use POST /v1/approvals/:id/decide.")
		return
	}
	var body struct {
		Approve   bool   `json:"approve"`
		DecidedBy string `json:"decided_by"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.DecidedBy == "" {
		body.DecidedBy = "human"
	}
	ap, ok := s.svc.Store().DecideApproval(orgID, parts[0], body.DecidedBy, body.Approve)
	if !ok {
		errJSON(w, 404, "approval_not_found", "Approval is missing, already decided, or in another organization.")
		return
	}
	s.store.Emit(domain.AuditEvent{OrgID: orgID, TransactionID: ap.TransactionID, ActorType: "human", ActorID: body.DecidedBy, Type: map[bool]string{true: "approval.granted", false: "approval.denied"}[body.Approve], Payload: map[string]any{"approval_id": ap.ID}})
	if body.Approve && ap.ActionID != nil {
		if a, ok := s.store.GetAction(orgID, *ap.ActionID); ok {
			a.Status = "approved"
			s.store.UpdateAction(a)
			t, _ := s.store.GetTxn(orgID, ap.TransactionID)
			if t != nil {
				_ = transactions.MustTransition(t, domain.TxnExecuting)
				_ = s.store.SetTxnStatus(orgID, t.ID, t.Status)
			}
		}
	}
	if !body.Approve {
		if t, ok := s.store.GetTxn(orgID, ap.TransactionID); ok {
			_ = transactions.MustTransition(t, domain.TxnAborting)
			_ = s.store.SetTxnStatus(orgID, t.ID, t.Status)
		}
	}
	writeJSON(w, 200, ap)
}

func (s *Server) handleListReceipts(w http.ResponseWriter, r *http.Request) {
	orgID := orgOf(r)
	txn := r.URL.Query().Get("transaction_id")
	writeJSON(w, 200, s.store.ReceiptsForTxn(orgID, txn))
}

func (s *Server) handleAudit(w http.ResponseWriter, r *http.Request) {
	orgID := orgOf(r)
	txn := r.URL.Query().Get("transaction_id")
	writeJSON(w, 200, s.store.Audit(orgID, txn, 200))
}

func (s *Server) handleListPolicies(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, 200, s.store.Policies(orgOf(r)))
}

func (s *Server) handleOpenAPI(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(openAPISpec))
}

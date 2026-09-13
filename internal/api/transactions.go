package api

import (
	"net/http"
)

func (s *Server) handleCommitTxn(w http.ResponseWriter, r *http.Request) {
	orgID := orgOf(r)
	id := r.PathValue("id")
	t, ok := s.store.GetTxn(orgID, id)
	if !ok {
		errJSON(w, 404, "not_found", "Transaction not found in this organization.")
		return
	}
	if !callerIsOperator(r) && t.AgentID != callerAgentID(r) {
		errJSON(w, 403, "agent_mismatch", "Only the owning agent or an operator can commit a transaction.")
		return
	}
	out, err := s.svc.Commit(r.Context(), orgID, id)
	if err != nil {
		errJSON(w, 400, "commit_refused", err.Error())
		return
	}
	writeJSON(w, 200, out)
}

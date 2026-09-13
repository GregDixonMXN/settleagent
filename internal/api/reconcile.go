package api

import (
	"encoding/hex"
	"encoding/json"
	"net/http"

	"github.com/agentguard/agentguard/internal/domain"
)

func (s *Server) handleReconcile(w http.ResponseWriter, r *http.Request) {
	orgID := orgOf(r)
	if !callerIsOperator(r) {
		errJSON(w, 403, "operator_required", "Reconciliation settles uncertain side effects; an operator token is required.")
		return
	}
	id := r.PathValue("id")
	a, err := s.svc.Reconcile(r.Context(), orgID, id)
	if err != nil {
		errJSON(w, 400, "reconcile_failed", err.Error())
		return
	}
	writeJSON(w, 200, a)
}

func (s *Server) sigOK(r domain.Receipt) (bool, string) {
	if r.Signature == "" || r.KeyID == "" {
		return false, "unsigned"
	}
	if s.verifier == nil {
		return false, "no verifier configured"
	}
	sig, err := hex.DecodeString(r.Signature)
	if err != nil {
		return false, "malformed signature"
	}
	if !s.verifier.Verify([]byte(r.Hash), sig) {
		return false, "signature invalid"
	}
	return true, "valid (key " + r.KeyID + ")"
}

func (s *Server) handleVerifyReceipts(w http.ResponseWriter, r *http.Request) {
	orgID := orgOf(r)
	txn := r.URL.Query().Get("transaction_id")
	receipts := s.store.ReceiptsForTxn(orgID, txn)
	broken := s.store.VerifyChain(orgID)
	chainOK := broken == -1
	out := make([]map[string]any, 0, len(receipts))
	for _, rc := range receipts {
		ok, detail := s.sigOK(rc)
		out = append(out, map[string]any{
			"receipt_id": rc.ID, "action_id": rc.ActionID,
			"hash": rc.Hash, "chain_ok": chainOK,
			"signature_ok": ok, "signature_detail": detail,
		})
	}
	writeJSON(w, 200, map[string]any{"chain_broken_at": broken, "receipts": out})
}

func (s *Server) handleVerifyReceipt(w http.ResponseWriter, r *http.Request) {
	var rc domain.Receipt
	if err := json.NewDecoder(r.Body).Decode(&rc); err != nil {
		errJSON(w, 400, "bad_request", "Body must be a receipt object.")
		return
	}
	ok, detail := s.sigOK(rc)
	writeJSON(w, 200, map[string]any{
		"receipt_id": rc.ID, "hash": rc.Hash,
		"signature_ok": ok, "signature_detail": detail,
	})
}

package gateway_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// The $80 refund allows under default policies; the risk screen decides
// whether it stays allowed.
func riskStub(t *testing.T, score, confidence float64) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{
			"answers": map[string]any{
				"risk": map[string]any{"score": score, "confidence": confidence},
			},
		})
	}))
}

func proposeRefund(t *testing.T) string {
	t.Helper()
	_, g, ag, tx, org := policySetup(t)
	a, err := g.ProposeAction(context.Background(), org, tx.ID, ag.ID, "stripe", "refund", map[string]any{"amount_cents": 8000}, "rk-1")
	if err != nil {
		t.Fatal(err)
	}
	return string(a.Status)
}

func TestRiskScreenFlagOffStaysAllowed(t *testing.T) {
	t.Setenv("SETTLE_JEV", "")
	t.Setenv("JEV_API_KEY", "test-key")
	if got := proposeRefund(t); got != "allowed" {
		t.Fatalf("status = %s, want allowed", got)
	}
}

func TestRiskScreenLowRiskStaysAllowed(t *testing.T) {
	srv := riskStub(t, 0.4, 0.9) // ~10/100, confident
	defer srv.Close()
	t.Setenv("SETTLE_JEV", "1")
	t.Setenv("JEV_API_KEY", "test-key")
	t.Setenv("JEV_API_URL", srv.URL)
	if got := proposeRefund(t); got != "allowed" {
		t.Fatalf("status = %s, want allowed", got)
	}
}

func TestRiskScreenHighRiskEscalates(t *testing.T) {
	srv := riskStub(t, 3.6, 0.9) // ~90/100, confident
	defer srv.Close()
	t.Setenv("SETTLE_JEV", "1")
	t.Setenv("JEV_API_KEY", "test-key")
	t.Setenv("JEV_API_URL", srv.URL)
	if got := proposeRefund(t); got != "awaiting_approval" {
		t.Fatalf("status = %s, want awaiting_approval", got)
	}
}

func TestRiskScreenTornJudgmentEscalates(t *testing.T) {
	srv := riskStub(t, 1.0, 0.3) // low score, no conviction
	defer srv.Close()
	t.Setenv("SETTLE_JEV", "1")
	t.Setenv("JEV_API_KEY", "test-key")
	t.Setenv("JEV_API_URL", srv.URL)
	if got := proposeRefund(t); got != "awaiting_approval" {
		t.Fatalf("status = %s, want awaiting_approval", got)
	}
}

func TestRiskScreenUnreachableJudgeEscalates(t *testing.T) {
	t.Setenv("SETTLE_JEV", "1")
	t.Setenv("JEV_API_KEY", "test-key")
	t.Setenv("JEV_API_URL", "http://127.0.0.1:1/")
	if got := proposeRefund(t); got != "awaiting_approval" {
		t.Fatalf("status = %s, want awaiting_approval", got)
	}
}

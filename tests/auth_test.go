package gateway_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/GregDixonMXN/settleagent/internal/api"
	"github.com/GregDixonMXN/settleagent/internal/auth"
	"github.com/GregDixonMXN/settleagent/internal/store"
	"golang.org/x/crypto/bcrypt"
)

func authSetup(t *testing.T) (store.Store, string, string, string, string, string) {
	t.Helper()
	s := store.New()
	org, principal := s.SeedOrg("Auth Co")
	ag := s.CreateAgent(org.ID, principal.ID, "a1", "production", []string{"support"}, "unused")
	kid, secret, hash, err := auth.NewAgentSecret()
	if err != nil {
		t.Fatal(err)
	}
	s.StoreCredential(org.ID, ag.ID, kid, hash)
	okid, opSecret, opHash, err := auth.NewOperatorSecret()
	if err != nil {
		t.Fatal(err)
	}
	s.CreateOperatorToken(org.ID, "tester", okid, opHash)
	return s, org.ID, principal.ID, secret, opSecret, ag.ID
}

func doReq(t *testing.T, h http.Handler, method, path, token, body string) *httptest.ResponseRecorder {
	t.Helper()
	var rdr *strings.Reader
	if body == "" {
		rdr = strings.NewReader("")
	} else {
		rdr = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rdr)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestUnauthenticatedRejected(t *testing.T) {
	s, _, _, _, _, _ := authSetup(t)
	h := api.New(s).Handler()
	rec := doReq(t, h, "GET", "/v1/transactions", "", "")
	if rec.Code != 401 {
		t.Fatalf("expected 401 got %d", rec.Code)
	}
	rec = doReq(t, h, "GET", "/health", "", "")
	if rec.Code != 200 {
		t.Fatalf("health should stay open, got %d", rec.Code)
	}
}

func TestBadTokenRejected(t *testing.T) {
	s, _, _, _, _, _ := authSetup(t)
	h := api.New(s).Handler()
	rec := doReq(t, h, "GET", "/v1/transactions", "st_deadbeef_wrong", "")
	if rec.Code != 401 {
		t.Fatalf("expected 401 got %d", rec.Code)
	}
}

func TestAgentFlowsAndMismatch(t *testing.T) {
	s, orgID, prinID, secret, opSecret, agentID := authSetup(t)
	h := api.New(s).Handler()

	// Create txn as self.
	rec := doReq(t, h, "POST", "/v1/transactions", secret,
		`{"agent_id":"`+agentID+`","principal_id":"p","session_id":"s","objective":"o"}`)
	if rec.Code != 201 {
		t.Fatalf("create txn: %d %s", rec.Code, rec.Body.String())
	}
	var txn map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &txn)

	// Acting as another (real) agent is forbidden.
	rec = doReq(t, h, "POST", "/v1/agents", opSecret,
		`{"principal_id":"`+prinID+`","name":"a2","environment":"production","groups":[]}`)
	if rec.Code != 201 {
		t.Fatalf("setup register: %d", rec.Code)
	}
	var reg2 struct {
		Agent struct {
			ID string `json:"id"`
		} `json:"agent"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &reg2)
	rec = doReq(t, h, "POST", "/v1/transactions", secret,
		`{"agent_id":"`+reg2.Agent.ID+`","principal_id":"p","session_id":"s","objective":"o"}`)
	if rec.Code != 403 {
		t.Fatalf("expected 403 agent_mismatch, got %d", rec.Code)
	}

	// Agents cannot register agents.
	rec = doReq(t, h, "POST", "/v1/agents", secret,
		`{"principal_id":"`+prinID+`","name":"evil","environment":"production","groups":[]}`)
	if rec.Code != 403 {
		t.Fatalf("expected 403 operator_required, got %d", rec.Code)
	}

	// Agents cannot decide approvals (need the txn to propose first is enough;
	// here the 403 must fire before any lookup).
	rec = doReq(t, h, "POST", "/v1/approvals/nope/decide", secret, `{"approve":true}`)
	if rec.Code != 403 {
		t.Fatalf("expected 403 on decide, got %d", rec.Code)
	}
	_ = orgID
}

func TestOperatorCanRegisterAndDecide(t *testing.T) {
	s, _, prinID, agentSecret, opSecret, agentID := authSetup(t)
	h := api.New(s).Handler()

	rec := doReq(t, h, "POST", "/v1/agents", opSecret,
		`{"principal_id":"`+prinID+`","name":"a2","environment":"staging","groups":["support"]}`)
	if rec.Code != 201 {
		t.Fatalf("operator register: %d %s", rec.Code, rec.Body.String())
	}
	var reg struct {
		Agent struct {
			ID string `json:"id"`
		} `json:"agent"`
		Secret string `json:"api_secret"`
	}
	_ = json.Unmarshal(rec.Body.Bytes(), &reg)
	if !strings.HasPrefix(reg.Secret, "st_") || reg.Agent.ID == "" {
		t.Fatalf("bad registration response: %s", rec.Body.String())
	}

	// New credential works for its own agent.
	rec = doReq(t, h, "POST", "/v1/transactions", reg.Secret,
		`{"agent_id":"`+reg.Agent.ID+`","principal_id":"p","session_id":"s","objective":"o"}`)
	if rec.Code != 201 {
		t.Fatalf("new agent txn: %d %s", rec.Code, rec.Body.String())
	}

	// Operator can act as any agent (provisioning/debugging path).
	rec = doReq(t, h, "POST", "/v1/transactions", opSecret,
		`{"agent_id":"`+agentID+`","principal_id":"p","session_id":"s","objective":"o"}`)
	if rec.Code != 201 {
		t.Fatalf("operator txn: %d %s", rec.Code, rec.Body.String())
	}
	_ = agentSecret
}

func TestLegacySecretNeedsOrg(t *testing.T) {
	s := store.New()
	org, principal := s.SeedOrg("Legacy Co")
	legacy := "st_" + strings.Repeat("ab", 24)
	h, err := bcrypt.GenerateFromPassword([]byte(legacy), bcrypt.MinCost)
	if err != nil {
		t.Fatal(err)
	}
	// Pre-key-ID credential: hash stored on the agent, no key row.
	ag := s.CreateAgent(org.ID, principal.ID, "old", "production", []string{"support"}, string(h))
	h2 := api.New(s).Handler()

	// Without org hint the legacy secret cannot be resolved.
	rec := doReq(t, h2, "GET", "/v1/transactions", legacy, "")
	if rec.Code != 401 {
		t.Fatalf("expected 401 without org, got %d", rec.Code)
	}
	// With X-Org-ID the bounded fallback scan authenticates it.
	req := httptest.NewRequest("GET", "/v1/transactions", strings.NewReader(""))
	req.Header.Set("Authorization", "Bearer "+legacy)
	req.Header.Set("X-Org-ID", org.ID)
	rec2 := httptest.NewRecorder()
	h2.ServeHTTP(rec2, req)
	if rec2.Code != 200 {
		t.Fatalf("expected 200 with org hint, got %d %s", rec2.Code, rec2.Body.String())
	}
	_ = ag
}

func TestCORSPreflight(t *testing.T) {
	s, _, _, _, _, _ := authSetup(t)
	h := api.New(s).Handler()
	req := httptest.NewRequest("OPTIONS", "/v1/transactions", nil)
	req.Header.Set("Origin", "http://localhost:3000")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 204 {
		t.Fatalf("preflight: %d", rec.Code)
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "http://localhost:3000" {
		t.Fatal("missing allow-origin on preflight")
	}
	// Unlisted origins get no CORS headers.
	req = httptest.NewRequest("OPTIONS", "/v1/transactions", nil)
	req.Header.Set("Origin", "https://evil.example")
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Header().Get("Access-Control-Allow-Origin") != "" {
		t.Fatal("CORS granted to unlisted origin")
	}
}

func TestRateLimit(t *testing.T) {
	s, _, _, secret, _, _ := authSetup(t)
	limited := auth.Middleware(s, auth.NewLimiter(2, time.Minute), http.HandlerFunc(
		func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200) }))
	for i := 0; i < 2; i++ {
		rec := doReq(t, limited, "GET", "/v1/transactions", secret, "")
		if rec.Code != 200 {
			t.Fatalf("request %d: %d", i, rec.Code)
		}
	}
	rec := doReq(t, limited, "GET", "/v1/transactions", secret, "")
	if rec.Code != 429 {
		t.Fatalf("expected 429 got %d", rec.Code)
	}
}

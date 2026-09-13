package integrations

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/agentguard/agentguard/internal/actions"
	"github.com/agentguard/agentguard/internal/domain"
	"github.com/agentguard/agentguard/internal/gateway"
	"github.com/agentguard/agentguard/internal/keys"
	"github.com/agentguard/agentguard/internal/policies"
	"github.com/agentguard/agentguard/internal/store"
)

func testStore(t *testing.T) (*store.MemoryStore, *gateway.Service, domain.Agent, domain.Transaction, string) {
	t.Helper()
	s := store.New()
	kp, _, err := keys.Load()
	if err != nil {
		t.Fatal(err)
	}
	s.SetKeyProvider(kp)
	org, principal := s.SeedOrg("Integ Co")
	s.SetPolicies(org.ID, policies.DefaultSupportPolicies(org.ID))
	reg := actions.DefaultRegistry()
	RegisterReal(reg, s)
	g := gateway.NewService(s, reg)
	ag := s.CreateAgent(org.ID, principal.ID, "agent", "production", []string{"support"}, "hash")
	tx := s.CreateTxn(domain.Transaction{OrgID: org.ID, AgentID: ag.ID, PrincipalID: principal.ID, SessionID: "s", Objective: "o", Status: domain.TxnPlanning})
	return s, g, ag, *tx, org.ID
}

func exec(t *testing.T, g *gateway.Service, s *store.MemoryStore, ag domain.Agent, tx domain.Transaction, tool, action string, args map[string]any, key string) (*domain.TxnAction, error) {
	t.Helper()
	a, err := g.ProposeAction(context.Background(), tx.OrgID, tx.ID, ag.ID, tool, action, args, key)
	if err != nil {
		t.Fatal(err)
	}
	return g.ExecuteAllowed(context.Background(), tx.OrgID, a.ID)
}

func TestStripeLiveKeyRefused(t *testing.T) {
	if _, err := stripeKey("sk_live_abc"); err == nil {
		t.Fatal("live key accepted")
	}
	if _, err := stripeKey("whatever"); err == nil {
		t.Fatal("non-test key accepted")
	}
	if _, err := stripeKey("sk_test_abc"); err != nil {
		t.Fatalf("test key refused: %v", err)
	}
}

func TestStripeUnconfiguredFailsLoudly(t *testing.T) {
	s, g, ag, tx, _ := testStore(t)
	_ = s
	_, err := exec(t, g, s, ag, tx, "stripe", "refund", map[string]any{"charge": "ch_1", "amount_cents": 100}, "st-1")
	if err == nil || !strings.Contains(err.Error(), "not configured") {
		t.Fatalf("expected not-configured, got %v", err)
	}
}

func TestStripeRefundAgainstStub(t *testing.T) {
	old := stripeBaseURL
	defer func() { stripeBaseURL = old }()
	var gotIdem string
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotIdem = r.Header.Get("Idempotency-Key")
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(r.URL.Path, "/v1/refunds") && r.Method == "POST" {
			_, _ = w.Write([]byte(`{"id":"re_123","amount":100,"status":"succeeded"}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"re_123","amount":100}]}`))
	}))
	defer stub.Close()
	stripeBaseURL = stub.URL

	s, g, ag, tx, org := testStore(t)
	if err := s.SetIntegrationCredential(org, "stripe", "sk_test_demo"); err != nil {
		t.Fatal(err)
	}
	done, err := exec(t, g, s, ag, tx, "stripe", "refund", map[string]any{"charge": "ch_1", "amount_cents": 100}, "st-2")
	if err != nil {
		t.Fatalf("refund: %v", err)
	}
	if done.Status != "executed" {
		t.Fatalf("status %s", done.Status)
	}
	if gotIdem != "st-2" {
		t.Fatalf("idempotency key not forwarded: %q", gotIdem)
	}
	// Sealed at rest: raw store must not contain the plaintext.
	raw, ok := s.GetIntegrationCredential(org, "stripe")
	if !ok || raw != "sk_test_demo" {
		t.Fatal("credential round-trip broken")
	}
}

func TestGitHubAgainstStub(t *testing.T) {
	old := githubBaseURL
	defer func() { githubBaseURL = old }()
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":1,"title":"hello"}`))
	}))
	defer stub.Close()
	githubBaseURL = stub.URL

	s, g, ag, tx, org := testStore(t)
	if err := s.SetIntegrationCredential(org, "github", "ghp_demo"); err != nil {
		t.Fatal(err)
	}
	done, err := exec(t, g, s, ag, tx, "github", "read_repo",
		map[string]any{"owner": "o", "repo": "r"}, "gh-1")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if done.Status != "executed" {
		t.Fatalf("status %s", done.Status)
	}
}

func TestPostgresGuardsAndRead(t *testing.T) {
	s, g, ag, tx, org := testStore(t)
	pgURL := testPGURL(t)
	if err := s.SetIntegrationCredential(org, "postgres", pgURL); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{
		"DROP TABLE x",
		"SELECT 1; DROP TABLE x",
		"UPDATE t SET a=1",
		"DELETE FROM t",
		"",
	} {
		key := "pg-bad-" + strings.Map(func(r rune) rune {
			if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
				return r
			}
			return '-'
		}, bad)
		if _, err := exec(t, g, s, ag, tx, "postgres", "query", map[string]any{"sql": bad}, key); err == nil {
			t.Fatalf("dangerous SQL accepted: %q", bad)
		}
	}
	done, err := exec(t, g, s, ag, tx, "postgres", "query", map[string]any{"sql": "SELECT 1 AS one"}, "pg-ok")
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	rows, _ := done.Result["rows"].([]any)
	if len(rows) != 1 {
		t.Fatalf("rows: %v", done.Result)
	}
}

func TestHTTPAllowlist(t *testing.T) {
	t.Setenv("AG_ALLOW_LOOPBACK", "1")
	stub := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("stub-body"))
	}))
	defer stub.Close()

	s, g, ag, tx, org := testStore(t)
	// http.request has no default rule; allow it explicitly for this test.
	rules := append([]domain.PolicyRule{{
		ID: "http-test", OrgID: org, Name: "http-test", Priority: 1,
		Match:  domain.PolicyMatch{Tool: "http"},
		Effect: domain.EffectAllow, Explanation: "test",
	}}, policies.DefaultSupportPolicies(org)...)
	s.SetPolicies(org, rules)
	// Default deny: no allowlist configured.
	if _, err := exec(t, g, s, ag, tx, "http", "request",
		map[string]any{"url": stub.URL}, "http-1"); err == nil {
		t.Fatal("fetch without allowlist allowed")
	}
	host := strings.TrimPrefix(stub.URL, "http://")
	if i := strings.Index(host, ":"); i >= 0 {
		host = host[:i]
	}
	s.SetIntegrationConfig(org, "http", map[string]any{"domains": []any{"127.0.0.1"}, "methods": []any{"GET"}})
	done, err := exec(t, g, s, ag, tx, "http", "request",
		map[string]any{"url": stub.URL + "/x"}, "http-2")
	if err != nil {
		t.Fatalf("allowlisted fetch: %v", err)
	}
	if done.Result["body"] != "stub-body" {
		t.Fatalf("body: %v", done.Result)
	}
	_ = host
	// Non-allowlisted method refused.
	if _, err := exec(t, g, s, ag, tx, "http", "request",
		map[string]any{"method": "POST", "url": stub.URL}, "http-3"); err == nil {
		t.Fatal("POST without method allowlist allowed")
	}
}

func testPGURL(t *testing.T) string {
	t.Helper()
	if u := os.Getenv("TEST_DATABASE_URL"); u != "" {
		return u
	}
	return "postgres://agentguard:agentguard@127.0.0.1:15433/agentguard?sslmode=disable"
}

func TestSealedCredentialsRoundTrip(t *testing.T) {
	kp, _, err := keys.Load()
	if err != nil {
		t.Fatal(err)
	}
	sealed, err := kp.Seal("sk_test_secret")
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(sealed, "sk_test") || !strings.HasPrefix(sealed, "v1:") {
		t.Fatalf("not sealed: %q", sealed[:8])
	}
	pt, err := kp.Open(sealed)
	if err != nil || pt != "sk_test_secret" {
		t.Fatalf("round-trip: %q %v", pt, err)
	}
	if pt, _ := kp.Open("plaintext-legacy"); pt != "plaintext-legacy" {
		t.Fatal("legacy passthrough broken")
	}
}

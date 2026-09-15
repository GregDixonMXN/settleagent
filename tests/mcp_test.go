package gateway_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/settleagent/settleagent/internal/api"
	"github.com/settleagent/settleagent/internal/domain"
	"github.com/settleagent/settleagent/internal/policies"
)

// mockMCP serves JSON-RPC tools/list + tools/call.
func mockMCP(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string `json:"method"`
			Params struct {
				Name      string         `json:"name"`
				Arguments map[string]any `json:"arguments"`
			} `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "tools/list":
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"tools":[
				{"name":"read_file","description":"Read a file"},
				{"name":"delete_repo","description":"Delete a repository"}]}}`))
		case "tools/call":
			if req.Params.Name == "explode" {
				_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"boom"}}`))
				return
			}
			out, _ := json.Marshal(map[string]any{
				"jsonrpc": "2.0", "id": 1,
				"result": map[string]any{"content": []any{
					map[string]any{"type": "text", "text": "mock:" + req.Params.Name},
				}},
			})
			_, _ = w.Write(out)
		default:
			w.WriteHeader(404)
		}
	}))
}

func TestMCPProxy(t *testing.T) {
	upstream := mockMCP(t)
	defer upstream.Close()

	s, org, _, agentSecret, opSecret, agentID := authSetup(t)
	s.SetPolicies(org, policies.DefaultSupportPolicies(org))
	h := api.New(s).Handler()

	// Operator registers the server; tools cached; token never returned.
	regBody := `{"name":"docs-mcp","url":"` + upstream.URL + `","classes":{"read_file":["READ_ONLY"],"delete_repo":["DESTRUCTIVE","IRREVERSIBLE"]}}`
	rec := doReq(t, h, "POST", "/v1/mcp/servers", opSecret, regBody)
	if rec.Code != 201 {
		t.Fatalf("register: %d %s", rec.Code, rec.Body.String())
	}
	if strings.Contains(rec.Body.String(), "auth_token") {
		t.Fatal("registration response leaks auth_token")
	}
	// Agents cannot register servers.
	rec = doReq(t, h, "POST", "/v1/mcp/servers", agentSecret, regBody)
	if rec.Code != 403 {
		t.Fatalf("expected 403, got %d", rec.Code)
	}
	// Bad names and non-HTTP URLs rejected with reasons.
	rec = doReq(t, h, "POST", "/v1/mcp/servers", opSecret, `{"name":"Bad_Name!","url":"`+upstream.URL+`"}`)
	if rec.Code != 400 {
		t.Fatalf("expected 400 bad name, got %d", rec.Code)
	}
	rec = doReq(t, h, "POST", "/v1/mcp/servers", opSecret, `{"name":"x","url":"ftp://evil/x"}`)
	if rec.Code != 400 {
		t.Fatalf("expected 400 bad url, got %d", rec.Code)
	}

	rec = doReq(t, h, "POST", "/v1/transactions", agentSecret,
		`{"agent_id":"`+agentID+`","principal_id":"p","session_id":"s","objective":"o"}`)
	var txn map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &txn)
	txID := txn["id"].(string)

	call := func(tool, key string, args string) map[string]any {
		rec := doReq(t, h, "POST", "/v1/mcp/call", agentSecret,
			`{"transaction_id":"`+txID+`","agent_id":"`+agentID+`","server":"docs-mcp","tool":"`+tool+`","arguments":`+args+`,"idempotency_key":"`+key+`"}`)
		if rec.Code != 201 {
			t.Fatalf("call %s: %d %s", tool, rec.Code, rec.Body.String())
		}
		var a map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &a)
		return a
	}
	exec := func(id string) (int, string) {
		rec := doReq(t, h, "POST", "/v1/actions/"+id+"/execute", agentSecret, "")
		return rec.Code, rec.Body.String()
	}

	// READ_ONLY tool: allowed by default, executes through the proxy.
	a := call("read_file", "m-read-1", `{"path":"/x"}`)
	if a["status"] != "allowed" {
		t.Fatalf("read_file: %v", a)
	}
	code, body := exec(a["id"].(string))
	if code != 200 || !strings.Contains(body, "mock:read_file") {
		t.Fatalf("execute: %d %s", code, body)
	}

	// Destructive tool: approval-gated, blocked before approval.
	b := call("delete_repo", "m-del-1", `{"repo":"r"}`)
	if b["status"] != "awaiting_approval" {
		t.Fatalf("delete_repo: %v", b)
	}
	if code, _ := exec(b["id"].(string)); code != 400 {
		t.Fatalf("executed before approval: %d", code)
	}

	// Unlisted tool and unknown server rejected before policy.
	rec = doReq(t, h, "POST", "/v1/mcp/call", agentSecret,
		`{"transaction_id":"`+txID+`","agent_id":"`+agentID+`","server":"docs-mcp","tool":"nope","arguments":{},"idempotency_key":"m-nope"}`)
	if rec.Code != 400 {
		t.Fatalf("expected 400 unlisted, got %d", rec.Code)
	}

	// Explicit DENY policy stops the call at propose time.
	rules := append([]domain.PolicyRule{{
		ID: "deny-mcp-del", OrgID: org, Name: "deny-mcp-del", Priority: 1,
		Match:       domain.PolicyMatch{Tool: "mcp:docs-mcp", Action: "delete_repo"},
		Effect:      domain.EffectDeny,
		Explanation: "MCP destructive tools are denied in this org.",
	}}, policies.DefaultSupportPolicies(org)...)
	s.SetPolicies(org, rules)
	c := call("delete_repo", "m-del-2", `{"repo":"r"}`)
	if c["status"] != "denied" {
		t.Fatalf("expected denied: %v", c)
	}
	if code, _ := exec(c["id"].(string)); code != 400 {
		t.Fatalf("denied executed: %d", code)
	}
}

package integrations

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/settleagent/settleagent/internal/actions"
	"github.com/settleagent/settleagent/internal/domain"
)

var upstreamClient = &http.Client{Timeout: 15 * time.Second}

type rpcRequest struct {
	JSONRPC string `json:"jsonrpc"`
	ID      int    `json:"id"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
}

type rpcResponse struct {
	Result *json.RawMessage `json:"result"`
	Error  *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// ValidateURL rejects non-HTTP(S) upstream targets. It is a scheme guard,
// not full SSRF protection (no allowlist/DNS pinning yet — see THREAT_MODEL).
func ValidateURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return fmt.Errorf("upstream must be an http(s) URL")
	}
	if u.Host == "" {
		return fmt.Errorf("upstream URL needs a host")
	}
	return nil
}

func callUpstream(ctx context.Context, serverURL, token, method string, params any) (*json.RawMessage, error) {
	body, _ := json.Marshal(rpcRequest{JSONRPC: "2.0", ID: 1, Method: method, Params: params})
	req, err := http.NewRequestWithContext(ctx, "POST", serverURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := upstreamClient.Do(req)
	if err != nil {
		// No response: the call may or may not have executed upstream.
		// Uncertain, never a blind retry — reconcile with the idempotency key.
		return nil, actions.Uncertain(fmt.Errorf("upstream unreachable: %w", err))
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("upstream status %d", resp.StatusCode)
	}
	var rpc rpcResponse
	if err := json.NewDecoder(resp.Body).Decode(&rpc); err != nil {
		// Treat malformed tool responses as errors, never as success.
		return nil, fmt.Errorf("upstream returned malformed response: %w", err)
	}
	if rpc.Error != nil {
		return nil, fmt.Errorf("upstream error %d: %s", rpc.Error.Code, rpc.Error.Message)
	}
	if rpc.Result == nil {
		return nil, fmt.Errorf("upstream returned empty result")
	}
	return rpc.Result, nil
}

// ListTools proxies tools/list and returns the server's tool catalog.
func ListTools(ctx context.Context, serverURL, token string) ([]domain.MCPTool, error) {
	raw, err := callUpstream(ctx, serverURL, token, "tools/list", nil)
	if err != nil {
		return nil, err
	}
	var out struct {
		Tools []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"tools"`
	}
	if err := json.Unmarshal(*raw, &out); err != nil {
		return nil, fmt.Errorf("upstream tools/list malformed: %w", err)
	}
	tools := make([]domain.MCPTool, len(out.Tools))
	for i, t := range out.Tools {
		tools[i] = domain.MCPTool{Name: t.Name, Description: t.Description}
	}
	return tools, nil
}

// ToolName maps an MCP server+tool to the engine's tool namespace.
func ToolName(server string) string { return "mcp:" + server }

// HandlerFor builds the engine ToolHandler that proxies one upstream tool.
// Upstream content is returned as evidence; upstream errors fail the action.
func HandlerFor(serverURL, token, tool string) actions.ToolHandler {
	return func(ctx context.Context, a domain.TxnAction) (map[string]any, error) {
		raw, err := callUpstream(ctx, serverURL, token, "tools/call", map[string]any{
			"name": tool, "arguments": a.Arguments,
		})
		if err != nil {
			return nil, err
		}
		var result map[string]any
		if err := json.Unmarshal(*raw, &result); err != nil {
			return map[string]any{"raw": string(*raw)}, nil
		}
		if isErr, _ := result["isError"].(bool); isErr {
			return nil, fmt.Errorf("upstream tool reported error: %v", result["content"])
		}
		return result, nil
	}
}

// RedactURL strips any userinfo before a URL is logged or stored in audit.
func RedactURL(raw string) string {
	if i := strings.Index(raw, "@"); i >= 0 {
		if j := strings.Index(raw, "://"); j >= 0 && j < i {
			return raw[:j+3] + "***@" + raw[i+1:]
		}
	}
	return raw
}

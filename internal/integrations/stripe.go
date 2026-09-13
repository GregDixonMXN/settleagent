package integrations

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/agentguard/agentguard/internal/actions"
	"github.com/agentguard/agentguard/internal/domain"
)

var integClient = &http.Client{Timeout: 15 * time.Second}

// providerBaseURLs are overridable in tests; production values are fixed.
var (
	stripeBaseURL = "https://api.stripe.com"
	githubBaseURL = "https://api.github.com"
)

// doJSON performs an authenticated JSON call. Transport failures (no
// response at all) return Uncertain: the side effect may have happened.
// Definitive HTTP responses decode or fail definitively.
func doJSON(ctx context.Context, method, url, token, idemKey string, form map[string]string, extraHeaders map[string]string) (map[string]any, int, error) {
	var body io.Reader
	if form != nil {
		data := urlValues(form)
		body = strings.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, 0, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	if form != nil {
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	} else {
		req.Header.Set("Content-Type", "application/json")
	}
	if idemKey != "" {
		req.Header.Set("Idempotency-Key", idemKey)
	}
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}
	resp, err := integClient.Do(req)
	if err != nil {
		return nil, 0, actions.Uncertain(fmt.Errorf("no response from provider: %w", err))
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return nil, resp.StatusCode, fmt.Errorf("provider status %d: %s", resp.StatusCode, truncate(string(raw), 300))
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("provider returned malformed JSON: %w", err)
	}
	return out, resp.StatusCode, nil
}

// doJSONRaw is doJSON with a pre-encoded JSON body.
func doJSONRaw(ctx context.Context, method, url, token string, bodyJSON []byte, extraHeaders map[string]string) (map[string]any, int, error) {
	var body io.Reader
	if bodyJSON != nil {
		body = bytes.NewReader(bodyJSON)
	}
	req, err := http.NewRequestWithContext(ctx, method, url, body)
	if err != nil {
		return nil, 0, err
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}
	resp, err := integClient.Do(req)
	if err != nil {
		return nil, 0, actions.Uncertain(fmt.Errorf("no response from provider: %w", err))
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode >= 300 {
		return nil, resp.StatusCode, fmt.Errorf("provider status %d: %s", resp.StatusCode, truncate(string(raw), 300))
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, resp.StatusCode, fmt.Errorf("provider returned malformed JSON: %w", err)
	}
	return out, resp.StatusCode, nil
}

func urlValues(form map[string]string) string {
	var parts []string
	for k, v := range form {
		parts = append(parts, urlEncode(k)+"="+urlEncode(v))
	}
	return strings.Join(parts, "&")
}

func urlEncode(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.' || c == '~' {
			b.WriteByte(c)
		} else {
			fmt.Fprintf(&b, "%%%02X", c)
		}
	}
	return b.String()
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func numArg(args map[string]any, keys ...string) (int64, bool) {
	for _, k := range keys {
		if v, ok := args[k]; ok {
			switch n := v.(type) {
			case int64:
				return n, true
			case int:
				return int64(n), true
			case float64:
				return int64(n), true
			}
		}
	}
	return 0, false
}

func strArg(args map[string]any, keys ...string) (string, bool) {
	for _, k := range keys {
		if v, ok := args[k]; ok {
			if s, ok := v.(string); ok && s != "" {
				return s, true
			}
		}
	}
	return "", false
}

// Stripe test-mode client. Live keys are refused outright: test mode only.
func stripeKey(secret string) (string, error) {
	if strings.HasPrefix(secret, "sk_live") {
		return "", fmt.Errorf("live Stripe keys are never accepted; use a test-mode key")
	}
	if !strings.HasPrefix(secret, "sk_test_") {
		return "", fmt.Errorf("not a Stripe test-mode key (must start sk_test_)")
	}
	return secret, nil
}

func stripeRefundHandler(getCred func(orgID string) (string, bool)) actions.ToolHandler {
	return func(ctx context.Context, a domain.TxnAction) (map[string]any, error) {
		secret, ok := getCred(a.OrgID)
		if !ok {
			return nil, fmt.Errorf("stripe integration not configured for this org (register a test-mode key first)")
		}
		key, err := stripeKey(secret)
		if err != nil {
			return nil, err
		}
		charge, ok := strArg(a.Arguments, "charge", "charge_id")
		if !ok {
			return nil, fmt.Errorf("stripe.refund needs a charge id")
		}
		form := map[string]string{"charge": charge}
		if amt, ok := numArg(a.Arguments, "amount_cents"); ok {
			form["amount"] = fmt.Sprint(amt)
		}
		if reason, ok := strArg(a.Arguments, "reason"); ok {
			form["reason"] = reason
		}
		out, _, err := doJSON(ctx, "POST", stripeBaseURL+"/v1/refunds", key, a.IdempotencyKey, form, nil)
		if err != nil {
			return nil, err
		}
		return out, nil
	}
}

func stripeReadCustomerHandler(getCred func(orgID string) (string, bool)) actions.ToolHandler {
	return func(ctx context.Context, a domain.TxnAction) (map[string]any, error) {
		secret, ok := getCred(a.OrgID)
		if !ok {
			return nil, fmt.Errorf("stripe integration not configured for this org")
		}
		key, err := stripeKey(secret)
		if err != nil {
			return nil, err
		}
		id, ok := strArg(a.Arguments, "customer", "customer_id")
		if !ok {
			return nil, fmt.Errorf("stripe.read_customer needs a customer id")
		}
		out, _, err := doJSON(ctx, "GET", stripeBaseURL+"/v1/customers/"+urlEncode(id), key, "", nil, nil)
		if err != nil {
			return nil, err
		}
		return out, nil
	}
}

// stripeRefundReconciler matches our idempotent refund by charge + amount.
func stripeRefundReconciler(getCred func(orgID string) (string, bool)) actions.Reconciler {
	return func(ctx context.Context, a domain.TxnAction) (bool, map[string]any, error) {
		secret, ok := getCred(a.OrgID)
		if !ok {
			return false, nil, fmt.Errorf("stripe integration not configured")
		}
		key, err := stripeKey(secret)
		if err != nil {
			return false, nil, err
		}
		charge, ok := strArg(a.Arguments, "charge", "charge_id")
		if !ok {
			return false, nil, fmt.Errorf("no charge id to reconcile against")
		}
		out, _, err := doJSON(ctx, "GET",
			stripeBaseURL+"/v1/refunds?charge="+urlEncode(charge)+"&limit=10",
			key, "", nil, nil)
		if err != nil {
			return false, nil, err
		}
		want, _ := numArg(a.Arguments, "amount_cents")
		if list, ok := out["data"].([]any); ok {
			for _, item := range list {
				if m, ok := item.(map[string]any); ok {
					if amt, ok := m["amount"].(float64); ok && (want == 0 || int64(amt) == want) {
						return true, m, nil
					}
				}
			}
		}
		return false, nil, nil
	}
}

var _ = domain.ClassFinancial

package integrations

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/settleagent/settleagent/internal/actions"
	"github.com/settleagent/settleagent/internal/domain"
	"github.com/settleagent/settleagent/internal/store"
)

var httpClient = &http.Client{Timeout: 15 * time.Second}

// domainAllowed matches exact hosts and subdomains ("api.example.com"
// matches "example.com"). Empty allowlist denies everything.
func domainAllowed(host string, allow []string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	for _, d := range allow {
		d = strings.ToLower(strings.TrimSpace(strings.TrimSuffix(d, ".")))
		if d == "" {
			continue
		}
		if host == d || strings.HasSuffix(host, "."+d) {
			return true
		}
	}
	return false
}

// publicIP rejects loopback, private, link-local, multicast, and unspecified
// addresses. DNS rebinding between check and connect remains a residual risk
// (documented in THREAT_MODEL); full egress proxying is future work.
func publicIP(host string) error {
	if os.Getenv("AG_ALLOW_LOOPBACK") == "1" {
		return nil // tests only: httptest servers are loopback
	}
	addrs, err := net.DefaultResolver.LookupIPAddr(context.Background(), host)
	if err != nil || len(addrs) == 0 {
		return fmt.Errorf("cannot resolve %s", host)
	}
	for _, a := range addrs {
		ip := a.IP
		if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
			ip.IsLinkLocalMulticast() || ip.IsMulticast() || ip.IsUnspecified() {
			return fmt.Errorf("destination %s resolves to non-public address", host)
		}
	}
	return nil
}

// httpRequestHandler proxies outbound calls to an operator-managed domain
// allowlist (integration config "http": {"domains": [...], "methods": [...]}).
// No allowlist means deny. GET/HEAD are READ_ONLY; other methods are
// EXTERNAL_COMMUNICATION with unknown side effects.
func httpRequestHandler(st store.Store) actions.ToolHandler {
	return func(ctx context.Context, a domain.TxnAction) (map[string]any, error) {
		cfg, _ := st.GetIntegrationConfig(a.OrgID, "http")
		var domains, methods []string
		if cfg != nil {
			domains = toStrings(cfg["domains"])
			methods = toStrings(cfg["methods"])
		}
		if len(methods) == 0 {
			methods = []string{"GET", "HEAD"}
		}
		method := "GET"
		if m, ok := strArg(a.Arguments, "method"); ok {
			method = strings.ToUpper(m)
		}
		allowed := false
		for _, m := range methods {
			if m == method {
				allowed = true
			}
		}
		if !allowed {
			return nil, fmt.Errorf("http.request: method %s not in org allowlist %v", method, methods)
		}
		raw, ok := strArg(a.Arguments, "url")
		if !ok {
			return nil, fmt.Errorf("http.request needs a url")
		}
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return nil, fmt.Errorf("http.request needs an http(s) url")
		}
		if !domainAllowed(u.Hostname(), domains) {
			return nil, fmt.Errorf("http.request: host %s not in org allowlist (request access first)", u.Hostname())
		}
		if err := publicIP(u.Hostname()); err != nil {
			return nil, fmt.Errorf("http.request refused: %v", err)
		}
		req, err := http.NewRequestWithContext(ctx, method, raw, nil)
		if err != nil {
			return nil, err
		}
		resp, err := httpClient.Do(req)
		if err != nil {
			return nil, actions.Uncertain(fmt.Errorf("no response from %s: %w", u.Hostname(), err))
		}
		defer resp.Body.Close()
		rawBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		out := map[string]any{"status": resp.StatusCode, "body": string(rawBody)}
		if method != "GET" && method != "HEAD" {
			out["warning"] = "non-idempotent method: side effects unknown, treat as EXTERNAL_COMMUNICATION"
		}
		return out, nil
	}
}

func toStrings(v any) []string {
	var out []string
	if list, ok := v.([]any); ok {
		for _, item := range list {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
	}
	return out
}

func httpClasses() map[string][]domain.ActionClass {
	return map[string][]domain.ActionClass{
		"request": {domain.ClassReadOnly, domain.ClassExternalCommunication},
	}
}

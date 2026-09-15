package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/settleagent/settleagent/internal/store"
	"golang.org/x/crypto/bcrypt"
)

const (
	KindAgent    = "agent"
	KindOperator = "operator"
	KindLegacy   = "legacy"
)

// Identity is the verified caller for a request.
type Identity struct {
	OrgID    string
	AgentID  string // empty for operators
	Operator bool
	Name     string // operator token name, or agent name lookup is caller's job
	KeyID    string // credential key id (for rotation, revocation, last-used)
}

type ctxKey struct{}

func IdentityFrom(ctx context.Context) (Identity, bool) {
	id, ok := ctx.Value(ctxKey{}).(Identity)
	return id, ok
}

func randHex(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// NewAgentSecret returns keyID, full secret, bcrypt hash.
func NewAgentSecret() (keyID, secret, hash string, err error) {
	kid, err := randHex(6)
	if err != nil {
		return "", "", "", err
	}
	r, err := randHex(24)
	if err != nil {
		return "", "", "", err
	}
	secret = "st_" + kid + "_" + r
	h, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.DefaultCost)
	if err != nil {
		return "", "", "", err
	}
	return kid, secret, string(h), nil
}

// NewOperatorSecret returns keyID, full secret, bcrypt hash.
func NewOperatorSecret() (keyID, secret, hash string, err error) {
	kid, err := randHex(6)
	if err != nil {
		return "", "", "", err
	}
	r, err := randHex(24)
	if err != nil {
		return "", "", "", err
	}
	secret = "sto_" + kid + "_" + r
	h, err := bcrypt.GenerateFromPassword([]byte(secret), bcrypt.DefaultCost)
	if err != nil {
		return "", "", "", err
	}
	return kid, secret, string(h), nil
}

func check(secret, hash string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(secret)) == nil
}

// Authenticate verifies a bearer secret. Legacy pre-key-ID agent secrets
// (st_<hex>) need the org passed explicitly since they are not
// self-identifying; keyed secrets ignore it.
func Authenticate(s store.Store, secret, headerOrg string) (Identity, bool) {
	if strings.HasPrefix(secret, "sto_") {
		rest := strings.TrimPrefix(secret, "sto_")
		kid, _, found := strings.Cut(rest, "_")
		if !found {
			return Identity{}, false
		}
		org, name, hash, ok := s.GetOperatorToken(kid)
		if !ok || !check(secret, hash) {
			return Identity{}, false
		}
		s.TouchOperatorToken(kid)
		return Identity{OrgID: org, Operator: true, Name: name, KeyID: kid}, true
	}
	if strings.HasPrefix(secret, "st_") {
		rest := strings.TrimPrefix(secret, "st_")
		if kid, _, found := strings.Cut(rest, "_"); found {
			org, agent, hash, ok := s.GetCredential(kid)
			if !ok || !check(secret, hash) {
				return Identity{}, false
			}
			s.TouchCredential(kid)
			return Identity{OrgID: org, AgentID: agent, KeyID: kid}, true
		}
		// Legacy secret: bounded per-org scan, needs X-Org-ID.
		if headerOrg == "" {
			return Identity{}, false
		}
		for agentID, hash := range s.AgentCredentialHashes(headerOrg) {
			if check(secret, hash) {
				return Identity{OrgID: headerOrg, AgentID: agentID}, true
			}
		}
	}
	return Identity{}, false
}

// Limiter is a per-key fixed-window rate limiter (single process;
// a multi-replica deployment needs a shared bucket store).
type Limiter struct {
	mu     sync.Mutex
	hits   map[string]*bucket
	max    int
	window time.Duration
}

type bucket struct {
	count int
	start time.Time
}

func NewLimiter(max int, window time.Duration) *Limiter {
	return &Limiter{hits: map[string]*bucket{}, max: max, window: window}
}

func (l *Limiter) Allow(key string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	b, ok := l.hits[key]
	if !ok || now.Sub(b.start) >= l.window {
		l.hits[key] = &bucket{count: 1, start: now}
		return true
	}
	if b.count >= l.max {
		return false
	}
	b.count++
	return true
}

// Middleware requires a valid bearer credential on every route except
// health and openapi. Verified identity lands in the request context;
// the tenant org comes from the credential, never from headers.
func Middleware(s store.Store, l *Limiter, next http.Handler) http.Handler {
	open := map[string]bool{"/health": true, "/openapi.json": true}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if open[r.URL.Path] {
			next.ServeHTTP(w, r)
			return
		}
		h := r.Header.Get("Authorization")
		secret := strings.TrimPrefix(h, "Bearer ")
		if h == "" || secret == h {
			writeAuthError(w, "missing_credentials", "Pass Authorization: Bearer <agent or operator secret>.")
			return
		}
		limitKey := secret
		if len(limitKey) > 32 {
			limitKey = limitKey[:32]
		}
		if !l.Allow(limitKey) {
			writeAuthError(w, "rate_limited", "")
			return
		}
		id, ok := Authenticate(s, secret, r.Header.Get("X-Org-ID"))
		if !ok {
			writeAuthError(w, "invalid_credentials", "Unknown, revoked, or mismatched credential.")
			return
		}
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), ctxKey{}, id)))
	})
}

func writeAuthError(w http.ResponseWriter, code, why string) {
	if code == "rate_limited" {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"error":"rate_limited","why":"Too many requests; slow down and retry."}`))
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	_, _ = w.Write([]byte(`{"error":"` + code + `","why":"` + why + `"}`))
}

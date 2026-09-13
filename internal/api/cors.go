package api

import (
	"net/http"
	"os"
	"strings"
)

// CORS for browser dashboard access. Allowed origins come from
// AG_CORS_ORIGINS (comma-separated, default localhost:3000 variants).
// Preflight (OPTIONS) is answered here, before auth, since browsers never
// attach credentials to preflight requests.
func corsMiddleware(next http.Handler) http.Handler {
	origins := map[string]bool{}
	for _, o := range strings.Split(os.Getenv("AG_CORS_ORIGINS"), ",") {
		if o = strings.TrimSpace(o); o != "" {
			origins[o] = true
		}
	}
	if len(origins) == 0 {
		for _, o := range []string{"http://localhost:3000", "http://127.0.0.1:3000"} {
			origins[o] = true
		}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origins[origin] {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type, X-Org-ID, X-Request-ID")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		}
		if r.Method == "OPTIONS" {
			w.WriteHeader(204)
			return
		}
		next.ServeHTTP(w, r)
	})
}

package main

import (
	"log"
	"net/http"
	"os"

	"github.com/agentguard/agentguard/internal/api"
	"github.com/agentguard/agentguard/internal/policies"
	"github.com/agentguard/agentguard/internal/store"
)

func main() {
	s := store.New()
	org, principal := s.SeedOrg("Acme Corp")
	s.SetPolicies(org.ID, policies.DefaultSupportPolicies(org.ID))
	srv := api.New(s)
	addr := os.Getenv("ADDR")
	if addr == "" {
		addr = ":8080"
	}
	log.Printf("agentguard api on %s (demo org %s principal %s)", addr, org.ID, principal.ID)
	log.Fatal(http.ListenAndServe(addr, srv.Handler()))
}

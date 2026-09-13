package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/agentguard/agentguard/internal/api"
	"github.com/agentguard/agentguard/internal/auth"
	"github.com/agentguard/agentguard/internal/keys"
	"github.com/agentguard/agentguard/internal/observe"
	"github.com/agentguard/agentguard/internal/policies"
	"github.com/agentguard/agentguard/internal/store"
)

// mustOperatorToken mints the boot operator credential (shown once in logs).
func mustOperatorToken(s store.Store, orgID string) string {
	kid, secret, hash, err := auth.NewOperatorSecret()
	if err != nil {
		log.Fatalf("operator token: %v", err)
	}
	s.CreateOperatorToken(orgID, "boot-operator", kid, hash)
	return secret
}

func main() {
	ctx := context.Background()
	var backend store.Store
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL != "" {
		pg, err := store.Open(ctx, dbURL)
		if err != nil {
			log.Fatalf("postgres: %v", err)
		}
		defer pg.Close()
		if err := store.Migrate(ctx, pg.Pool()); err != nil {
			log.Fatalf("migrate: %v", err)
		}
		if counts := pg.Recover(); len(counts) > 0 {
			log.Printf("recovered incomplete transactions: %v", counts)
		}
		if len(pg.OrgIDs()) == 0 {
			org, principal := pg.SeedOrg("Acme Corp")
			pg.SetPolicies(org.ID, policies.DefaultSupportPolicies(org.ID))
			log.Printf("seeded demo org %s principal %s", org.ID, principal.ID)
			log.Printf("operator token (dashboard/human): %s", mustOperatorToken(pg, org.ID))
		} else {
			log.Printf("existing orgs: %v", pg.OrgIDs())
		}
		backend = pg
		log.Print("store: postgres")
	} else {
		mem := store.New()
		org, principal := mem.SeedOrg("Acme Corp")
		mem.SetPolicies(org.ID, policies.DefaultSupportPolicies(org.ID))
		backend = mem
		log.Printf("store: memory (demo org %s principal %s)", org.ID, principal.ID)
		log.Printf("operator token (dashboard/human): %s", mustOperatorToken(mem, org.ID))
	}
	apiSrv := api.New(backend)
	if signer, persistent, err := keys.Load(); err != nil {
		log.Fatalf("signing keys: %v", err)
	} else {
		apiSrv.SetSigner(signer)
		log.Printf("receipt signing: key %s (persistent=%v)", signer.KeyID(), persistent)
	}
	addr := os.Getenv("ADDR")
	if addr == "" {
		if p := os.Getenv("PORT"); p != "" {
			addr = ":" + p
		} else {
			addr = ":8080"
		}
	}
	log.Printf("agentguard api on %s", addr)
	shutdownTracing, err := observe.Init(ctx)
	if err != nil {
		log.Fatalf("tracing: %v", err)
	}
	srv := &http.Server{Addr: addr, Handler: apiSrv.Handler()}
	go func() {
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("serve: %v", err)
		}
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	log.Print("shutting down")
	_ = srv.Shutdown(ctx)
	_ = shutdownTracing(ctx)
}

package gateway_test

import (
	"context"
	"testing"

	"github.com/agentguard/agentguard/internal/actions"
	"github.com/agentguard/agentguard/internal/domain"
	"github.com/agentguard/agentguard/internal/gateway"
	"github.com/agentguard/agentguard/internal/policies"
	"github.com/agentguard/agentguard/internal/store"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

// TestSpansEmitted proves the gateway emits the documented spans with the
// attributes the dashboard/Jaeger views depend on.
func TestSpansEmitted(t *testing.T) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	otel.SetTracerProvider(tp)
	defer otel.SetTracerProvider(nil)

	s := store.New()
	org, principal := s.SeedOrg("Trace Co")
	s.SetPolicies(org.ID, policies.DefaultSupportPolicies(org.ID))
	g := gateway.NewService(s, actions.DefaultRegistry())
	ag := s.CreateAgent(org.ID, principal.ID, "support-agent-14", "production", []string{"support"}, "hash")
	tx := s.CreateTxn(domain.Transaction{OrgID: org.ID, AgentID: ag.ID, PrincipalID: principal.ID, SessionID: "s", Objective: "o", Status: domain.TxnPlanning})

	ctx := context.Background()
	a, err := g.ProposeAction(ctx, org.ID, tx.ID, ag.ID, "stripe", "refund", map[string]any{"amount_cents": 8000}, "tr-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.ExecuteAllowed(ctx, org.ID, a.ID); err != nil {
		t.Fatal(err)
	}

	names := map[string]map[string]string{}
	for _, sp := range sr.Ended() {
		attrs := map[string]string{}
		for _, a := range sp.Attributes() {
			attrs[string(a.Key)] = a.Value.AsString()
		}
		names[sp.Name()] = attrs
	}
	eval, ok := names["policy.evaluate"]
	if !ok || eval["effect"] != "ALLOW" || eval["tool"] != "stripe" {
		t.Fatalf("policy.evaluate span missing/wrong: %v", names)
	}
	exec, ok := names["action.execute"]
	if !ok || exec["tool"] != "stripe" || exec["txn"] != tx.ID {
		t.Fatalf("action.execute span missing/wrong: %v", names)
	}
}

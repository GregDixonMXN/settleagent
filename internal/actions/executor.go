package actions

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/settleagent/settleagent/internal/domain"
)

// ToolHandler executes a tool action (mock/sandbox in MVP). Real
// integrations implement this interface; never fake rollback.
type ToolHandler func(ctx context.Context, action domain.TxnAction) (map[string]any, error)

// UncertainError marks failures where the side effect may or may not have
// happened (timeout, connection lost). These must be reconciled, never
// blindly retried: a retry could duplicate a payment, email, or deletion.
type UncertainError struct{ Err error }

func (e *UncertainError) Error() string { return "execution uncertain: " + e.Err.Error() }
func (e *UncertainError) Unwrap() error { return e.Err }

func Uncertain(err error) *UncertainError { return &UncertainError{Err: err} }

// Reconciler settles an uncertain action by querying the provider with the
// action's idempotency key. found=true with a result confirms execution;
// found=false confirms it never happened.
type Reconciler func(ctx context.Context, action domain.TxnAction) (found bool, result map[string]any, err error)

// Compensator undoes a previously executed action where possible.
type Compensator func(ctx context.Context, action domain.TxnAction) error

type Registry struct {
	mu           sync.RWMutex
	handlers     map[string]ToolHandler
	compensators map[string]Compensator
	reconcilers  map[string]Reconciler
	meta         map[string][]domain.ActionClass
}

func key(tool, action string) string { return tool + "." + action }

// shortKey never panics on attacker- or test-controlled short keys.
func shortKey(s string, n int) string {
	if len(s) < n {
		return s
	}
	return s[:n]
}

func DefaultRegistry() *Registry {
	r := &Registry{
		handlers:     map[string]ToolHandler{},
		compensators: map[string]Compensator{},
		reconcilers:  map[string]Reconciler{},
		meta: map[string][]domain.ActionClass{
			"crm.lookup_customer":      {domain.ClassReadOnly},
			"crm.update_record":        {domain.ClassReversible},
			"stripe.refund":            {domain.ClassFinancial, domain.ClassCompensable},
			"email.send":               {domain.ClassExternalCommunication, domain.ClassIrreversible},
			"fs.read":                  {domain.ClassReadOnly},
			"fs.write":                 {domain.ClassReversible},
			"shell.exec":               {domain.ClassPrivileged, domain.ClassDestructive},
			"postgres.delete_database": {domain.ClassDestructive, domain.ClassPrivileged, domain.ClassIrreversible},
			"github.create_issue":      {domain.ClassReversible},
			"http.fetch":               {domain.ClassReadOnly},
			"deploy.deploy":            {domain.ClassPrivileged, domain.ClassCompensable},
			"iam.grant":                {domain.ClassPrivileged, domain.ClassDestructive},
		},
	}
	ok := func(result map[string]any) ToolHandler {
		return func(ctx context.Context, a domain.TxnAction) (map[string]any, error) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(5 * time.Millisecond):
			}
			return result, nil
		}
	}
	r.handlers["crm.lookup_customer"] = ok(map[string]any{"customer": "cust_9182", "tier": "pro"})
	r.handlers["crm.update_record"] = ok(map[string]any{"updated": true})
	r.handlers["stripe.refund"] = func(ctx context.Context, a domain.TxnAction) (map[string]any, error) {
		amt, _ := a.Arguments["amount_cents"]
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(5 * time.Millisecond):
		}
		// Mock failure injection for compensation demo.
		if m, ok := a.Arguments["__fail_after"]; ok && m == true {
			return nil, fmt.Errorf("injected downstream failure after refund")
		}
		return map[string]any{"refunded_cents": amt, "refund_id": "re_" + shortKey(a.IdempotencyKey, 8)}, nil
	}
	r.handlers["email.send"] = ok(map[string]any{"sent": true})
	r.handlers["fs.read"] = ok(map[string]any{"content": "(mock file)"})
	r.handlers["fs.write"] = ok(map[string]any{"written": true})
	r.handlers["http.fetch"] = ok(map[string]any{"status": 200})
	r.handlers["github.create_issue"] = ok(map[string]any{"issue": 1})
	r.handlers["shell.exec"] = func(ctx context.Context, a domain.TxnAction) (map[string]any, error) {
		return nil, fmt.Errorf("shell.exec requires explicit approval and is disabled in sandbox demo")
	}
	r.handlers["postgres.delete_database"] = func(ctx context.Context, a domain.TxnAction) (map[string]any, error) {
		return nil, fmt.Errorf("refusing to execute destructive db delete in demo without approval path")
	}
	r.handlers["deploy.deploy"] = ok(map[string]any{"deployed": true})
	r.handlers["iam.grant"] = func(ctx context.Context, a domain.TxnAction) (map[string]any, error) {
		return nil, fmt.Errorf("iam.grant is never executed by policy")
	}
	r.compensators["stripe.refund"] = func(ctx context.Context, a domain.TxnAction) error {
		return nil // mock: reverse refund
	}
	r.compensators["crm.update_record"] = func(ctx context.Context, a domain.TxnAction) error {
		return nil // mock: restore prior record
	}
	r.compensators["fs.write"] = func(ctx context.Context, a domain.TxnAction) error { return nil }
	r.compensators["deploy.deploy"] = func(ctx context.Context, a domain.TxnAction) error { return nil }
	r.compensators["github.create_issue"] = func(ctx context.Context, a domain.TxnAction) error { return nil }
	return r
}

func (r *Registry) Classes(tool, action string) []domain.ActionClass {
	r.mu.RLock()
	defer r.mu.RUnlock()
	if c, ok := r.meta[key(tool, action)]; ok {
		return c
	}
	if strings.HasPrefix(tool, "crm.") || tool == "crm" {
		return []domain.ActionClass{domain.ClassUnknown}
	}
	return []domain.ActionClass{domain.ClassUnknown}
}

func (r *Registry) Execute(ctx context.Context, a domain.TxnAction) (map[string]any, error) {
	r.mu.RLock()
	h, ok := r.handlers[key(a.Tool, a.Action)]
	r.mu.RUnlock()
	if !ok {
		return nil, fmt.Errorf("unknown tool action %s.%s", a.Tool, a.Action)
	}
	return h(ctx, a)
}

func (r *Registry) Compensate(ctx context.Context, a domain.TxnAction) error {
	r.mu.RLock()
	c, ok := r.compensators[key(a.Tool, a.Action)]
	r.mu.RUnlock()
	if !ok {
		return fmt.Errorf("no compensation defined for %s.%s (treated as irreversible)", a.Tool, a.Action)
	}
	return c(ctx, a)
}

// RegisterReconciler attaches a settler for uncertain executions of a tool.
func (r *Registry) RegisterReconciler(tool, action string, rec Reconciler) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.reconcilers[key(tool, action)] = rec
}

func (r *Registry) ReconcilerFor(tool, action string) (Reconciler, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rec, ok := r.reconcilers[key(tool, action)]
	return rec, ok
}

// Register adds or replaces a tool action at runtime (used by the MCP proxy
// when a server is registered). Safe for concurrent use.
func (r *Registry) Register(tool, action string, classes []domain.ActionClass, h ToolHandler, c Compensator) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if len(classes) == 0 {
		classes = []domain.ActionClass{domain.ClassUnknown}
	}
	r.meta[key(tool, action)] = classes
	if h != nil {
		r.handlers[key(tool, action)] = h
	}
	if c != nil {
		r.compensators[key(tool, action)] = c
	}
}

func IsReversible(a domain.TxnAction) bool {
	for _, c := range a.Classes {
		if c == domain.ClassReversible {
			return true
		}
	}
	return false
}

func IsCompensable(a domain.TxnAction) bool {
	for _, c := range a.Classes {
		if c == domain.ClassCompensable || c == domain.ClassReversible {
			return true
		}
	}
	return false
}

func IsIrreversible(a domain.TxnAction) bool {
	for _, c := range a.Classes {
		if c == domain.ClassIrreversible || c == domain.ClassDestructive {
			return true
		}
	}
	return false
}

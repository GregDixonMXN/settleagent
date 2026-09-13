package store

import (
	"fmt"
	"sync"
	"time"

	"github.com/agentguard/agentguard/internal/domain"
	"github.com/agentguard/agentguard/internal/receipts"
	"github.com/google/uuid"
)

type MemoryStore struct {
	mu sync.Mutex

	orgs         map[string]domain.Organization
	principals   map[string]domain.Principal
	agents       map[string]domain.Agent
	credentials  map[string]string // agentID -> bcrypt hash (never plaintext)
	policies     map[string][]domain.PolicyRule
	txns         map[string]*domain.Transaction
	actions      map[string]*domain.TxnAction
	byIdem       map[string]*domain.TxnAction // orgID|key -> action
	approvals    map[string]*domain.Approval
	receiptsList []*domain.Receipt
	lastHash     map[string]string // orgID -> last receipt hash
	audit        []domain.AuditEvent
}

func New() *MemoryStore {
	return &MemoryStore{
		orgs:        map[string]domain.Organization{},
		principals:  map[string]domain.Principal{},
		agents:      map[string]domain.Agent{},
		credentials: map[string]string{},
		policies:    map[string][]domain.PolicyRule{},
		txns:        map[string]*domain.Transaction{},
		actions:     map[string]*domain.TxnAction{},
		byIdem:      map[string]*domain.TxnAction{},
		approvals:   map[string]*domain.Approval{},
		lastHash:    map[string]string{},
	}
}

func uid() string    { return uuid.NewString() }
func now() time.Time { return time.Now().UTC() }

func (s *MemoryStore) SeedOrg(name string) (domain.Organization, domain.Principal) {
	s.mu.Lock()
	defer s.mu.Unlock()
	o := domain.Organization{ID: uid(), Name: name, CreatedAt: now()}
	p := domain.Principal{ID: uid(), OrgID: o.ID, Type: domain.PrincipalOrganization, Name: name, CreatedAt: now()}
	s.orgs[o.ID] = o
	s.principals[p.ID] = p
	return o, p
}

func (s *MemoryStore) CreateAgent(orgID, principalID, name, env string, groups []string, secretHash string) domain.Agent {
	s.mu.Lock()
	defer s.mu.Unlock()
	a := domain.Agent{ID: uid(), OrgID: orgID, PrincipalID: principalID, Name: name, Groups: groups, Environment: env, Status: "active", CreatedAt: now()}
	s.agents[a.ID] = a
	s.credentials[a.ID] = secretHash
	return a
}

func (s *MemoryStore) GetAgent(orgID, agentID string) (domain.Agent, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.agents[agentID]
	if !ok || a.OrgID != orgID {
		return domain.Agent{}, false
	}
	return a, true
}

func (s *MemoryStore) CredentialHash(agentID string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	h, ok := s.credentials[agentID]
	return h, ok
}

func (s *MemoryStore) SetPolicies(orgID string, rules []domain.PolicyRule) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.policies[orgID] = rules
}

func (s *MemoryStore) Policies(orgID string) []domain.PolicyRule {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]domain.PolicyRule(nil), s.policies[orgID]...)
}

func (s *MemoryStore) CreateTxn(t domain.Transaction) *domain.Transaction {
	s.mu.Lock()
	defer s.mu.Unlock()
	t.ID = uid()
	t.CreatedAt = now()
	t.UpdatedAt = t.CreatedAt
	if t.Status == "" {
		t.Status = domain.TxnCreated
	}
	cp := t
	s.txns[t.ID] = &cp
	s.emitLocked(domain.AuditEvent{ID: uid(), OrgID: t.OrgID, TransactionID: t.ID, ActorType: "agent", ActorID: t.AgentID, Type: "transaction.created", CreatedAt: now()})
	return &cp
}

func (s *MemoryStore) GetTxn(orgID, id string) (*domain.Transaction, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.txns[id]
	if !ok || t.OrgID != orgID {
		return nil, false
	}
	cp := *t
	return &cp, true
}

func (s *MemoryStore) SetTxnStatus(orgID, id string, st domain.TxnStatus) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.txns[id]
	if !ok || t.OrgID != orgID {
		return fmt.Errorf("transaction not found")
	}
	t.Status = st
	t.UpdatedAt = now()
	return nil
}

func (s *MemoryStore) ListTxns(orgID string) []domain.Transaction {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.Transaction
	for _, t := range s.txns {
		if t.OrgID == orgID {
			out = append(out, *t)
		}
	}
	return out
}

func (s *MemoryStore) AddAction(a domain.TxnAction) (*domain.TxnAction, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	key := a.OrgID + "|" + a.IdempotencyKey
	if prev, ok := s.byIdem[key]; ok {
		cp := *prev
		return &cp, true
	}
	a.ID = uid()
	a.CreatedAt = now()
	if a.Status == "" {
		a.Status = "proposed"
	}
	a.ArgumentsHash = receipts.Canonical(a.Arguments)
	cp := a
	s.actions[a.ID] = &cp
	s.byIdem[key] = &cp
	return &cp, false
}

func (s *MemoryStore) GetAction(orgID, id string) (*domain.TxnAction, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.actions[id]
	if !ok || a.OrgID != orgID {
		return nil, false
	}
	cp := *a
	return &cp, true
}

func (s *MemoryStore) UpdateAction(a *domain.TxnAction) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if cur, ok := s.actions[a.ID]; ok {
		*cur = *a
		s.byIdem[a.OrgID+"|"+a.IdempotencyKey] = cur
	}
}

func (s *MemoryStore) ActionsForTxn(orgID, txnID string) []domain.TxnAction {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.TxnAction
	for _, a := range s.actions {
		if a.OrgID == orgID && a.TransactionID == txnID {
			out = append(out, *a)
		}
	}
	return out
}

func (s *MemoryStore) CreateApproval(a domain.Approval) *domain.Approval {
	s.mu.Lock()
	defer s.mu.Unlock()
	a.ID = uid()
	a.CreatedAt = now()
	a.Status = "pending"
	cp := a
	s.approvals[a.ID] = &cp
	return &cp
}

func (s *MemoryStore) GetApproval(orgID, id string) (*domain.Approval, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.approvals[id]
	if !ok || a.OrgID != orgID {
		return nil, false
	}
	cp := *a
	return &cp, true
}

func (s *MemoryStore) DecideApproval(orgID, id, by string, approve bool) (*domain.Approval, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	a, ok := s.approvals[id]
	if !ok || a.OrgID != orgID || a.Status != "pending" {
		return nil, false
	}
	t := now()
	if approve {
		a.Status = "approved"
	} else {
		a.Status = "denied"
	}
	a.DecidedBy = by
	a.DecidedAt = &t
	cp := *a
	return &cp, true
}

func (s *MemoryStore) PendingApprovals(orgID string) []domain.Approval {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.Approval
	for _, a := range s.approvals {
		if a.OrgID == orgID && a.Status == "pending" {
			out = append(out, *a)
		}
	}
	return out
}

func (s *MemoryStore) AppendReceipt(r domain.Receipt) *domain.Receipt {
	s.mu.Lock()
	defer s.mu.Unlock()
	r.ID = uid()
	r.PrevHash = s.lastHash[r.OrgID]
	payload := map[string]any{
		"txn": r.TransactionID, "action": r.ActionID, "tool": r.Tool,
		"args_hash": r.ArgumentsHash, "result_hash": r.ResultHash,
		"decision": r.Decision, "started": r.StartedAt, "completed": r.CompletedAt,
	}
	r.Hash = receipts.ChainHash(r.PrevHash, payload)
	cp := r
	s.receiptsList = append(s.receiptsList, &cp)
	s.lastHash[r.OrgID] = r.Hash
	return &cp
}

func (s *MemoryStore) ReceiptsForTxn(orgID, txnID string) []domain.Receipt {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.Receipt
	for _, r := range s.receiptsList {
		if r.OrgID == orgID && r.TransactionID == txnID {
			out = append(out, *r)
		}
	}
	return out
}

func (s *MemoryStore) Emit(e domain.AuditEvent) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e.ID = uid()
	e.CreatedAt = now()
	s.audit = append(s.audit, e)
}

func (s *MemoryStore) emitLocked(e domain.AuditEvent) {
	s.audit = append(s.audit, e)
}

func (s *MemoryStore) Audit(orgID, txnID string, limit int) []domain.AuditEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []domain.AuditEvent
	for i := len(s.audit) - 1; i >= 0 && len(out) < limit; i-- {
		e := s.audit[i]
		if e.OrgID != orgID {
			continue
		}
		if txnID != "" && e.TransactionID != txnID {
			continue
		}
		out = append(out, e)
	}
	return out
}

// VerifyChain recomputes the org receipt chain; returns first broken index or -1.
func (s *MemoryStore) VerifyChain(orgID string) int {
	s.mu.Lock()
	defer s.mu.Unlock()
	prev := ""
	for i, r := range s.receiptsList {
		if r.OrgID != orgID {
			continue
		}
		payload := map[string]any{
			"txn": r.TransactionID, "action": r.ActionID, "tool": r.Tool,
			"args_hash": r.ArgumentsHash, "result_hash": r.ResultHash,
			"decision": r.Decision, "started": r.StartedAt, "completed": r.CompletedAt,
		}
		if r.PrevHash != prev || r.Hash != receipts.ChainHash(prev, payload) {
			return i
		}
		prev = r.Hash
	}
	return -1
}

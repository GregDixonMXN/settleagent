package store

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/GregDixonMXN/settleagent/internal/domain"
	"github.com/GregDixonMXN/settleagent/internal/keys"
	"github.com/GregDixonMXN/settleagent/internal/receipts"
)

type MemoryStore struct {
	mu sync.Mutex

	orgs         map[string]domain.Organization
	principals   map[string]domain.Principal
	agents       map[string]domain.Agent
	credentials  map[string]string // agentID -> bcrypt hash (never plaintext)
	credByKey    map[string]credRef
	opTokens     map[string]opToken
	mcpServers   map[string]mcpEntry
	grants       map[string]*domain.AuthorityGrant
	policySets   map[string][]domain.PolicySet
	integCreds   map[string]string
	integConfig  map[string]map[string]any
	keyProvider  keys.Provider
	policies     map[string][]domain.PolicyRule
	txns         map[string]*domain.Transaction
	actions      map[string]*domain.TxnAction
	byIdem       map[string]*domain.TxnAction // orgID|key -> action
	approvals    map[string]*domain.Approval
	receiptsList []*domain.Receipt
	lastHash     map[string]string // orgID -> last receipt hash
	audit        []domain.AuditEvent
}

type credRef struct {
	orgID, agentID, hash string
	revoked              bool
	lastUsed             time.Time
}

type opToken struct {
	orgID, name, hash string
	revoked           bool
	lastUsed          time.Time
}

type mcpEntry struct {
	server domain.MCPServer
	token  string
}

func New() *MemoryStore {
	return &MemoryStore{
		credByKey:   map[string]credRef{},
		opTokens:    map[string]opToken{},
		mcpServers:  map[string]mcpEntry{},
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
	s.CreatePolicySet(orgID, rules, "system")
	s.ActivatePolicySet(orgID, s.latestVersionLocked(orgID))
}

func (s *MemoryStore) latestVersionLocked(orgID string) int {
	max := 0
	for _, p := range s.policySets[orgID] {
		if p.Version > max {
			max = p.Version
		}
	}
	return max
}

func (s *MemoryStore) Policies(orgID string) []domain.PolicyRule {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.policySets[orgID] {
		if p.Status == "active" {
			return append([]domain.PolicyRule(nil), p.Rules...)
		}
	}
	return append([]domain.PolicyRule(nil), s.policies[orgID]...)
}

func (s *MemoryStore) CreatePolicySet(orgID string, rules []domain.PolicyRule, createdBy string) domain.PolicySet {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.policySets == nil {
		s.policySets = map[string][]domain.PolicySet{}
	}
	set := domain.PolicySet{
		ID: uid(), OrgID: orgID, Version: s.latestVersionLocked(orgID) + 1,
		Rules:  append([]domain.PolicyRule(nil), rules...),
		Status: "draft", CreatedBy: createdBy, CreatedAt: now(),
	}
	s.policySets[orgID] = append(s.policySets[orgID], set)
	return set
}

func (s *MemoryStore) ActivatePolicySet(orgID string, version int) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	found := false
	for _, p := range s.policySets[orgID] {
		if p.Version == version {
			found = true
		}
	}
	if !found {
		return false
	}
	for i := range s.policySets[orgID] {
		if s.policySets[orgID][i].Version == version {
			s.policySets[orgID][i].Status = "active"
		} else if s.policySets[orgID][i].Status == "active" {
			s.policySets[orgID][i].Status = "disabled"
		}
	}
	return true
}

func (s *MemoryStore) PolicySets(orgID string) []domain.PolicySet {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]domain.PolicySet(nil), s.policySets[orgID]...)
	return out
}

func (s *MemoryStore) ActivePolicySet(orgID string) (domain.PolicySet, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, p := range s.policySets[orgID] {
		if p.Status == "active" {
			return p, true
		}
	}
	return domain.PolicySet{}, false
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
	// Idempotency is scoped to (org, transaction): the same key in a new
	// transaction is a different action, never a replay of another txn's.
	key := a.OrgID + "|" + a.TransactionID + "|" + a.IdempotencyKey
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
		s.byIdem[a.OrgID+"|"+a.TransactionID+"|"+a.IdempotencyKey] = cur
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

// UpdateReceipt persists post-append receipt fields (key_id, signature).
// The chain hash never changes, so ordering is unaffected.
func (s *MemoryStore) UpdateReceipt(r *domain.Receipt) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, cur := range s.receiptsList {
		if cur.ID == r.ID && cur.OrgID == r.OrgID {
			cur.KeyID = r.KeyID
			cur.Signature = r.Signature
		}
	}
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

func (s *MemoryStore) StoreCredential(orgID, agentID, keyID, secretHash string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.credByKey[keyID] = credRef{orgID: orgID, agentID: agentID, hash: secretHash}
	s.credentials[agentID] = secretHash
}

func (s *MemoryStore) GetCredential(keyID string) (string, string, string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.credByKey[keyID]
	if !ok || c.revoked {
		return "", "", "", false
	}
	return c.orgID, c.agentID, c.hash, true
}

func (s *MemoryStore) AgentCredentialHashes(orgID string) map[string]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]string{}
	// Legacy pre-key-ID hashes (no revocation tracking; rotation replaces).
	for id, a := range s.agents {
		if a.OrgID == orgID {
			if h, ok := s.credentials[id]; ok {
				out[id] = h
			}
		}
	}
	// Keyed credentials override; revoked ones are excluded.
	for _, c := range s.credByKey {
		if c.orgID != orgID {
			continue
		}
		if c.revoked {
			delete(out, c.agentID)
		} else {
			out[c.agentID] = c.hash
		}
	}
	return out
}

func (s *MemoryStore) CreateOperatorToken(orgID, name, keyID, secretHash string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.opTokens[keyID] = opToken{orgID: orgID, name: name, hash: secretHash}
}

func (s *MemoryStore) GetOperatorToken(keyID string) (string, string, string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.opTokens[keyID]
	if !ok || t.revoked {
		return "", "", "", false
	}
	return t.orgID, t.name, t.hash, true
}

func mcpKey(orgID, name string) string { return orgID + "|" + name }

func (s *MemoryStore) UpsertMCPServer(srv domain.MCPServer, authToken string) domain.MCPServer {
	s.mu.Lock()
	defer s.mu.Unlock()
	if srv.ID == "" {
		srv.ID = uid()
	}
	srv.CreatedAt = now()
	srv.HasToken = authToken != ""
	s.mcpServers[mcpKey(srv.OrgID, srv.Name)] = mcpEntry{server: srv, token: s.sealLocked(authToken)}
	return srv
}

func (s *MemoryStore) GetMCPServer(orgID, name string) (domain.MCPServer, string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e, ok := s.mcpServers[mcpKey(orgID, name)]
	if !ok {
		return domain.MCPServer{}, "", false
	}
	token, ok := s.openLocked(e.token)
	if !ok {
		return domain.MCPServer{}, "", false
	}
	return e.server, token, true
}

func (s *MemoryStore) ListMCPServers(orgID string) []domain.MCPServer {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []domain.MCPServer{}
	for _, e := range s.mcpServers {
		if e.server.OrgID == orgID {
			out = append(out, e.server)
		}
	}
	return out
}

func (s *MemoryStore) CreateGrant(g domain.AuthorityGrant) domain.AuthorityGrant {
	s.mu.Lock()
	defer s.mu.Unlock()
	if g.ID == "" {
		g.ID = uid()
	}
	g.IssuedAt = now()
	if s.grants == nil {
		s.grants = map[string]*domain.AuthorityGrant{}
	}
	cp := g
	s.grants[g.ID] = &cp
	return cp
}

func (s *MemoryStore) GrantsForAgent(orgID, agentID string) []domain.AuthorityGrant {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []domain.AuthorityGrant{}
	for _, g := range s.grants {
		if g.OrgID == orgID && g.AgentID == agentID {
			out = append(out, *g)
		}
	}
	return out
}

func (s *MemoryStore) RevokeGrant(orgID, grantID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	g, ok := s.grants[grantID]
	if !ok || g.OrgID != orgID || g.RevokedAt != nil {
		return false
	}
	t := now()
	g.RevokedAt = &t
	return true
}

func (s *MemoryStore) RevokeCredential(keyID string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.credByKey[keyID]
	if !ok || c.revoked {
		return false
	}
	c.revoked = true
	s.credByKey[keyID] = c
	return true
}

func (s *MemoryStore) TouchCredential(keyID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c, ok := s.credByKey[keyID]; ok {
		c.lastUsed = now()
		s.credByKey[keyID] = c
	}
}

func (s *MemoryStore) TouchOperatorToken(keyID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if t, ok := s.opTokens[keyID]; ok {
		t.lastUsed = now()
		s.opTokens[keyID] = t
	}
}

func (s *MemoryStore) SetKeyProvider(p keys.Provider) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.keyProvider = p
}

func (s *MemoryStore) sealLocked(secret string) string {
	if secret == "" || s.keyProvider == nil {
		return secret
	}
	sealed, err := s.keyProvider.Seal(secret)
	if err != nil {
		return secret
	}
	return sealed
}

func (s *MemoryStore) openLocked(sealed string) (string, bool) {
	if sealed == "" {
		return "", true
	}
	if s.keyProvider == nil {
		return sealed, true
	}
	pt, err := s.keyProvider.Open(sealed)
	if err != nil {
		return "", false
	}
	return pt, true
}

func (s *MemoryStore) SetIntegrationCredential(orgID, name, secret string) error {
	if secret == "" {
		return fmt.Errorf("empty secret")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.integCreds == nil {
		s.integCreds = map[string]string{}
	}
	s.integCreds[orgID+"|"+name] = s.sealLocked(secret)
	return nil
}

func (s *MemoryStore) GetIntegrationCredential(orgID, name string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	sealed, ok := s.integCreds[orgID+"|"+name]
	if !ok {
		return "", false
	}
	return s.openLocked(sealed)
}

func (s *MemoryStore) ListIntegrationCredentials(orgID string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := []string{}
	for k := range s.integCreds {
		if strings.HasPrefix(k, orgID+"|") {
			out = append(out, strings.TrimPrefix(k, orgID+"|"))
		}
	}
	return out
}

func (s *MemoryStore) SetIntegrationConfig(orgID, name string, config map[string]any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.integConfig == nil {
		s.integConfig = map[string]map[string]any{}
	}
	cp := map[string]any{}
	for k, v := range config {
		cp[k] = v
	}
	s.integConfig[orgID+"|"+name] = cp
}

func (s *MemoryStore) GetIntegrationConfig(orgID, name string) (map[string]any, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	c, ok := s.integConfig[orgID+"|"+name]
	if !ok {
		return nil, false
	}
	cp := map[string]any{}
	for k, v := range c {
		cp[k] = v
	}
	return cp, true
}

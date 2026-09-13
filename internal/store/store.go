package store

import "github.com/agentguard/agentguard/internal/domain"

// Store is the persistence contract the gateway depends on.
// MemoryStore (dev/test) and PGStore (production) both implement it.
// Every method enforces tenant isolation: cross-org reads behave as not-found.
type Store interface {
	SeedOrg(name string) (domain.Organization, domain.Principal)
	CreateAgent(orgID, principalID, name, env string, groups []string, secretHash string) domain.Agent
	GetAgent(orgID, agentID string) (domain.Agent, bool)
	CredentialHash(agentID string) (string, bool)

	SetPolicies(orgID string, rules []domain.PolicyRule)
	Policies(orgID string) []domain.PolicyRule

	CreateTxn(t domain.Transaction) *domain.Transaction
	GetTxn(orgID, id string) (*domain.Transaction, bool)
	SetTxnStatus(orgID, id string, st domain.TxnStatus) error
	ListTxns(orgID string) []domain.Transaction

	AddAction(a domain.TxnAction) (*domain.TxnAction, bool)
	GetAction(orgID, id string) (*domain.TxnAction, bool)
	UpdateAction(a *domain.TxnAction)
	ActionsForTxn(orgID, txnID string) []domain.TxnAction

	CreateApproval(a domain.Approval) *domain.Approval
	GetApproval(orgID, id string) (*domain.Approval, bool)
	DecideApproval(orgID, id, by string, approve bool) (*domain.Approval, bool)
	PendingApprovals(orgID string) []domain.Approval

	AppendReceipt(r domain.Receipt) *domain.Receipt
	ReceiptsForTxn(orgID, txnID string) []domain.Receipt

	Emit(e domain.AuditEvent)
	Audit(orgID, txnID string, limit int) []domain.AuditEvent
	VerifyChain(orgID string) int

	// Auth: key-ID credential lookup. Secrets are never stored, only hashes.
	StoreCredential(orgID, agentID, keyID, secretHash string)
	GetCredential(keyID string) (orgID, agentID, secretHash string, ok bool)
	// Fallback for pre-key-ID secrets: hashes of all active creds in the org.
	AgentCredentialHashes(orgID string) map[string]string // agentID -> hash
	CreateOperatorToken(orgID, name, keyID, secretHash string)
	GetOperatorToken(keyID string) (orgID, name, secretHash string, ok bool)
}

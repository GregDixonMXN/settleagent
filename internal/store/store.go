package store

import "github.com/settleagent/settleagent/internal/domain"
import "github.com/settleagent/settleagent/internal/keys"

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

	// Versioned policy sets: exactly one ACTIVE per org. SetPolicies is
	// shorthand for create + activate (history preserved as prior versions).
	CreatePolicySet(orgID string, rules []domain.PolicyRule, createdBy string) domain.PolicySet
	ActivatePolicySet(orgID string, version int) bool
	PolicySets(orgID string) []domain.PolicySet
	ActivePolicySet(orgID string) (domain.PolicySet, bool)

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
	UpdateReceipt(r *domain.Receipt)

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

	// MCP: registered upstream servers. Tokens never leave the store.
	UpsertMCPServer(s domain.MCPServer, authToken string) domain.MCPServer
	GetMCPServer(orgID, name string) (domain.MCPServer, string, bool)
	ListMCPServers(orgID string) []domain.MCPServer

	// Authority grants: what an agent may attempt (policy decides outcome).
	CreateGrant(g domain.AuthorityGrant) domain.AuthorityGrant
	GrantsForAgent(orgID, agentID string) []domain.AuthorityGrant
	RevokeGrant(orgID, grantID string) bool

	// Credential lifecycle.
	RevokeCredential(keyID string) bool
	TouchCredential(keyID string)
	TouchOperatorToken(keyID string)

	// Third-party integration secrets (sealed at rest) + config.
	// SetKeyProvider enables sealing; nil provider stores plaintext (dev).
	SetKeyProvider(p keys.Provider)
	SetIntegrationCredential(orgID, name, secret string) error
	GetIntegrationCredential(orgID, name string) (string, bool)
	ListIntegrationCredentials(orgID string) []string
	SetIntegrationConfig(orgID, name string, config map[string]any)
	GetIntegrationConfig(orgID, name string) (map[string]any, bool)
}

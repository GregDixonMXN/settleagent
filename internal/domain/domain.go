package domain

import "time"

type ActionClass string

const (
	ClassReadOnly              ActionClass = "READ_ONLY"
	ClassReversible            ActionClass = "REVERSIBLE"
	ClassCompensable           ActionClass = "COMPENSABLE"
	ClassIrreversible          ActionClass = "IRREVERSIBLE"
	ClassFinancial             ActionClass = "FINANCIAL"
	ClassDestructive           ActionClass = "DESTRUCTIVE"
	ClassExternalCommunication ActionClass = "EXTERNAL_COMMUNICATION"
	ClassPrivileged            ActionClass = "PRIVILEGED"
	ClassUnknown               ActionClass = "UNKNOWN"
)

type TxnStatus string

const (
	TxnCreated              TxnStatus = "CREATED"
	TxnPlanning             TxnStatus = "PLANNING"
	TxnAwaitingApproval     TxnStatus = "AWAITING_APPROVAL"
	TxnExecuting            TxnStatus = "EXECUTING"
	TxnCommitted            TxnStatus = "COMMITTED"
	TxnAborting             TxnStatus = "ABORTING"
	TxnCompensating         TxnStatus = "COMPENSATING"
	TxnRolledBack           TxnStatus = "ROLLED_BACK"
	TxnPartiallyCompensated TxnStatus = "PARTIALLY_COMPENSATED"
	TxnFailed               TxnStatus = "FAILED"
)

type PolicyEffect string

const (
	EffectAllow                PolicyEffect = "ALLOW"
	EffectDeny                 PolicyEffect = "DENY"
	EffectRequireApproval      PolicyEffect = "REQUIRE_APPROVAL"
	EffectAllowWithConstraints PolicyEffect = "ALLOW_WITH_CONSTRAINTS"
)

type Organization struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	CreatedAt time.Time `json:"created_at"`
}

type PrincipalType string

const (
	PrincipalUser         PrincipalType = "USER"
	PrincipalOrganization PrincipalType = "ORGANIZATION"
)

type Principal struct {
	ID        string        `json:"id"`
	OrgID     string        `json:"org_id"`
	Type      PrincipalType `json:"type"`
	Name      string        `json:"name"`
	CreatedAt time.Time     `json:"created_at"`
}

type Agent struct {
	ID          string    `json:"id"`
	OrgID       string    `json:"org_id"`
	PrincipalID string    `json:"principal_id"`
	Name        string    `json:"name"`
	Groups      []string  `json:"groups"`
	Environment string    `json:"environment"`
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
}

type PolicyRule struct {
	ID          string       `json:"id"`
	OrgID       string       `json:"org_id"`
	Name        string       `json:"name"`
	Description string       `json:"description"`
	Priority    int          `json:"priority"`
	Match       PolicyMatch  `json:"match"`
	Effect      PolicyEffect `json:"effect"`
	Explanation string       `json:"explanation"`
}

type PolicyMatch struct {
	AgentGroups    []string `json:"agent_groups,omitempty"`
	Tool           string   `json:"tool,omitempty"`
	Action         string   `json:"action,omitempty"`
	MaxAmountCents *int64   `json:"max_amount_cents,omitempty"`
	MinAmountCents *int64   `json:"min_amount_cents,omitempty"`
	MinRecipients  *int     `json:"min_recipients,omitempty"`
	Environment    string   `json:"environment,omitempty"`
	// V2 conditions (all must hold when present; capped expressiveness).
	Classifications []string    `json:"classifications,omitempty"`
	Environments    []string    `json:"environments,omitempty"`
	ResourcePrefix  string      `json:"resource_prefix,omitempty"`
	TimeWindow      *TimeWindow `json:"time_window,omitempty"`
}

// TimeWindow constrains matching to UTC weekdays + hour range.
// Weekdays: 0=Sunday..6=Saturday. Hours: 0-24, Start inclusive, End exclusive.
type TimeWindow struct {
	Weekdays  []int `json:"weekdays,omitempty"`
	StartHour int   `json:"start_hour"`
	EndHour   int   `json:"end_hour"`
}

type PolicyDecision struct {
	Effect        PolicyEffect `json:"effect"`
	RuleID        string       `json:"rule_id"`
	Explanation   string       `json:"explanation"`
	ReasonCode    string       `json:"reason_code,omitempty"`
	PolicyVersion int          `json:"policy_version,omitempty"`
}

// PolicySet is one immutable version of an org's rules. Only one ACTIVE.
type PolicySet struct {
	ID        string       `json:"id"`
	OrgID     string       `json:"org_id"`
	Version   int          `json:"version"`
	Rules     []PolicyRule `json:"rules"`
	Status    string       `json:"status"`
	CreatedBy string       `json:"created_by,omitempty"`
	CreatedAt time.Time    `json:"created_at"`
}

const (
	ReasonAllowed           = "ALLOWED"
	ReasonAuthorityExceeded = "AUTHORITY_EXCEEDED"
	ReasonPolicyDenied      = "POLICY_DENIED"
	ReasonApprovalRequired  = "APPROVAL_REQUIRED"
)

// GrantConstraints are per-grant limits. Empty/absent means no limit.
type GrantConstraints struct {
	MaxAmountCents *int64   `json:"max_amount_cents,omitempty"`
	Environments   []string `json:"environments,omitempty"`
}

type AuthorityGrant struct {
	ID          string           `json:"id"`
	OrgID       string           `json:"org_id"`
	PrincipalID string           `json:"principal_id,omitempty"`
	AgentID     string           `json:"agent_id"`
	Scope       []string         `json:"scope"`
	Constraints GrantConstraints `json:"constraints"`
	Environment string           `json:"environment,omitempty"`
	IssuedBy    string           `json:"issued_by,omitempty"`
	IssuedAt    time.Time        `json:"issued_at"`
	ExpiresAt   *time.Time       `json:"expires_at,omitempty"`
	RevokedAt   *time.Time       `json:"revoked_at,omitempty"`
	Bootstrap   bool             `json:"bootstrap,omitempty"`
}

type TxnAction struct {
	ID             string          `json:"id"`
	OrgID          string          `json:"org_id"`
	TransactionID  string          `json:"transaction_id"`
	Seq            int             `json:"seq"`
	Tool           string          `json:"tool"`
	Action         string          `json:"action"`
	Arguments      map[string]any  `json:"arguments"`
	ArgumentsHash  string          `json:"arguments_hash"`
	Classes        []ActionClass   `json:"classes"`
	AmountCents    *int64          `json:"amount_cents,omitempty"`
	IdempotencyKey string          `json:"idempotency_key"`
	Decision       *PolicyDecision `json:"decision,omitempty"`
	Status         string          `json:"status"`
	Result         map[string]any  `json:"result,omitempty"`
	Error          string          `json:"error,omitempty"`
	LatencyMS      int64           `json:"latency_ms"`
	CreatedAt      time.Time       `json:"created_at"`
}

type Transaction struct {
	ID              string    `json:"id"`
	OrgID           string    `json:"org_id"`
	AgentID         string    `json:"agent_id"`
	PrincipalID     string    `json:"principal_id"`
	SessionID       string    `json:"session_id"`
	Objective       string    `json:"objective"`
	Status          TxnStatus `json:"status"`
	RiskBudgetCents *int64    `json:"risk_budget_cents,omitempty"`
	CreatedAt       time.Time `json:"created_at"`
	UpdatedAt       time.Time `json:"updated_at"`
}

type Approval struct {
	ID            string     `json:"id"`
	OrgID         string     `json:"org_id"`
	TransactionID string     `json:"transaction_id"`
	ActionID      *string    `json:"action_id,omitempty"`
	RequestedBy   string     `json:"requested_by"`
	Reason        string     `json:"reason"`
	ExposureCents *int64     `json:"exposure_cents,omitempty"`
	Status        string     `json:"status"`
	DecidedBy     string     `json:"decided_by,omitempty"`
	DecidedAt     *time.Time `json:"decided_at,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
}

type Receipt struct {
	ID             string       `json:"id"`
	OrgID          string       `json:"org_id"`
	TransactionID  string       `json:"transaction_id"`
	ActionID       string       `json:"action_id"`
	AgentID        string       `json:"agent_id"`
	PrincipalID    string       `json:"principal_id"`
	Tool           string       `json:"tool"`
	Action         string       `json:"action"`
	ArgumentsHash  string       `json:"arguments_hash"`
	Decision       PolicyEffect `json:"decision"`
	ResultHash     string       `json:"result_hash"`
	FinancialCents *int64       `json:"financial_cents,omitempty"`
	Compensation   string       `json:"compensation"`
	PrevHash       string       `json:"prev_hash"`
	Hash           string       `json:"hash"`
	KeyID          string       `json:"key_id,omitempty"`
	Signature      string       `json:"signature,omitempty"`
	PolicyVersion  int          `json:"policy_version,omitempty"`
	StartedAt      time.Time    `json:"started_at"`
	CompletedAt    time.Time    `json:"completed_at"`
}

type AuditEvent struct {
	ID            string         `json:"id"`
	OrgID         string         `json:"org_id"`
	TransactionID string         `json:"transaction_id,omitempty"`
	ActorType     string         `json:"actor_type"`
	ActorID       string         `json:"actor_id"`
	Type          string         `json:"type"`
	Payload       map[string]any `json:"payload,omitempty"`
	CreatedAt     time.Time      `json:"created_at"`
}

type MCPTool struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

type MCPServer struct {
	ID        string                   `json:"id"`
	OrgID     string                   `json:"org_id"`
	Name      string                   `json:"name"`
	URL       string                   `json:"url"`
	HasToken  bool                     `json:"has_token"`
	Tools     []MCPTool                `json:"tools"`
	Classes   map[string][]ActionClass `json:"classes,omitempty"`
	CreatedAt time.Time                `json:"created_at"`
}

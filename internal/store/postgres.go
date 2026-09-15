package store

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/settleagent/settleagent/internal/domain"
	"github.com/settleagent/settleagent/internal/keys"
	"github.com/settleagent/settleagent/internal/receipts"
)

var _ Store = (*PGStore)(nil)

type PGStore struct {
	pool *pgxpool.Pool
	keys keys.Provider
}

func (s *PGStore) SetKeyProvider(p keys.Provider) { s.keys = p }

func (s *PGStore) seal(secret string) string {
	if secret == "" || s.keys == nil {
		return secret
	}
	sealed, err := s.keys.Seal(secret)
	if err != nil {
		return secret
	}
	return sealed
}

func (s *PGStore) open(sealed string) (string, bool) {
	if sealed == "" {
		return "", true
	}
	if s.keys == nil {
		return sealed, true
	}
	pt, err := s.keys.Open(sealed)
	if err != nil {
		return "", false
	}
	return pt, true
}

func Open(ctx context.Context, url string) (*PGStore, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, err
	}
	return &PGStore{pool: pool}, nil
}

func (s *PGStore) Close() { s.pool.Close() }

// Pool exposes the underlying pool for migration on boot.
func (s *PGStore) Pool() *pgxpool.Pool { return s.pool }

func pgUUID(id string) (pgtype.UUID, error) {
	u, err := uuid.Parse(id)
	if err != nil {
		return pgtype.UUID{}, err
	}
	var b [16]byte
	copy(b[:], u[:])
	return pgtype.UUID{Bytes: b, Valid: true}, nil
}

func mustPGUUID(id string) pgtype.UUID {
	u, err := pgUUID(id)
	if err != nil {
		return pgtype.UUID{}
	}
	return u
}

func optUUID(id string) pgtype.UUID {
	if id == "" {
		return pgtype.UUID{}
	}
	return mustPGUUID(id)
}

func jsonParam(v any) string {
	b, _ := json.Marshal(v)
	if b == nil {
		return "null"
	}
	return string(b)
}

func classesToStrings(c []domain.ActionClass) []string {
	out := make([]string, len(c))
	for i, x := range c {
		out[i] = string(x)
	}
	return out
}

func stringsToClasses(s []string) []domain.ActionClass {
	out := make([]domain.ActionClass, len(s))
	for i, x := range s {
		out[i] = domain.ActionClass(x)
	}
	return out
}

// --- orgs / principals / agents ---

func (s *PGStore) SeedOrg(name string) (domain.Organization, domain.Principal) {
	ctx := context.Background()
	var o domain.Organization
	var p domain.Principal
	_ = s.pool.QueryRow(ctx,
		`INSERT INTO organizations(name) VALUES($1) RETURNING org_id, name, created_at`,
		name).Scan(&o.ID, &o.Name, &o.CreatedAt)
	_ = s.pool.QueryRow(ctx,
		`INSERT INTO principals(org_id, kind, display_name) VALUES($1,'ORGANIZATION',$2)
		 RETURNING principal_id, org_id, display_name, created_at`,
		o.ID, name).Scan(&p.ID, &p.OrgID, &p.Name, &p.CreatedAt)
	p.Type = domain.PrincipalOrganization
	return o, p
}

func (s *PGStore) OrgIDs() []string {
	ctx := context.Background()
	rows, err := s.pool.Query(ctx, `SELECT org_id::text FROM organizations ORDER BY created_at`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err == nil {
			out = append(out, id)
		}
	}
	return out
}

func (s *PGStore) CreateAgent(orgID, principalID, name, env string, groups []string, secretHash string) domain.Agent {
	ctx := context.Background()
	id := uuid.NewString()
	var a domain.Agent
	_ = s.pool.QueryRow(ctx,
		`INSERT INTO agents(agent_id, org_id, owner_principal_id, agent_group, display_name, environment, groups)
		 VALUES($1,$2,$3,$4,$5,$6,$7)
		 RETURNING agent_id::text, org_id::text, display_name, status, created_at, environment, groups`,
		id, orgID, principalID, firstOr(groups, ""), name, env, groups,
	).Scan(&a.ID, &a.OrgID, &a.Name, &a.Status, &a.CreatedAt, &a.Environment, &a.Groups)
	a.PrincipalID = principalID
	_, _ = s.pool.Exec(ctx,
		`INSERT INTO agent_credentials(org_id, agent_id, secret_hash) VALUES($1,$2,$3)`,
		orgID, id, secretHash)
	return a
}

func firstOr(s []string, d string) string {
	if len(s) > 0 {
		return s[0]
	}
	return d
}

func (s *PGStore) GetAgent(orgID, agentID string) (domain.Agent, bool) {
	ctx := context.Background()
	var a domain.Agent
	var principalID string
	err := s.pool.QueryRow(ctx,
		`SELECT agent_id::text, org_id::text, owner_principal_id::text, display_name,
		        status, created_at, environment, groups
		 FROM agents WHERE org_id=$1 AND agent_id=$2`,
		orgID, agentID).Scan(&a.ID, &a.OrgID, &principalID, &a.Name, &a.Status, &a.CreatedAt, &a.Environment, &a.Groups)
	if err != nil {
		return domain.Agent{}, false
	}
	a.PrincipalID = principalID
	return a, true
}

func (s *PGStore) CredentialHash(agentID string) (string, bool) {
	ctx := context.Background()
	var h string
	err := s.pool.QueryRow(ctx,
		`SELECT secret_hash FROM agent_credentials
		 WHERE agent_id=$1 AND revoked_at IS NULL ORDER BY created_at DESC LIMIT 1`,
		agentID).Scan(&h)
	return h, err == nil
}

// --- policies ---

func (s *PGStore) SetPolicies(orgID string, rules []domain.PolicyRule) {
	set := s.CreatePolicySet(orgID, rules, "system")
	s.ActivatePolicySet(orgID, set.Version)
}

func (s *PGStore) Policies(orgID string) []domain.PolicyRule {
	if set, ok := s.ActivePolicySet(orgID); ok {
		return append([]domain.PolicyRule(nil), set.Rules...)
	}
	return s.legacyPolicies(orgID)
}

func (s *PGStore) legacyPolicies(orgID string) []domain.PolicyRule {
	ctx := context.Background()
	rows, err := s.pool.Query(ctx,
		`SELECT policy_rule_id::text, org_id::text, name, description, priority, match::text, effect, explanation
		 FROM policy_rules WHERE org_id=$1 ORDER BY priority`, orgID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []domain.PolicyRule
	for rows.Next() {
		var r domain.PolicyRule
		var matchText, effect string
		if err := rows.Scan(&r.ID, &r.OrgID, &r.Name, &r.Description, &r.Priority, &matchText, &effect, &r.Explanation); err != nil {
			continue
		}
		_ = json.Unmarshal([]byte(matchText), &r.Match)
		r.Effect = domain.PolicyEffect(effect)
		out = append(out, r)
	}
	return out
}

func scanPolicySet(row pgx.Row) (domain.PolicySet, bool) {
	var p domain.PolicySet
	var rulesText string
	if err := row.Scan(&p.ID, &p.OrgID, &p.Version, &rulesText, &p.Status, &p.CreatedBy, &p.CreatedAt); err != nil {
		return domain.PolicySet{}, false
	}
	_ = json.Unmarshal([]byte(rulesText), &p.Rules)
	return p, true
}

const policySetCols = `set_id::text, org_id::text, version, rules::text, status, created_by, created_at`

func (s *PGStore) CreatePolicySet(orgID string, rules []domain.PolicyRule, createdBy string) domain.PolicySet {
	ctx := context.Background()
	// Serialize rule IDs: rules carry their own IDs for stable references.
	for i := range rules {
		if rules[i].ID == "" {
			rules[i].ID = uuid.NewString()
		}
		rules[i].OrgID = orgID
	}
	row := s.pool.QueryRow(ctx,
		`INSERT INTO policy_sets(org_id, version, rules, status, created_by)
		 VALUES($1, COALESCE((SELECT MAX(version) FROM policy_sets WHERE org_id=$1),0)+1, $2::jsonb, 'draft', $3)
		 RETURNING `+policySetCols, orgID, jsonParam(rules), createdBy)
	if set, ok := scanPolicySet(row); ok {
		return set
	}
	return domain.PolicySet{OrgID: orgID, Rules: rules, Status: "draft"}
}

func (s *PGStore) ActivatePolicySet(orgID string, version int) bool {
	ctx := context.Background()
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return false
	}
	defer tx.Rollback(ctx)
	var exists bool
	_ = tx.QueryRow(ctx, `SELECT true FROM policy_sets WHERE org_id=$1 AND version=$2`, orgID, version).Scan(&exists)
	if !exists {
		return false
	}
	if _, err := tx.Exec(ctx, `UPDATE policy_sets SET status='disabled' WHERE org_id=$1 AND status='active'`, orgID); err != nil {
		return false
	}
	if _, err := tx.Exec(ctx, `UPDATE policy_sets SET status='active' WHERE org_id=$1 AND version=$2`, orgID, version); err != nil {
		return false
	}
	return tx.Commit(ctx) == nil
}

func (s *PGStore) PolicySets(orgID string) []domain.PolicySet {
	rows, err := s.pool.Query(context.Background(),
		`SELECT `+policySetCols+` FROM policy_sets WHERE org_id=$1 ORDER BY version`, orgID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []domain.PolicySet{}
	for rows.Next() {
		if p, ok := scanPolicySet(rows); ok {
			out = append(out, p)
		}
	}
	return out
}

func (s *PGStore) ActivePolicySet(orgID string) (domain.PolicySet, bool) {
	return scanPolicySet(s.pool.QueryRow(context.Background(),
		`SELECT `+policySetCols+` FROM policy_sets WHERE org_id=$1 AND status='active'`, orgID))
}

// --- transactions ---

func (s *PGStore) CreateTxn(t domain.Transaction) *domain.Transaction {
	ctx := context.Background()
	t.ID = uuid.NewString()
	t.CreatedAt = time.Now().UTC()
	t.UpdatedAt = t.CreatedAt
	if t.Status == "" {
		t.Status = domain.TxnCreated
	}
	_, _ = s.pool.Exec(ctx,
		`INSERT INTO transactions(txn_id, org_id, agent_id, principal_id, state, idempotency_key,
		                          session_id, objective, risk_budget_cents, created_at, updated_at)
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11)`,
		t.ID, t.OrgID, t.AgentID, optUUID(t.PrincipalID), string(t.Status),
		"txn:"+t.ID, t.SessionID, t.Objective, t.RiskBudgetCents, t.CreatedAt, t.UpdatedAt)
	s.Emit(domain.AuditEvent{OrgID: t.OrgID, TransactionID: t.ID, ActorType: "agent", ActorID: t.AgentID, Type: "transaction.created"})
	return &t
}

func scanTxn(row pgx.Row) (*domain.Transaction, bool) {
	var t domain.Transaction
	var state string
	var principalID pgtype.UUID
	var risk *int64
	var session, objective string
	if err := row.Scan(&t.ID, &t.OrgID, &t.AgentID, &principalID, &state,
		&session, &objective, &risk, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return nil, false
	}
	t.Status = domain.TxnStatus(state)
	if principalID.Valid {
		u := uuid.UUID(principalID.Bytes)
		t.PrincipalID = u.String()
	}
	t.RiskBudgetCents = risk
	t.SessionID = session
	t.Objective = objective
	return &t, true
}

func (s *PGStore) GetTxn(orgID, id string) (*domain.Transaction, bool) {
	return scanTxn(s.pool.QueryRow(context.Background(),
		`SELECT txn_id::text, org_id::text, agent_id::text, principal_id,
		        state, session_id, objective, risk_budget_cents, created_at, updated_at
		 FROM transactions WHERE org_id=$1 AND txn_id=$2`, orgID, id))
}

func (s *PGStore) SetTxnStatus(orgID, id string, st domain.TxnStatus) error {
	ct, err := s.pool.Exec(context.Background(),
		`UPDATE transactions SET state=$3, updated_at=now()
		 WHERE org_id=$1 AND txn_id=$2`, orgID, id, string(st))
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return fmt.Errorf("transaction not found")
	}
	return nil
}

func (s *PGStore) ListTxns(orgID string) []domain.Transaction {
	rows, err := s.pool.Query(context.Background(),
		`SELECT txn_id::text, org_id::text, agent_id::text, principal_id,
		        state, session_id, objective, risk_budget_cents, created_at, updated_at
		 FROM transactions WHERE org_id=$1 ORDER BY created_at DESC`, orgID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []domain.Transaction
	for rows.Next() {
		if t, ok := scanTxn(rows); ok {
			out = append(out, *t)
		}
	}
	return out
}

// Recover lists non-terminal transactions and records a recovery audit event
// for each, so a restarted process resumes with full evidence intact.
func (s *PGStore) Recover() map[string]int {
	ctx := context.Background()
	rows, err := s.pool.Query(ctx,
		`SELECT org_id::text, txn_id::text, state FROM transactions
		 WHERE state NOT IN ('COMMITTED','ROLLED_BACK','PARTIALLY_COMPENSATED','FAILED')`)
	if err != nil {
		return nil
	}
	defer rows.Close()
	counts := map[string]int{}
	for rows.Next() {
		var org, id, state string
		if err := rows.Scan(&org, &id, &state); err != nil {
			continue
		}
		counts[state]++
		s.Emit(domain.AuditEvent{OrgID: org, TransactionID: id, ActorType: "system",
			ActorID: "gateway", Type: "transaction.recovered",
			Payload: map[string]any{"state": state}})
	}
	return counts
}

// --- actions ---

func scanAction(row pgx.Row) (*domain.TxnAction, bool) {
	var a domain.TxnAction
	var classes []string
	var argsText string
	var resultText *string
	var decisionText *string
	var amount *int64
	var errText string
	if err := row.Scan(&a.ID, &a.OrgID, &a.TransactionID, &a.Seq, &a.Tool, &a.Action,
		&argsText, &classes, &a.ArgumentsHash, &amount, &a.IdempotencyKey,
		&decisionText, &a.Status, &resultText, &errText, &a.LatencyMS, &a.CreatedAt); err != nil {
		return nil, false
	}
	_ = json.Unmarshal([]byte(argsText), &a.Arguments)
	a.Classes = stringsToClasses(classes)
	a.AmountCents = amount
	if decisionText != nil {
		var d domain.PolicyDecision
		if json.Unmarshal([]byte(*decisionText), &d) == nil {
			a.Decision = &d
		}
	}
	if resultText != nil {
		var res map[string]any
		if json.Unmarshal([]byte(*resultText), &res) == nil {
			a.Result = res
		}
	}
	a.Error = errText
	return &a, true
}

const actionCols = `action_id::text, org_id::text, txn_id::text, seq, tool, action,
	args::text, classes, args_hash, amount_cents, idempotency_key,
	decision::text, status, result::text, error, latency_ms, created_at`

func (s *PGStore) AddAction(a domain.TxnAction) (*domain.TxnAction, bool) {
	ctx := context.Background()
	a.ID = uuid.NewString()
	a.CreatedAt = time.Now().UTC()
	if a.Status == "" {
		a.Status = "proposed"
	}
	a.ArgumentsHash = receipts.Canonical(a.Arguments)
	var decisionText *string
	var effect *string
	if a.Decision != nil {
		b, _ := json.Marshal(a.Decision)
		str := string(b)
		decisionText = &str
		e := string(a.Decision.Effect)
		effect = &e
	}
	var resultText *string
	if a.Result != nil {
		b, _ := json.Marshal(a.Result)
		str := string(b)
		resultText = &str
	}
	row := s.pool.QueryRow(ctx,
		`INSERT INTO actions(action_id, org_id, txn_id, seq, tool, action, args, classes,
		                     args_hash, amount_cents, idempotency_key, decision, policy_result,
		                     status, result, error, latency_ms, created_at)
		 VALUES($1,$2,$3,$4,$5,$6,$7::jsonb,$8,$9,$10,$11,$12::jsonb,$13,$14,$15::jsonb,$16,$17,$18)
		 ON CONFLICT (org_id, txn_id, idempotency_key) DO NOTHING
		 RETURNING `+actionCols,
		a.ID, a.OrgID, a.TransactionID, a.Seq, a.Tool, a.Action, jsonParam(a.Arguments),
		classesToStrings(a.Classes), a.ArgumentsHash, a.AmountCents, a.IdempotencyKey,
		nullable(decisionText), nullable(effect), a.Status, nullable(resultText), a.Error,
		a.LatencyMS, a.CreatedAt)
	if stored, ok := scanAction(row); ok {
		return stored, false
	}
	// Conflict: return the pre-existing action (idempotent replay).
	existing, ok := scanAction(s.pool.QueryRow(ctx,
		`SELECT `+actionCols+` FROM actions WHERE org_id=$1 AND txn_id=$2 AND idempotency_key=$3`,
		a.OrgID, a.TransactionID, a.IdempotencyKey))
	if !ok {
		return &a, true
	}
	return existing, true
}

func nullable(s *string) any {
	if s == nil {
		return nil
	}
	return *s
}

func reloadAction(s *PGStore, orgID, id string) (*domain.TxnAction, bool) {
	return scanAction(s.pool.QueryRow(context.Background(),
		`SELECT `+actionCols+` FROM actions WHERE org_id=$1 AND action_id=$2`, orgID, id))
}

func (s *PGStore) GetAction(orgID, id string) (*domain.TxnAction, bool) {
	return reloadAction(s, orgID, id)
}

func (s *PGStore) UpdateAction(a *domain.TxnAction) {
	ctx := context.Background()
	var decisionText *string
	var effect *string
	if a.Decision != nil {
		b, _ := json.Marshal(a.Decision)
		str := string(b)
		decisionText = &str
		e := string(a.Decision.Effect)
		effect = &e
	}
	var resultText *string
	if a.Result != nil {
		b, _ := json.Marshal(a.Result)
		str := string(b)
		resultText = &str
	}
	_, _ = s.pool.Exec(ctx,
		`UPDATE actions SET seq=$3, tool=$4, action=$5, args=$6::jsonb, classes=$7,
		                    amount_cents=$8, decision=$9::jsonb, policy_result=$10,
		                    status=$11, result=$12::jsonb, error=$13, latency_ms=$14,
		                    updated_at=now()
		 WHERE org_id=$1 AND action_id=$2`,
		a.OrgID, a.ID, a.Seq, a.Tool, a.Action, jsonParam(a.Arguments),
		classesToStrings(a.Classes), a.AmountCents, nullable(decisionText), nullable(effect),
		a.Status, nullable(resultText), a.Error, a.LatencyMS)
}

func (s *PGStore) ActionsForTxn(orgID, txnID string) []domain.TxnAction {
	rows, err := s.pool.Query(context.Background(),
		`SELECT `+actionCols+` FROM actions
		 WHERE org_id=$1 AND txn_id=$2 ORDER BY seq, created_at, action_id`, orgID, txnID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []domain.TxnAction
	for rows.Next() {
		if a, ok := scanAction(rows); ok {
			out = append(out, *a)
		}
	}
	return out
}

// --- approvals ---

func scanApproval(row pgx.Row) (*domain.Approval, bool) {
	var ap domain.Approval
	var actionID pgtype.UUID
	var decidedAt *time.Time
	var exposure *int64
	var decision string
	if err := row.Scan(&ap.ID, &ap.OrgID, &ap.TransactionID, &actionID,
		&ap.RequestedBy, &ap.DecidedBy, &exposure, &ap.Reason, &decision,
		&decidedAt, &ap.CreatedAt); err != nil {
		return nil, false
	}
	if actionID.Valid {
		u := uuid.UUID(actionID.Bytes)
		str := u.String()
		ap.ActionID = &str
	}
	ap.ExposureCents = exposure
	ap.Status = decision
	ap.DecidedAt = decidedAt
	return &ap, true
}

const approvalCols = `approval_id::text, org_id::text, txn_id::text, action_id,
	requested_by, decided_by, exposure_cents, reason_text, decision, decided_at, created_at`

func (s *PGStore) CreateApproval(a domain.Approval) *domain.Approval {
	ctx := context.Background()
	a.ID = uuid.NewString()
	a.CreatedAt = time.Now().UTC()
	a.Status = "pending"
	_, _ = s.pool.Exec(ctx,
		`INSERT INTO approvals(approval_id, org_id, txn_id, action_id, requested_by_agent_id,
		                       requested_by, reason, reason_text, exposure_cents, decision, created_at)
		 VALUES($1,$2,$3,$4,$5,$6,$7,$7,$8,'pending',$9)`,
		a.ID, a.OrgID, a.TransactionID, optUUID(strOr(a.ActionID, "")),
		optUUID(a.RequestedBy), a.RequestedBy, a.Reason, a.ExposureCents, a.CreatedAt)
	return &a
}

func strOr(p *string, d string) string {
	if p == nil {
		return d
	}
	return *p
}

func (s *PGStore) GetApproval(orgID, id string) (*domain.Approval, bool) {
	return scanApproval(s.pool.QueryRow(context.Background(),
		`SELECT `+approvalCols+` FROM approvals WHERE org_id=$1 AND approval_id=$2`, orgID, id))
}

func (s *PGStore) DecideApproval(orgID, id, by string, approve bool) (*domain.Approval, bool) {
	decision := "denied"
	if approve {
		decision = "approved"
	}
	return scanApproval(s.pool.QueryRow(context.Background(),
		`UPDATE approvals SET decision=$3, decided_by=$4, decided_at=now()
		 WHERE org_id=$1 AND approval_id=$2 AND decision='pending'
		 RETURNING `+approvalCols, orgID, id, decision, by))
}

func (s *PGStore) PendingApprovals(orgID string) []domain.Approval {
	rows, err := s.pool.Query(context.Background(),
		`SELECT `+approvalCols+` FROM approvals
		 WHERE org_id=$1 AND decision='pending' ORDER BY created_at`, orgID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []domain.Approval
	for rows.Next() {
		if ap, ok := scanApproval(rows); ok {
			out = append(out, *ap)
		}
	}
	return out
}

// --- receipts ---

func (s *PGStore) AppendReceipt(r domain.Receipt) *domain.Receipt {
	ctx := context.Background()
	// Advisory lock serializes per-org chain appends; microsecond truncation
	// keeps inserted hashes reproducible when rows round-trip through PG.
	var prev string
	_ = s.pool.QueryRow(ctx, `SELECT pg_advisory_xact_lock(hashtext($1))`, r.OrgID).Scan(&prev)
	_ = s.pool.QueryRow(ctx,
		`SELECT hash FROM receipts WHERE org_id=$1 ORDER BY created_at DESC, receipt_id DESC LIMIT 1`,
		r.OrgID).Scan(&prev)
	r.ID = uuid.NewString()
	r.PrevHash = prev
	r.StartedAt = r.StartedAt.UTC().Truncate(time.Microsecond)
	r.CompletedAt = r.CompletedAt.UTC().Truncate(time.Microsecond)
	payload := map[string]any{
		"txn": r.TransactionID, "action": r.ActionID, "tool": r.Tool,
		"args_hash": r.ArgumentsHash, "result_hash": r.ResultHash,
		"decision": r.Decision, "started": r.StartedAt, "completed": r.CompletedAt,
	}
	r.Hash = receipts.ChainHash(r.PrevHash, payload)
	_, _ = s.pool.Exec(ctx,
		`INSERT INTO receipts(receipt_id, org_id, txn_id, action_id, agent_id, principal_id,
		                      tool, action_name, decision, args_hash, result_hash,
		                      financial_cents, compensation, prev_hash, hash,
		                      started_at, completed_at, policy_version, payload)
		 VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19::jsonb)`,
		r.ID, r.OrgID, r.TransactionID, optUUID(r.ActionID), optUUID(r.AgentID),
		optUUID(r.PrincipalID), r.Tool, r.Action, string(r.Decision),
		r.ArgumentsHash, r.ResultHash, r.FinancialCents, r.Compensation,
		r.PrevHash, r.Hash, r.StartedAt, r.CompletedAt, r.PolicyVersion, jsonParam(payload))
	return &r
}

func scanReceipt(row pgx.Row) (*domain.Receipt, bool) {
	var r domain.Receipt
	var actionID, agentID, principalID pgtype.UUID
	var decision string
	if err := row.Scan(&r.ID, &r.OrgID, &r.TransactionID, &actionID, &agentID, &principalID,
		&r.Tool, &r.Action, &decision, &r.ArgumentsHash, &r.ResultHash,
		&r.FinancialCents, &r.Compensation, &r.PrevHash, &r.Hash,
		&r.KeyID, &r.Signature, &r.PolicyVersion, &r.StartedAt, &r.CompletedAt); err != nil {
		return nil, false
	}
	if actionID.Valid {
		u := uuid.UUID(actionID.Bytes)
		r.ActionID = u.String()
	}
	if agentID.Valid {
		u := uuid.UUID(agentID.Bytes)
		r.AgentID = u.String()
	}
	if principalID.Valid {
		u := uuid.UUID(principalID.Bytes)
		r.PrincipalID = u.String()
	}
	r.Decision = domain.PolicyEffect(decision)
	return &r, true
}

const receiptCols = `receipt_id::text, org_id::text, txn_id::text, action_id, agent_id, principal_id,
	tool, action_name, decision, args_hash, result_hash,
	financial_cents, compensation, prev_hash, hash, key_id, signature, policy_version, started_at, completed_at`

func (s *PGStore) ReceiptsForTxn(orgID, txnID string) []domain.Receipt {
	rows, err := s.pool.Query(context.Background(),
		`SELECT `+receiptCols+` FROM receipts
		 WHERE org_id=$1 AND txn_id=$2 ORDER BY created_at, receipt_id`, orgID, txnID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []domain.Receipt
	for rows.Next() {
		if r, ok := scanReceipt(rows); ok {
			out = append(out, *r)
		}
	}
	return out
}

func (s *PGStore) UpdateReceipt(r *domain.Receipt) {
	_, _ = s.pool.Exec(context.Background(),
		`UPDATE receipts SET key_id=$3, signature=$4
		 WHERE org_id=$1 AND receipt_id=$2`, r.OrgID, r.ID, r.KeyID, r.Signature)
}

// --- audit ---

func (s *PGStore) Emit(e domain.AuditEvent) {
	ctx := context.Background()
	if e.CreatedAt.IsZero() {
		e.CreatedAt = time.Now().UTC()
	}
	_, _ = s.pool.Exec(ctx,
		`INSERT INTO audit_events(org_id, txn_id, actor, actor_type, actor_id, kind, detail, created_at)
		 VALUES($1,$2,$3,$4,$5,$6,$7::jsonb,$8)`,
		e.OrgID, optUUID(e.TransactionID), e.ActorType+":"+e.ActorID,
		e.ActorType, e.ActorID, e.Type, jsonParam(e.Payload), e.CreatedAt)
}

func (s *PGStore) Audit(orgID, txnID string, limit int) []domain.AuditEvent {
	ctx := context.Background()
	var rows pgx.Rows
	var err error
	if txnID == "" {
		rows, err = s.pool.Query(ctx,
			`SELECT event_id::text, org_id::text, txn_id::text, actor_type, actor_id, kind, detail::text, created_at
			 FROM audit_events WHERE org_id=$1 ORDER BY created_at DESC, event_id DESC LIMIT $2`,
			orgID, limit)
	} else {
		rows, err = s.pool.Query(ctx,
			`SELECT event_id::text, org_id::text, txn_id::text, actor_type, actor_id, kind, detail::text, created_at
			 FROM audit_events WHERE org_id=$1 AND txn_id=$2 ORDER BY created_at DESC, event_id DESC LIMIT $3`,
			orgID, txnID, limit)
	}
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []domain.AuditEvent
	for rows.Next() {
		var e domain.AuditEvent
		var txnIDText *string
		var detailText string
		if err := rows.Scan(&e.ID, &e.OrgID, &txnIDText, &e.ActorType, &e.ActorID, &e.Type, &detailText, &e.CreatedAt); err != nil {
			continue
		}
		if txnIDText != nil {
			e.TransactionID = *txnIDText
		}
		_ = json.Unmarshal([]byte(detailText), &e.Payload)
		out = append(out, e)
	}
	return out
}

func (s *PGStore) VerifyChain(orgID string) int {
	rows, err := s.pool.Query(context.Background(),
		`SELECT `+receiptCols+` FROM receipts WHERE org_id=$1 ORDER BY created_at, receipt_id`, orgID)
	if err != nil {
		return -1
	}
	defer rows.Close()
	prev := ""
	idx := 0
	for rows.Next() {
		r, ok := scanReceipt(rows)
		if !ok {
			return idx
		}
		payload := map[string]any{
			"txn": r.TransactionID, "action": r.ActionID, "tool": r.Tool,
			"args_hash": r.ArgumentsHash, "result_hash": r.ResultHash,
			"decision": r.Decision, "started": r.StartedAt.UTC(), "completed": r.CompletedAt.UTC(),
		}
		if r.PrevHash != prev || r.Hash != receipts.ChainHash(prev, payload) {
			return idx
		}
		prev = r.Hash
		idx++
	}
	return -1
}

func (s *PGStore) StoreCredential(orgID, agentID, keyID, secretHash string) {
	_, _ = s.pool.Exec(context.Background(),
		`INSERT INTO agent_credentials(org_id, agent_id, key_id, secret_hash)
		 VALUES($1,$2,$3,$4)`, orgID, agentID, keyID, secretHash)
}

func (s *PGStore) GetCredential(keyID string) (string, string, string, bool) {
	var org, agent, hash string
	err := s.pool.QueryRow(context.Background(),
		`SELECT org_id::text, agent_id::text, secret_hash FROM agent_credentials
		 WHERE key_id=$1 AND revoked_at IS NULL`, keyID).Scan(&org, &agent, &hash)
	return org, agent, hash, err == nil
}

func (s *PGStore) AgentCredentialHashes(orgID string) map[string]string {
	rows, err := s.pool.Query(context.Background(),
		`SELECT DISTINCT ON (agent_id) agent_id::text, secret_hash FROM agent_credentials
		 WHERE org_id=$1 AND revoked_at IS NULL ORDER BY agent_id, created_at DESC`, orgID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var id, hash string
		if err := rows.Scan(&id, &hash); err == nil {
			out[id] = hash
		}
	}
	return out
}

func (s *PGStore) CreateOperatorToken(orgID, name, keyID, secretHash string) {
	_, _ = s.pool.Exec(context.Background(),
		`INSERT INTO operator_tokens(org_id, name, key_id, secret_hash)
		 VALUES($1,$2,$3,$4)`, orgID, name, keyID, secretHash)
}

func (s *PGStore) GetOperatorToken(keyID string) (string, string, string, bool) {
	var org, name, hash string
	err := s.pool.QueryRow(context.Background(),
		`SELECT org_id::text, name, secret_hash FROM operator_tokens
		 WHERE key_id=$1 AND revoked_at IS NULL`, keyID).Scan(&org, &name, &hash)
	return org, name, hash, err == nil
}

func (s *PGStore) UpsertMCPServer(srv domain.MCPServer, authToken string) domain.MCPServer {
	ctx := context.Background()
	toolsJSON := jsonParam(srv.Tools)
	classesJSON := jsonParam(srv.Classes)
	var id, created string
	_ = s.pool.QueryRow(ctx,
		`INSERT INTO mcp_servers(org_id, name, url, auth_token, tools, classes, updated_at)
		 VALUES($1,$2,$3,$4,$5::jsonb,$6::jsonb,now())
		 ON CONFLICT (org_id, name) DO UPDATE
		   SET url=EXCLUDED.url, auth_token=EXCLUDED.auth_token,
		       tools=EXCLUDED.tools, classes=EXCLUDED.classes, updated_at=now()
		 RETURNING server_id::text, created_at::text`,
		srv.OrgID, srv.Name, srv.URL, s.seal(authToken), toolsJSON, classesJSON).Scan(&id, &created)
	srv.ID = id
	srv.HasToken = authToken != ""
	if t, err := time.Parse(time.RFC3339, created); err == nil {
		srv.CreatedAt = t
	}
	return srv
}

func scanMCPServer(withToken bool) (string, func(pgx.Row) (domain.MCPServer, string, bool)) {
	q := `server_id::text, org_id::text, name, url, tools::text, classes::text, created_at`
	if withToken {
		q += `, auth_token`
	}
	scan := func(row pgx.Row) (domain.MCPServer, string, bool) {
		var srv domain.MCPServer
		var toolsText, classesText string
		var token string
		var err error
		if withToken {
			err = row.Scan(&srv.ID, &srv.OrgID, &srv.Name, &srv.URL, &toolsText, &classesText, &srv.CreatedAt, &token)
		} else {
			err = row.Scan(&srv.ID, &srv.OrgID, &srv.Name, &srv.URL, &toolsText, &classesText, &srv.CreatedAt)
		}
		if err != nil {
			return domain.MCPServer{}, "", false
		}
		_ = json.Unmarshal([]byte(toolsText), &srv.Tools)
		_ = json.Unmarshal([]byte(classesText), &srv.Classes)
		srv.HasToken = token != ""
		return srv, token, true
	}
	return q, scan
}

func (s *PGStore) GetMCPServer(orgID, name string) (domain.MCPServer, string, bool) {
	cols, scan := scanMCPServer(true)
	srv, sealed, ok := scan(s.pool.QueryRow(context.Background(),
		`SELECT `+cols+` FROM mcp_servers WHERE org_id=$1 AND name=$2`, orgID, name))
	if !ok {
		return domain.MCPServer{}, "", false
	}
	token, ok := s.open(sealed)
	if !ok {
		return domain.MCPServer{}, "", false
	}
	return srv, token, true
}

func (s *PGStore) SetIntegrationCredential(orgID, name, secret string) error {
	if secret == "" {
		return fmt.Errorf("empty secret")
	}
	_, err := s.pool.Exec(context.Background(),
		`INSERT INTO integration_credentials(org_id, name, secret_enc, updated_at)
		 VALUES($1,$2,$3,now())
		 ON CONFLICT (org_id, name) DO UPDATE SET secret_enc=EXCLUDED.secret_enc, updated_at=now()`,
		orgID, name, s.seal(secret))
	return err
}

func (s *PGStore) GetIntegrationCredential(orgID, name string) (string, bool) {
	var sealed string
	err := s.pool.QueryRow(context.Background(),
		`SELECT secret_enc FROM integration_credentials WHERE org_id=$1 AND name=$2`,
		orgID, name).Scan(&sealed)
	if err != nil {
		return "", false
	}
	return s.open(sealed)
}

func (s *PGStore) ListIntegrationCredentials(orgID string) []string {
	rows, err := s.pool.Query(context.Background(),
		`SELECT name FROM integration_credentials WHERE org_id=$1 ORDER BY name`, orgID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err == nil {
			out = append(out, n)
		}
	}
	return out
}

func (s *PGStore) SetIntegrationConfig(orgID, name string, config map[string]any) {
	_, _ = s.pool.Exec(context.Background(),
		`INSERT INTO integration_configs(org_id, name, config, updated_at)
		 VALUES($1,$2,$3::jsonb,now())
		 ON CONFLICT (org_id, name) DO UPDATE SET config=EXCLUDED.config, updated_at=now()`,
		orgID, name, jsonParam(config))
}

func (s *PGStore) GetIntegrationConfig(orgID, name string) (map[string]any, bool) {
	var text string
	err := s.pool.QueryRow(context.Background(),
		`SELECT config::text FROM integration_configs WHERE org_id=$1 AND name=$2`,
		orgID, name).Scan(&text)
	if err != nil {
		return nil, false
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(text), &out); err != nil {
		return nil, false
	}
	return out, true
}

func (s *PGStore) ListMCPServers(orgID string) []domain.MCPServer {
	cols, scan := scanMCPServer(false)
	rows, err := s.pool.Query(context.Background(),
		`SELECT `+cols+` FROM mcp_servers WHERE org_id=$1 ORDER BY name`, orgID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []domain.MCPServer{}
	for rows.Next() {
		if srv, _, ok := scan(rows); ok {
			out = append(out, srv)
		}
	}
	return out
}

const grantCols = `grant_id::text, org_id::text, principal_id::text, agent_id::text,
	scope, constraints::text, environment, issued_by, issued_at, expires_at, revoked_at,
	(COALESCE(metadata->>'bootstrap','false') = 'true') AS bootstrap`

func scanGrant(row pgx.Row) (domain.AuthorityGrant, bool) {
	var g domain.AuthorityGrant
	var principal *string
	var constraintsText string
	if err := row.Scan(&g.ID, &g.OrgID, &principal, &g.AgentID, &g.Scope,
		&constraintsText, &g.Environment, &g.IssuedBy, &g.IssuedAt,
		&g.ExpiresAt, &g.RevokedAt, &g.Bootstrap); err != nil {
		return domain.AuthorityGrant{}, false
	}
	if principal != nil {
		g.PrincipalID = *principal
	}
	_ = json.Unmarshal([]byte(constraintsText), &g.Constraints)
	return g, true
}

func (s *PGStore) CreateGrant(g domain.AuthorityGrant) domain.AuthorityGrant {
	meta := map[string]any{}
	if g.Bootstrap {
		meta["bootstrap"] = "true"
	}
	row := s.pool.QueryRow(context.Background(),
		`INSERT INTO authority_grants(org_id, principal_id, agent_id, scope, constraints,
		                              environment, issued_by, expires_at, metadata)
		 VALUES($1,$2,$3,$4,$5::jsonb,$6,$7,$8,$9::jsonb)
		 RETURNING `+grantCols,
		g.OrgID, optUUID(g.PrincipalID), g.AgentID, g.Scope, jsonParam(g.Constraints),
		g.Environment, g.IssuedBy, g.ExpiresAt, jsonParam(meta))
	if created, ok := scanGrant(row); ok {
		return created
	}
	return g
}

func (s *PGStore) GrantsForAgent(orgID, agentID string) []domain.AuthorityGrant {
	rows, err := s.pool.Query(context.Background(),
		`SELECT `+grantCols+` FROM authority_grants
		 WHERE org_id=$1 AND agent_id=$2 ORDER BY issued_at`, orgID, agentID)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := []domain.AuthorityGrant{}
	for rows.Next() {
		if g, ok := scanGrant(rows); ok {
			out = append(out, g)
		}
	}
	return out
}

func (s *PGStore) RevokeGrant(orgID, grantID string) bool {
	ct, err := s.pool.Exec(context.Background(),
		`UPDATE authority_grants SET revoked_at=now()
		 WHERE org_id=$1 AND grant_id=$2 AND revoked_at IS NULL`, orgID, grantID)
	return err == nil && ct.RowsAffected() == 1
}

func (s *PGStore) RevokeCredential(keyID string) bool {
	ct, err := s.pool.Exec(context.Background(),
		`UPDATE agent_credentials SET revoked_at=now()
		 WHERE key_id=$1 AND revoked_at IS NULL`, keyID)
	return err == nil && ct.RowsAffected() == 1
}

func (s *PGStore) TouchCredential(keyID string) {
	_, _ = s.pool.Exec(context.Background(),
		`UPDATE agent_credentials SET last_used_at=now() WHERE key_id=$1`, keyID)
}

func (s *PGStore) TouchOperatorToken(keyID string) {
	_, _ = s.pool.Exec(context.Background(),
		`UPDATE operator_tokens SET last_used_at=now() WHERE key_id=$1`, keyID)
}

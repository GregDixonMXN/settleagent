# Domain Model

Multi-tenant via `organizations.org_id` on every row; all queries scoped server-side.

## organizations
- `org_id` (PK, uuid), `name`, `created_at`
- Root tenant. Everything hangs off `org_id`.

## principals
- `principal_id` (PK), `org_id` (FK), `kind` (`human` | `service`), `display_name`, `created_at`
- People / services that own agents and grant approvals.

## agents
- `agent_id` (PK), `org_id` (FK), `owner_principal_id` (FK → principals), `agent_group` (e.g. `support`, `deploy`, `data`), `display_name`, `status`, `created_at`
- Identity that proposes transactions. Group membership drives policy matching.

## agent_credentials
- `credential_id` (PK), `org_id` (FK), `agent_id` (FK → agents), `secret_hash` (never plaintext), `scopes`, `expires_at`, `revoked_at`, `created_at`
- Auth for agent API calls. Secrets stored hashed only; verification server-side; revocation checked on every request.

## transactions
- `txn_id` (PK), `org_id` (FK), `agent_id` (FK → agents), `state` (see transactions.md), `idempotency_key` (unique per org), `correlation_id`, `created_at`, `updated_at`
- Unit of governed work. State transitions persisted before side effects (crash-resumable).

## actions
- `action_id` (PK), `org_id` (FK), `txn_id` (FK → transactions), `tool`, `action` (verb), `args` (jsonb), `classes` (subset of `READ_ONLY REVERSIBLE COMPENSABLE IRREVERSIBLE FINANCIAL DESTRUCTIVE EXTERNAL_COMMUNICATION PRIVILEGED UNKNOWN`, combinable), `policy_result` (`ALLOW DENY REQUIRE_APPROVAL ALLOW_WITH_CONSTRAINTS`), `status` (`pending approved denied executing succeeded failed compensated`), `compensation` (jsonb, nullable), `idempotency_key`, `created_at`, `updated_at`
- `transactions 1—N actions`. Classes are combinable flags, not exclusive.

## policies
- `policy_id` (PK), `org_id` (FK), `version`, `name`, `rules` (jsonb, policy model v1), `is_active`, `created_by`, `created_at`
- Declarative JSON rules; evaluated in order, first match wins (see policies.md).

## approvals
- `approval_id` (PK), `org_id` (FK), `txn_id` (FK → transactions), `action_id` (FK → actions, nullable for txn-level), `requested_by_agent_id`, `decided_by_principal_id` (nullable until decided), `decision` (`pending granted denied expired`), `reason`, `expires_at`, `created_at`, `decided_at`
- Gate `AWAITING_APPROVAL → EXECUTING`. Expiry is treated as denial.

## receipts
- `receipt_id` (PK), `org_id` (FK), `txn_id` (FK → transactions), `action_id` (FK → actions, nullable), `payload` (canonical jsonb), `prev_hash`, `hash` (`sha256(prev_hash + canonical payload)`), `created_at`
- Append-only hash chain per org/txn. `prev_hash` = previous receipt's `hash` (`GENESIS` for first). Tamper-evident.

## audit_events
- `event_id` (PK), `org_id` (FK), `txn_id` (nullable FK), `actor` (agent or principal id), `kind`, `detail` (jsonb), `correlation_id`, `created_at`
- Append-only log of everything: state transitions, policy decisions, approvals, receipts, auth failures.

## Relationships
```
organizations 1—N principals, agents, transactions, policies, receipts, audit_events
principals 1—N agents (owner), approvals (decider)
agents 1—N agent_credentials, transactions
transactions 1—N actions, approvals, receipts
actions 1—N approvals (action-level)
```

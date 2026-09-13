# Transactions — State Machine

States: `CREATED PLANNING AWAITING_APPROVAL EXECUTING COMMITTED ABORTING COMPENSATING ROLLED_BACK PARTIALLY_COMPENSATED FAILED`

## Allowed transitions

| From | To | Trigger |
|---|---|---|
| `CREATED` | `PLANNING` | plan submitted (actions registered) |
| `CREATED` | `ABORTING` | policy DENY at plan time |
| `PLANNING` | `AWAITING_APPROVAL` | ≥1 action is `REQUIRE_APPROVAL` |
| `PLANNING` | `EXECUTING` | all actions ALLOW / ALLOW_WITH_CONSTRAINTS, no approval pending |
| `PLANNING` | `ABORTING` | any action DENY |
| `AWAITING_APPROVAL` | `EXECUTING` | all required approvals granted |
| `AWAITING_APPROVAL` | `ABORTING` | any approval denied or expired |
| `EXECUTING` | `COMMITTED` | all actions succeeded |
| `EXECUTING` | `COMPENSATING` | ≥1 action failed and compensations exist |
| `EXECUTING` | `FAILED` | ≥1 action failed, no compensation path (incl. irreversible) |
| `EXECUTING` | `ABORTING` | operator cancel / policy revocation mid-run |
| `ABORTING` | `ROLLED_BACK` | nothing executed yet, or all executed actions reversed |
| `ABORTING` | `FAILED` | abort could not complete cleanly |
| `COMPENSATING` | `ROLLED_BACK` | all compensations succeeded |
| `COMPENSATING` | `PARTIALLY_COMPENSATED` | some compensations failed |
| `PARTIALLY_COMPENSATED` | `PARTIALLY_COMPENSATED` | terminal (operator resolves remainder manually) |
| `COMMITTED` | — | terminal, no outgoing transitions |
| `ROLLED_BACK` | — | terminal, no outgoing transitions |
| `FAILED` | — | terminal, no outgoing transitions |

Terminal states: `COMMITTED`, `ROLLED_BACK`, `PARTIALLY_COMPENSATED`, `FAILED`.

## Invariants (enforced server-side)

1. **Denied never executes.** An action with policy result `DENY` is never dispatched. A transaction containing a denied action moves to `ABORTING`, never `EXECUTING`.
2. **Approval-gated never executes before approval.** Actions with result `REQUIRE_APPROVAL` block the `PLANNING → EXECUTING` path; the transaction must sit in `AWAITING_APPROVAL` until every required approval is granted. Execution with a pending/denied/expired approval is rejected.
3. **Committed is terminal.** No transition out of `COMMITTED`. No post-commit mutation, compensation, or rollback.
4. **Irreversible never rolled back.** Actions with class `IRREVERSIBLE` (and `FINANCIAL`/`DESTRUCTIVE` unless compensable by policy) have no compensation path. If one fails mid-transaction the transaction goes to `FAILED`, never `COMPENSATING`/`ROLLED_BACK` for that action. Successful irreversible actions are never compensated.
5. **Crash-resumable via persisted state.** Transaction state, per-action status, approvals, and policy decisions are persisted in Postgres before each side effect. On restart the executor reloads persisted state and resumes from the recorded state (`EXECUTING`/`COMPENSATING`/`ABORTING` continue; terminal states are left alone). Idempotency keys make re-driven tool calls safe.

## Execution uncertainty (M8)

An action whose side effects cannot be confirmed (timeout, connection lost
mid-call) enters status `unknown`, not `failed`. Unknown actions:

- produce no receipt (nothing confirmed, nothing attested),
- refuse execution retries until reconciled (a retry could duplicate a
  payment, email, or deletion),
- are settled by `POST /v1/actions/:id/reconcile` (operator), which asks
  the provider via the tool's reconciler: confirmed → `executed` + receipt;
  confirmed-absent → `failed`; unresolvable → stays `unknown`, visible in
  audit (`action.unknown`, `reconcile.no_reconciler`, `action.reconciled`).

6. **Uncertain never auto-retries.** Only reconciliation moves `unknown`.

## Receipt signatures (M8)

Receipts carry `key_id` + ed25519 `signature` over the receipt hash
(`AG_SIGNING_KEY` selects the key; unset means a logged ephemeral dev key).
`GET /v1/receipts/verify?transaction_id=` reports chain + per-receipt
signature validity; `POST /v1/receipts/verify` checks one receipt object.
Tamper-evident, not immutable — terminology matters.

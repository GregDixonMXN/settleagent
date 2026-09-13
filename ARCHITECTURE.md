# AgentGuard Architecture

## What this is

AgentGuard is a trust/transaction layer for AI agents. Every side-effecting
agent action goes through a preflight policy check, optional human approval,
guarded execution with compensation, and a tamper-evident receipt.

## Request path

```
Agent -> Gateway -> Policy -> Approval -> Executor -> Receipt
```

1. **Gateway** (`internal/gateway`): authenticates the agent (`internal/identity`),
   enforces org tenancy, checks the idempotency key, and creates a transaction row.
2. **Policy** (`internal/policies`): evaluates matching policies deterministically.
   Outcome is one of `ALLOW`, `DENY`, `REQUIRE_APPROVAL`, `ALLOW_WITH_CONSTRAINTS`.
3. **Approval** (`internal/approvals`): if required, parks the transaction in
   `awaiting_approval` until a human approves/rejects (with expiry). DENY stops here.
4. **Executor** (`internal/actions` + `internal/integrations`): runs the action's
   steps in order. Each step is classified `reversible` / `compensable` /
   `irreversible`. On failure, runs compensations in reverse order (saga).
5. **Receipt** (`internal/receipts` + `internal/audit`): writes a hash-chained
   receipt linking `prev_hash -> entry_hash`. Every state transition is also
   appended to the audit log (`internal/audit`, `internal/store`).

Transaction states (persisted in Postgres, never in memory only):

```
pending -> evaluating -> awaiting_approval -> executing -> completed
                    \-> denied          \-> failed -> compensating -> compensated
```

## Repo layout (modular monolith)

```
apps/api/                 HTTP server, routing, OpenAPI /v1 (thin; delegates inward)
apps/dashboard/           Next.js dashboard (approvals queue, receipts, audit view)
internal/identity/        agents, API keys, orgs, multi-tenant scoping
internal/gateway/         preflight orchestration, idempotency, tx lifecycle
internal/policies/        policy model + deterministic evaluator
internal/approvals/       approval workflow, expiry, notifications hook
internal/actions/         action registry, step runner, compensation (saga)
internal/integrations/    external side effects (CRM, refunds, etc.) behind interfaces
internal/receipts/        hash-chained receipt issuance + verification
internal/audit/           append-only audit log
internal/store/           Postgres access (one package per domain table set)
internal/api/             shared HTTP types / middleware (auth, org scope, OTel)
internal/store/migrations/  SQL migrations, embedded and applied on boot
deploy/docker/            Docker Compose: api + dashboard + postgres + otel-collector
examples/                 simple-agent, financial-agent, mcp-agent
packages/                 shared TS/Go client SDKs
tests/                    end-to-end MVP demo script
```

Rules:

- `apps/api` contains no business logic. It parses, authenticates, and calls
  `internal/*`.
- `internal/*` packages never import `apps/*`.
- Cross-domain calls go through small Go interfaces, not direct struct access.
- `internal/integrations` is the only package that touches the outside world.
- `internal/store` is the only package that touches SQL.

## Why these choices

- **Go modular monolith, not microservices.** One binary, one deploy, single
  Postgres transaction across gateway/policy/tx-state writes. Splitting early
  adds network failure modes we don't need at MVP scale.
- **PostgreSQL as the source of truth.** Transactions, approvals, receipts, and
  audit all need atomic writes + row-level locking (`FOR UPDATE` on approval).
  No separate event store needed.
- **Saga-style compensation, not 2PC.** External systems (Stripe-like refunds,
  CRM) don't participate in our DB transactions. Each step declares how to undo
  itself; on failure we compensate in reverse order.
- **Deterministic policy evaluator, no LLM in the decision path.** The evaluator
  is pure functions over (agent, action, args, org limits): match rules in
  priority order, first match wins. Same input always gives the same outcome.
  Auditable and testable.
- **No Kafka / message queue.** Approval waits and step execution fit in
  Postgres state + API polling. A queue adds ops burden with no MVP benefit.
  If step volume grows, extract a worker behind the existing `actions` interface.
- **Hash-chained receipts, not a blockchain.** Each receipt stores
  `prev_hash` and `entry_hash = sha256(prev_hash || canonical_json(entry))`.
  Tampering with any entry breaks the chain. Verifiable with SQL + sha256,
  no consensus needed.
- **Persisted states, not in-memory.** Every transition (`pending`,
  `awaiting_approval`, `executing`, `completed`, `failed`, `compensated`,
  `denied`) is a DB row update. Crashes resume by re-reading the row.

## Component boundaries (how to split later)

Each `internal/*` package owns its tables and exposes an interface:

| Package | Owns | Split trigger |
|---|---|---|
| `identity` | orgs, agents, keys | needs separate auth service / SSO |
| `gateway` | transactions, idempotency keys | throughput needs dedicated ingress |
| `policies` | policies, evaluations | needs versioned policy distribution |
| `approvals` | approvals, expiry | needs push/Slack/pager integrations |
| `actions` | action defs, step runs | needs async worker pool |
| `integrations` | connectors | per-connector scaling / isolation |
| `receipts`/`audit` | receipts, audit entries | needs separate compliance store |

To split: move the package to its own module + service, keep the interface,
replace the in-process call with HTTP/gRPC. No other package changes.

## Open-source / commercial boundary

Open source (this repo): gateway, policy engine, approvals, saga executor,
receipts/audit, Postgres store, dashboard, examples, REST/OpenAPI.

Commercial (not here): hosted multi-region control plane, SSO/SAML, SLA-backed
connector pack (Salesforce, NetSuite, Workday), anomaly detection on audit log,
retention/legal-hold exports. Commercial features consume the same `/v1` API
and never fork the policy or receipt logic.

# AgentGuard Roadmap

Milestones are small vertical slices. Each ends with a runnable demo against the
MVP success path: **agent requests action → gateway evaluates policy →
approval (if required) → execution with idempotency → receipt + append-only audit.**

## M0 — Scaffold + vertical slice

Goal: repo builds, one happy-path action end-to-end.

- `apps/api` serves `POST /v1/actions/evaluate` and `POST /v1/actions/execute`
  (stub executor, no real integrations).
- Minimal policy: static allow-list in code (`internal/policies`).
- Request/response schemas frozen in `packages/protocol`.
- Postgres via `migrations/` + `docker compose up db`; `internal/store` connects.
- `make dev`, `make test`, `make lint` work from a clean clone.
- `examples/simple-agent` runs the happy path with a hardcoded API key.

Exit criteria:
- [ ] `docker compose up` + `make dev` boots API + DB with zero manual SQL.
- [ ] `examples/simple-agent` completes one allow-listed action and prints a receipt ID.
- [ ] `go build ./...` and `go test ./...` green in CI.

## M1 — Policy engine

Goal: declarative policies with deny-by-default and reasons.

- Policy DSL (YAML): match on `agent_id`, `action`, `resource`, `amount`,
  `time_window`, velocity limits; rule order: explicit deny > allow > default deny.
- `internal/policies`: load, validate, hot-reload from file; evaluation returns
  `allow | require_approval | deny` + human-readable reason.
- `examples/financial-agent`: spend-limit + velocity policy.
- Unit tests: precedence, default-deny, malformed policy rejected at load.

Exit criteria:
- [ ] Deny-by-default proven by test (unknown action → deny with reason).
- [ ] Policy file edit takes effect without API restart.
- [ ] `POST /v1/actions/evaluate` returns decision + reason + matched rule ID.

## M2 — Approvals + dashboard

Goal: human-in-the-loop for sensitive actions, visible in UI.

- `internal/approvals`: create / approve / reject / expire; single approver,
  TTL (default 15 min); action executes only with a valid approval token.
- `apps/dashboard`: pending queue, approve/reject buttons, decision + receipt view.
- Gateway holds execution until approval resolves; expiry → deny.
- Auth: API keys for agents, session/OIDC stub for dashboard (hardcoded role in MVP).

Exit criteria:
- [ ] `require_approval` action pauses; approval in dashboard unblocks execution.
- [ ] Expired approval cannot be used to execute (tested).
- [ ] Dashboard shows decision history for the demo agent.

## M3 — Compensation / idempotency

Goal: safe retries, no double-spend, undo for reversible actions.

- Idempotency keys on `execute`: same key → same receipt, no re-execution
  (unique constraint in `internal/store`).
- Ledger in `internal/transactions`: debit/credit with exactly-once semantics
  per idempotency key (double-spend refund scenario covered by test).
- `internal/actions`: compensating action registry (e.g. `refund` reverses `charge`);
  failed multi-step execution triggers compensation and records it.
- Receipt (`internal/receipts`) includes idempotency key + compensation status.

Exit criteria:
- [ ] Replaying `execute` with the same key returns the original receipt, no side effect.
- [ ] Concurrent duplicate executes (10x parallel) produce exactly one ledger entry.
- [ ] Failed 2-step demo action leaves a compensation record in the audit trail.

## M4 — Integrations + MCP proxy stub

Goal: real tool calls go through the gateway, including one MCP server.

- `internal/integrations`: adapter interface (`Execute(ctx, action) (result, error)`);
  two adapters: HTTP webhook + local stub.
- `internal/gateway`: MCP proxy stub — whitelisted tools only, request/response
  size caps, timeout, response schema validation; unlisted tool → deny.
- `examples/mcp-agent`: calls through the proxy; malicious/prompt-injected tool
  response is logged and blocked from auto-execution.
- Secrets for adapters from env, never from agent input.

Exit criteria:
- [ ] Demo agent triggers a real webhook call via gateway policy check.
- [ ] Non-whitelisted MCP tool call is denied and audited.
- [ ] Oversized/malformed MCP response is rejected before reaching the agent.

## M5 — Hardening / CI / observability

Goal: MVP is boring to operate and auditable.

- AuthN/Z: per-tenant API keys (bcrypt-hashed), tenant scoping on every query,
  rate limiting per key.
- Audit (`internal/audit`): hash-chained append-only log; startup verification;
  tamper → loud failure, not silent skip.
- CI: build, test (incl. `-race`), lint, `gosec`, migration check, Trivy on image.
- Observability: structured JSON logs (request ID), `/healthz` + `/readyz`,
  Prometheus metrics (decisions, approvals, execution latency), OpenTelemetry stub.
- `deploy/docker`: pinned base images, non-root user, read-only FS, no secrets in image.

Exit criteria:
- [ ] Cross-tenant read returns empty/denied (tested at store + API layers).
- [ ] Audit chain verification passes; single-byte tamper is detected in test.
- [ ] CI green on a fresh PR; image builds from `deploy/docker` with no `latest` tags.
- [ ] MVP success demo runs end-to-end from docs with copy-paste commands.

## M6 — Post-MVP (explicitly out of scope)

Multi-approver/quorum, OPA/Rego or Cedar policy backend, KMS-backed receipt
signing, SIEM export, per-action sandboxing, full MCP spec coverage, SLA/SLOs.

Non-goals for M0–M5: multi-region, HA failover, billing, agent reputation scoring.

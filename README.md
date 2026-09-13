# AgentGuard

Trust and transaction infrastructure for autonomous AI agents. Every
consequential action passes through identity, delegated authority, policy,
approval, guarded execution, and tamper-evident receipts:

    Agent → AgentGuard → Policy / Authority / Approvals → Tool / MCP / API

See [ARCHITECTURE.md](ARCHITECTURE.md), [docs/](docs/), and [docs/adr](docs/adr).

## Quickstart (local)

Prereqs: Go 1.26+, Node 20+, Python 3.12. Docker optional (compose),
Postgres optional (memory store by default).

```bash
# 1. API (prints demo org, principal, and a one-time operator token).
# AG_DEMO_MOCKS=1 keeps the demo on mock tools; without it, register
# live test credentials per org (see docs/integrations.md).
AG_DEMO_MOCKS=1 go run ./apps/api
# store: memory (demo org <ORG> principal <PRIN>)
# operator token (dashboard/human): ago_...

# 2. Governed demo agent (new terminal; uses values from step 1)
API_URL=http://127.0.0.1:8080 ORG=<ORG> PRINCIPAL=<PRIN> \
  OPERATOR_TOKEN=<ago_...> python3 examples/simple-agent/demo.py
# -> CRM update ALLOW, $80 refund ALLOW, $300 refund approval -> approved,
#    $2000 refund DENY with reasons, 20-event audit timeline, DEMO OK

# 3. Dashboard (new terminal)
cd apps/dashboard && npm install
NEXT_PUBLIC_API_URL=http://127.0.0.1:8080 NEXT_PUBLIC_API_TOKEN=<ago_...> npm run dev
# open http://localhost:3000 (Transactions, Approvals, Receipts, Audit)
```

With Postgres (durable): set `DATABASE_URL` before starting the API;
migrations apply automatically and incomplete transactions are reported
on boot. Compose (`deploy/docker/docker-compose.yml`) runs Postgres +
API + dashboard + Jaeger: `docker compose up --build` (dashboard on
:3000, Jaeger traces on :16686).

## What the demo proves

| Action | Outcome |
|---|---|
| `crm.lookup_customer`, `crm.update_record` | ALLOW, executed |
| `stripe.refund` $80 | ALLOW (support auto-limit $100), executed |
| `stripe.refund` $300 | REQUIRE_APPROVAL → human approves → executed |
| `stripe.refund` $2000 | DENY ("above $500 prohibited"), never executes |
| Failure after compensable step | compensates; `PARTIALLY_COMPENSATED` if irreversible steps ran |
| Duplicate delivery (same idempotency key) | returns original record, no duplicate side effect |

Every request carries `Authorization: Bearer` (agent `ag_…` or operator
`ago_…` secret); every action needs an `idempotency_key`; every denial
explains why.

## Repo map

- `apps/api` — HTTP gateway (`/v1`, OpenAPI at `/openapi.json`)
- `apps/dashboard` — Next.js control plane (overview, transactions+timeline,
  approvals, agents, authority, policies, receipts, audit)
- `internal/gateway` — transaction engine (saga semantics, never fake atomicity)
- `internal/policies` — deterministic policy engine (versioned sets, simulation)
- `internal/auth` — bearer credentials, operator tokens, rate limiting
- `internal/integrations` — Stripe test-mode, GitHub, Postgres read-only,
  HTTP allowlist, MCP proxy (mocks where live creds are absent)
- `internal/store` — Memory (dev) + Postgres (prod), embedded migrations
- `packages/sdk-python` — Python SDK (Bearer, idempotency, approvals, verify)
- `examples/` — `simple-agent` (support flow), `financial-agent`
  (compensation), `signature-demo` (website-video scenario)
- `deploy/docker`, `deploy/tofu` — compose stack + OpenTofu production stack
- `tests/` — invariant, auth, MCP, reconcile, policy-V2, Postgres suites

# AgentGuard

Trust/transaction layer for AI agents. Every side-effecting action goes through
preflight policy eval (`ALLOW` / `DENY` / `REQUIRE_APPROVAL` /
`ALLOW_WITH_CONSTRAINTS`), optional human approval, guarded execution with
saga compensation, and a hash-chained receipt.

See [ARCHITECTURE.md](ARCHITECTURE.md) and [docs/adr](docs/adr).

## Quickstart

Prereqs: Docker + Docker Compose, Go 1.22+, Node 20+.

```bash
docker compose -f deploy/docker/docker-compose.yml up --build
```

- API: `http://localhost:8080/v1` (OpenAPI at `/v1/openapi.yaml`)
- Dashboard: `http://localhost:3000` (approvals queue, receipts, audit)
- Postgres: `localhost:5432`

Seed the demo org, agent, and policies:

```bash
go run ./apps/api/seed
```

## Example agent

`examples/simple-agent` shows the minimal loop: submit an action, handle the
verdict, poll approvals, fetch the receipt.

```bash
cd examples/simple-agent
export AGENTGUARD_URL=http://localhost:8080 AGENTGUARD_API_KEY=$SEED_KEY
go run . --action crm.update --args '{"contact":"acme","field":"tier","value":"pro"}'
# -> ALLOW, executes, prints receipt id + entry_hash
```

Refund policy thresholds (seeded demo):

| Action | Outcome |
|---|---|
| `crm.update` | ALLOW |
| `refund $80` | ALLOW |
| `refund $300` | REQUIRE_APPROVAL (approve in dashboard at localhost:3000) |
| `refund $2000` | DENY |
| failing step after a compensable step | compensates, receipt shows `compensated` |

```bash
go run . --action refund.issue --args '{"amount_cents":8000,"order":"o_1"}'
go run . --action refund.issue --args '{"amount_cents":30000,"order":"o_2"}'
# then approve o_2 at http://localhost:3000/approvals
go run . --action refund.issue --args '{"amount_cents":200000,"order":"o_3"}'
```

Every request takes an `Idempotency-Key` header; retries with the same key
return the original transaction instead of re-executing.

## Repo map

- `apps/api` — HTTP server, `/v1` routes (thin)
- `apps/dashboard` — Next.js dashboard
- `internal/` — identity, gateway, policies, approvals, actions, integrations,
  receipts, audit, store
- `internal/store/migrations/` — SQL schema (embedded, applied on boot)
- `tests/` — end-to-end MVP demo script

# SettleAgent Dashboard

Minimal Next.js (pages router) + TypeScript UI for the SettleAgent API. Boring, Stripe-like.

## Pages

- `/` — overview metrics + recent transactions
- `/transactions` — list + filter
- `/transactions/[id]` — detail with timeline (audit + actions + receipts), actions table, receipts JSON, audit table
- `/approvals` — pending queue with Approve / Deny buttons
- `/agents` — register agent form (secret shown once)
- `/receipts`, `/audit` — filterable by `transaction_id`

## Dev

```bash
cd apps/dashboard
npm install
NEXT_PUBLIC_API_URL=http://localhost:8080 NEXT_PUBLIC_API_TOKEN=<operator-token> npm run dev
# open http://localhost:3000
```

API must be running on :8080. The tenant org comes from the bearer
credential, not headers — pass an operator token (`sto_...`, printed once
in the API boot log) as NEXT_PUBLIC_API_TOKEN so register/approve work.

## Operator gate

Set DASHBOARD_PASSWORD to require login (single-operator cookie session,
v0.1 interim — no multi-user/SSO yet). Unset means open (local dev only).

## Build / typecheck

```bash
npm run typecheck   # tsc --noEmit
npm run build
```

## Docker

```bash
docker build -t settleagent-dashboard ./apps/dashboard
docker run -p 3000:3000 -e NEXT_PUBLIC_API_URL=http://localhost:8080 settleagent-dashboard
```

Or via compose from repo root: `docker compose -f deploy/docker/docker-compose.yml up --build`.

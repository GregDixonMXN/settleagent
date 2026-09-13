# AgentGuard Dashboard

Minimal Next.js (pages router) + TypeScript UI for the AgentGuard API. Boring, Stripe-like.

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
NEXT_PUBLIC_API_URL=http://localhost:8080 NEXT_PUBLIC_ORG_ID=org_demo npm run dev
# open http://localhost:3000
```

API must be running on :8080 with `X-Org-ID` scoping (the dashboard sends
`NEXT_PUBLIC_ORG_ID`, default `org_demo`).

## Build / typecheck

```bash
npm run typecheck   # tsc --noEmit
npm run build
```

## Docker

```bash
docker build -t agentguard-dashboard ./apps/dashboard
docker run -p 3000:3000 -e NEXT_PUBLIC_API_URL=http://localhost:8080 agentguard-dashboard
```

Or via compose from repo root: `docker compose -f deploy/docker/docker-compose.yml up --build`.

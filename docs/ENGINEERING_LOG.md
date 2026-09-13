# Engineering log

Concise record of meaningful work: date, milestone, changes, decisions,
known limitations, next step.

## 2026-09-13 — v0.2 audit (M7 prep)

- Verified v0.1: build + full suite green (incl. race), live enforcement
  paths on memory + Postgres, joint Ballast run.
- Joint test caught org-global idempotency keys replaying another txn's
  actions; fixed by scoping to (org, transaction), migration 005,
  regression test.
- Wrote docs/V0.2_AUDIT.md: keeps / incomplete / unsafe / refactor / delete
  / risks + cut-down M7–M10 scope (deferred: CLI, webhooks, risk budgets,
  trust scores, TS SDK, live UI, multi-replica limiter, K8s docs).
- Removed dead `var _ = policies.Evaluate` from api/server.go.
- Updated ROADMAP (v0.2 section), THREAT_MODEL (items 12–15).
- Known limitations: MCP tokens reversible at rest; limiter single-process;
  dashboard token browser-visible behind login gate.
- Next: M7 authority grants.

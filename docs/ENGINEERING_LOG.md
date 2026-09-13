# Engineering log

Concise record of meaningful work: date, milestone, changes, decisions,
known limitations, next step.

## 2026-09-13 — M7 authority grants + credential lifecycle

- AuthorityGrant model (scope tool.action/tool.*/*, max-amount + environment
  constraints, expiry, revocation, bootstrap flag), migration 006.
- Enforcement BEFORE policy in ProposeAction; no-grants agents run
  policy-only (audit-logged migration path); uncovered ops deny with
  AUTHORITY_EXCEEDED + human why. Reason codes on all decisions and audit.
- Endpoints: grants CRUD-ish (create/list/revoke, operator), credential
  rotate/revoke; last-used tracking on every auth; revoked creds rejected
  in keyed lookup and legacy fallback.
- Dashboard Authority page (read); SDK grant/credential methods; protocol
  docs updated. Live-verified all five paths (policy-only, covered,
  over-cap, out-of-scope, revoked).
- Known limitations: policy-only mode is operator opt-in by neglect —
  document narrowing bootstrap grants; legacy fallback hashes have no
  per-key revocation.
- Next: M8 execution uncertainty + receipt signatures.

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

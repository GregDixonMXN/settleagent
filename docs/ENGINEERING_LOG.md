# Engineering log

Concise record of meaningful work: date, milestone, changes, decisions,
known limitations, next step.

## 2026-09-13 — v0.2 completion pass

- Fresh-clone verification: build, vet, tests, dashboard install+build,
  compose config, tofu validate — all green from a clean clone.
- Rewrote README to match reality (auth env, mocks flag, real commands).
- Found no COMMITTED path: added explicit commit (refuses unresolved
  work, terminal once) + endpoint + tests + SDK.
- examples/signature-demo: 5-act narrated scenario (ALLOW/commit,
  approval/commit, DENY, DROP DATABASE DENY via v2, MCP shred DENY,
  verify finale with 4 valid signatures). Green end to end.
- Next: v0.2 done. Candidates: persistent staging + Ballast production
  pairing, Tryout spend guard, or first external user.

## 2026-09-13 — M10 real integrations + sealed credentials

- Envelope encryption (AES-256-GCM, keys.Provider; AG_DATA_KEY or derived;
  legacy plaintext reads through); migration 009 credential/config tables;
  MCP tokens sealed; per-org credential + allowlist endpoints.
- Live integrations: Stripe test-mode (live keys refused twice, idempotency
  forwarded, charge+amount reconciler), GitHub (6 actions, merges
  privileged), Postgres SELECT-only (read-only txn, timeout, row cap,
  single statement), HTTP allowlist + non-public-IP refusal.
- Unconfigured integrations fail loudly; AG_DEMO_MOCKS=1 keeps local demo
  on mocks with a loud boot log. docs/ballast.md pairing guide.
- Live-verified: real SELECT rows, all refusal messages, names-only
  inventory, sealed round-trip. Tests: stub-backed Stripe/GitHub/HTTP,
  guardrail matrix for SQL, seal round-trip + legacy.
- Known limitations: no GitHub/Stripe reconcilers beyond Stripe refunds;
  per-call PG connects (no pool cache); DNS-rebinding residual on HTTP;
  shell deferred.
- Next: v0.2 completion pass (demo video script + fresh-clone check).

## 2026-09-13 — M9 policy V2 core

- Versioned policy_sets (one active per org, DB-enforced); draft/activate
  endpoints with change-audit events; decisions + receipts stamp version;
  history immutable (test asserts old receipt keeps v1).
- Simulation endpoint (authority + policy, zero persistence) + dashboard
  Policies page (versions, raw-JSON draft, test mode).
- V2 conditions: classifications, environments, resource prefix, UTC time
  windows (overnight wrap); expressiveness deliberately capped.
- Live-verified: simulate → approval-required v1; draft v2; activate;
  email now DENY v2; history intact. Docs/policies.md reconciled with
  implemented schema.
- Known limitations: no structured form authoring (raw JSON only); no
  per-rule test-against-history; Policies() fallback keeps legacy rows.
- Next: M10 real integrations.

## 2026-09-13 — M8 execution uncertainty + receipt signatures

- `unknown` action status for unconfirmable side effects (typed
  UncertainError; MCP transport failures map to it); no receipt, no blind
  retry; reconcile endpoint (operator) settles via per-tool reconcilers
  (confirmed → executed+receipt, absent → failed, none → stays visible).
- Ed25519 receipt signatures (keys.Provider abstraction for future KMS +
  M10 encryption); migration 007; verify endpoints (txn + single object);
  ephemeral-dev key warning when AG_SIGNING_KEY unset.
- `action.replayed` audit labels distinguish replay from fresh execution.
- Live-verified: signed receipt validates, tampered/unsigned rejected,
  replay labeled. Tests: uncertain park, confirm/absent, sig+tamper.
- Known limitations: no background reconciliation worker (endpoint +
  boot-report only); MCP has no reconcilers registered yet (unknown stays
  until integrations provide them); single-key signing, no rotation.
- Next: M9 policy V2.

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

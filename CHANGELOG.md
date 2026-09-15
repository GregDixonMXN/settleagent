# Changelog

## v0.2.0

- Renamed AgentGuard to SettleAgent: product, Go module
  (`github.com/GregDixonMXN/settleagent`), credential prefixes (`st_`,
  `sto_`), Python SDK package, dashboard. Receipt chain format unchanged.
- Dashboard operator token is server-side only. Pages call same-origin
  `/api/backend`, which attaches `SETTLEAGENT_OPERATOR_TOKEN`; the browser
  bundle contains no credential.
- Pilot onboarding doc (`PILOT.md`): two-week plan, scope, start path.
- Authority grants with expiry and revocation; versioned policy sets with
  simulation; receipt signatures; real integrations (Stripe test-mode,
  GitHub, read-only Postgres, HTTP domain allowlist); sealed credentials
  with rotation, revocation, and last-used tracking; Ballast pairing docs.

Support intent: single-node Postgres or memory store; rate limiting is
per-process. SSO/SAML, SIEM export, and SLA are Enterprise scope.
See THREAT_MODEL.md for boundaries, including no protection against a
compromised gateway host.

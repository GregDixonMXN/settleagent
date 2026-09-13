# Security Policy

## Report a vulnerability

Email the maintainers (see repo owners) with: affected version/commit, steps to
reproduce, and impact. Do not open a public issue for unpatched vulns. Expect
acknowledgement within 2 business days; we aim to ship a fix within 14 days
for high severity.

## Operational rules

- Secrets only via environment / secret manager. Never commit `.env`, keys, or
  tokens. `git commit` is blocked on secret patterns by CI (`gitleaks`).
- API keys stored bcrypt-hashed; approval tokens single-use with TTL.
- Postgres role for the API: no `UPDATE`/`DELETE` on audit tables (append-only).
- Images: pinned digests, non-root, read-only FS; no `latest` tags.
- Agent-facing errors are generic; details go to server logs only.
- Dependencies: `go mod` tidy + `govulncheck` in CI; update monthly.

## Local dev

- Copy `.env.example` to `.env` (never commit `.env`).
- `docker compose up db` then `make dev`. Test keys in `.env.example` are
  dev-only and rejected if `APP_ENV=production`.

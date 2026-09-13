# Contributing

## Setup

```bash
docker compose up -d db
make dev      # API on :8080, dashboard on :3000
make test     # go test ./... (with -race)
```

## Workflow

1. Branch from `master`: `feat/<scope>` or `fix/<scope>`.
2. Keep changes to one milestone slice (see `ROADMAP.md`); update the slicing
   checkbox only when exit criteria are actually met.
3. Add/extend tests for policy, approval, idempotency, and tenant-scoping
   changes — no untested security-path code.
4. Run before pushing: `make test && make lint && go vet ./...`.

## PR rules

- Small (<400 lines), description links the milestone + exit criterion.
- New actions must register in `internal/actions` with schema + compensation
  (or explicit `irreversible: true` + required approval).
- New MCP tools default to **not** allow-listed.
- CI must be green: build, tests, lint, `gosec`, migration check, image scan.

## Style

Go: `gofmt` + `goimports`, exported symbols documented. Config via env with
sane dev defaults; production requires explicit secrets (fail closed).

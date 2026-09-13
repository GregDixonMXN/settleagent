# ADR-001: Go Modular Monolith

Decision: Build the backend as a single Go binary with `internal/*` domain packages.

Why: one deploy, single Postgres transaction across preflight writes, no network
failure modes at MVP scale; packages split into services later behind existing interfaces.

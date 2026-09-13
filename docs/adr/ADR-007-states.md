# ADR-007: Persisted Transaction States

Decision: Every transaction state transition is a Postgres row update
(`pending`, `awaiting_approval`, `executing`, `completed`, `failed`,
`compensated`, `denied`); crashes resume by re-reading the row.

Why: no in-memory-only workflow state, so restarts never lose or double-execute
a side effect (combined with idempotency keys).

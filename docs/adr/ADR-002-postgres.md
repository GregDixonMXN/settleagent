# ADR-002: PostgreSQL as Source of Truth

Decision: Persist transactions, approvals, receipts, and audit in PostgreSQL.

Why: atomic preflight writes plus `SELECT ... FOR UPDATE` on approval races;
no separate event store or queue needed for MVP.

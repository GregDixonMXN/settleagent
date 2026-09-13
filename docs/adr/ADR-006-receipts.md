# ADR-006: Hash-Chained Receipts

Decision: Each receipt stores `prev_hash`; `entry_hash = sha256(prev_hash ||
canonical_json(entry))`.

Why: any tampering breaks the chain; verifiable with SQL + sha256, no
blockchain or consensus needed.

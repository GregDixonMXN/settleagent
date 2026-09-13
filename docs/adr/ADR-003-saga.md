# ADR-003: Saga Compensation

Decision: Classify every action step as reversible / compensable / irreversible and
compensate in reverse order on failure.

Why: external systems (refunds, CRM) can't join our DB transaction; saga gives
defined rollback without 2PC.

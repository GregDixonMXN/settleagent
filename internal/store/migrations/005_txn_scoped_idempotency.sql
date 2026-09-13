-- 005_txn_scoped_idempotency.sql — idempotency keys are scoped to
-- (org, transaction). The same key in a new transaction is a different
-- action; without this a rerun silently replays another txn's actions.

ALTER TABLE actions DROP CONSTRAINT IF EXISTS actions_org_id_idempotency_key_key;
ALTER TABLE actions
  ADD CONSTRAINT actions_org_txn_idempotency_key UNIQUE (org_id, txn_id, idempotency_key);

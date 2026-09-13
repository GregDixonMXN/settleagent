-- 007_receipt_signatures.sql — server signatures on receipts.
-- Each receipt carries the signing key id + ed25519 signature over the
-- receipt hash. Verification is offline-capable given the public key.

ALTER TABLE receipts
  ADD COLUMN IF NOT EXISTS key_id TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS signature TEXT NOT NULL DEFAULT '';

-- Execution uncertainty: an action whose side effects cannot be confirmed
-- (timeout, connection lost mid-call) waits for reconciliation instead of
-- being retried blind or marked failed.
ALTER TABLE actions DROP CONSTRAINT IF EXISTS actions_status_check;
ALTER TABLE actions
  ADD CONSTRAINT actions_status_check CHECK (status IN
    ('proposed','allowed','awaiting_approval','approved','denied',
     'executing','executed','succeeded','failed','unknown',
     'compensated','compensation_failed'));

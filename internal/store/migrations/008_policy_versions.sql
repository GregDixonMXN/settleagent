-- 008_policy_versions.sql — versioned policy sets.
-- Exactly one ACTIVE set per org evaluates live traffic. Changes create new
-- versions; history is immutable. Receipts stamp the producing version.

CREATE TABLE IF NOT EXISTS policy_sets (
  set_id     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id     UUID NOT NULL REFERENCES organizations(org_id) ON DELETE CASCADE,
  version    INT NOT NULL,
  rules      JSONB NOT NULL DEFAULT '[]',
  status     TEXT NOT NULL DEFAULT 'draft'
             CHECK (status IN ('draft','active','disabled')),
  created_by TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (org_id, version)
);
CREATE INDEX IF NOT EXISTS idx_policy_sets_org_status ON policy_sets(org_id, status);
-- Exactly one active set per org, enforced by the database.
CREATE UNIQUE INDEX IF NOT EXISTS idx_policy_sets_org_active
  ON policy_sets(org_id) WHERE status = 'active';

ALTER TABLE receipts
  ADD COLUMN IF NOT EXISTS policy_version INT NOT NULL DEFAULT 0;

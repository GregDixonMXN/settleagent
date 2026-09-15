-- 003_auth.sql — key-ID credential lookup + human operator tokens.
-- Secrets are bcrypt hashes only. key_id is the plaintext lookup handle:
-- agent secret   st_<keyid>_<random>   (bound to one agent)
-- operator secret sto_<keyid>_<random> (org-wide human/dashboard access)

ALTER TABLE agent_credentials
  ADD COLUMN IF NOT EXISTS key_id TEXT;

-- Backfill: existing rows (issued pre-key-ID) get a NULL key_id and keep
-- working through the bounded per-org fallback scan in the auth package.
CREATE UNIQUE INDEX IF NOT EXISTS idx_agent_credentials_key_id
  ON agent_credentials(key_id) WHERE key_id IS NOT NULL;

CREATE TABLE IF NOT EXISTS operator_tokens (
  token_id    UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id      UUID NOT NULL REFERENCES organizations(org_id) ON DELETE CASCADE,
  key_id      TEXT NOT NULL UNIQUE,
  name        TEXT NOT NULL DEFAULT '',
  secret_hash TEXT NOT NULL,
  revoked_at  TIMESTAMPTZ,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS idx_operator_tokens_org ON operator_tokens(org_id);

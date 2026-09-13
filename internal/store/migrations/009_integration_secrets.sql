-- 009_integration_secrets.sql — per-org third-party credentials + config.
-- Secrets are sealed with the keys.Provider before storage ("v1:" prefix).
-- Plaintext legacy rows (MCP tokens written pre-M10) read through as-is.

CREATE TABLE IF NOT EXISTS integration_credentials (
  org_id     UUID NOT NULL REFERENCES organizations(org_id) ON DELETE CASCADE,
  name       TEXT NOT NULL,
  secret_enc TEXT NOT NULL DEFAULT '',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (org_id, name)
);

CREATE TABLE IF NOT EXISTS integration_configs (
  org_id     UUID NOT NULL REFERENCES organizations(org_id) ON DELETE CASCADE,
  name       TEXT NOT NULL,
  config     JSONB NOT NULL DEFAULT '{}',
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  PRIMARY KEY (org_id, name)
);

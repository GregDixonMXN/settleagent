-- 006_authority.sql — delegated authority grants + credential lifecycle.
-- A grant states what an agent may ATTEMPT (scope + constraints). Policy
-- still decides the final outcome. No grants configured for an agent means
-- policy-only mode (audit-logged); the first grant switches that agent to
-- authority-first enforcement. Deliberate migration path, not a hole.

CREATE TABLE IF NOT EXISTS authority_grants (
  grant_id     UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id       UUID NOT NULL REFERENCES organizations(org_id) ON DELETE CASCADE,
  principal_id UUID REFERENCES principals(principal_id),
  agent_id     UUID NOT NULL REFERENCES agents(agent_id) ON DELETE CASCADE,
  scope        TEXT[] NOT NULL DEFAULT '{}',
  constraints  JSONB NOT NULL DEFAULT '{}',
  environment  TEXT NOT NULL DEFAULT '',
  issued_by    TEXT NOT NULL DEFAULT '',
  issued_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  expires_at   TIMESTAMPTZ,
  revoked_at   TIMESTAMPTZ,
  metadata     JSONB NOT NULL DEFAULT '{}'
);
CREATE INDEX IF NOT EXISTS idx_grants_org_agent ON authority_grants(org_id, agent_id);

ALTER TABLE agent_credentials
  ADD COLUMN IF NOT EXISTS last_used_at TIMESTAMPTZ;
ALTER TABLE operator_tokens
  ADD COLUMN IF NOT EXISTS last_used_at TIMESTAMPTZ;

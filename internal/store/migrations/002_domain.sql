-- 002_domain.sql — align schema with the domain model (001 stays immutable).
-- Adds columns the gateway actually persists; widens checks to real statuses.

-- principals: USER / ORGANIZATION (+ future SERVICE / AGENT)
ALTER TABLE principals DROP CONSTRAINT IF EXISTS principals_kind_check;
ALTER TABLE principals
  ADD CONSTRAINT principals_kind_check
  CHECK (kind IN ('USER','ORGANIZATION','SERVICE','AGENT'));

-- agents: environment + multi-group membership
ALTER TABLE agents
  ADD COLUMN IF NOT EXISTS environment TEXT NOT NULL DEFAULT 'production',
  ADD COLUMN IF NOT EXISTS groups TEXT[] NOT NULL DEFAULT '{}';

-- transactions: principal, session, objective, risk budget
ALTER TABLE transactions
  ADD COLUMN IF NOT EXISTS principal_id UUID REFERENCES principals(principal_id),
  ADD COLUMN IF NOT EXISTS session_id TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS objective TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS risk_budget_cents BIGINT;

-- actions: execution lifecycle fields + real status set
ALTER TABLE actions DROP CONSTRAINT IF EXISTS actions_status_check;
ALTER TABLE actions
  ADD COLUMN IF NOT EXISTS seq INT NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS args_hash TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS amount_cents BIGINT,
  ADD COLUMN IF NOT EXISTS latency_ms BIGINT NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS result JSONB,
  ADD COLUMN IF NOT EXISTS error TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS decision JSONB,
  ADD CONSTRAINT actions_status_check CHECK (status IN
    ('proposed','allowed','awaiting_approval','approved','denied',
     'executing','executed','succeeded','failed',
     'compensated','compensation_failed'));

-- policy rules as first-class rows (v1 declarative model: match/effect/explanation)
CREATE TABLE IF NOT EXISTS policy_rules (
  policy_rule_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id         UUID NOT NULL REFERENCES organizations(org_id) ON DELETE CASCADE,
  name           TEXT NOT NULL,
  description    TEXT NOT NULL DEFAULT '',
  priority       INT NOT NULL DEFAULT 100,
  match          JSONB NOT NULL DEFAULT '{}',
  effect         TEXT NOT NULL CHECK (effect IN ('ALLOW','DENY','REQUIRE_APPROVAL','ALLOW_WITH_CONSTRAINTS')),
  explanation    TEXT NOT NULL DEFAULT ''
);
CREATE INDEX IF NOT EXISTS idx_policy_rules_org ON policy_rules(org_id, priority);

-- approvals: free-text decider + financial exposure
ALTER TABLE approvals
  ALTER COLUMN requested_by_agent_id DROP NOT NULL,
  ADD COLUMN IF NOT EXISTS requested_by TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS decided_by TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS exposure_cents BIGINT,
  ADD COLUMN IF NOT EXISTS reason_text TEXT NOT NULL DEFAULT '';
-- decision values: gateway uses pending/approved/denied
ALTER TABLE approvals DROP CONSTRAINT IF EXISTS approvals_decision_check;
ALTER TABLE approvals
  ADD CONSTRAINT approvals_decision_check
  CHECK (decision IN ('pending','approved','denied','granted','expired'));

-- receipts: explicit evidence columns (payload keeps full copy)
ALTER TABLE receipts
  ADD COLUMN IF NOT EXISTS agent_id UUID REFERENCES agents(agent_id),
  ADD COLUMN IF NOT EXISTS principal_id UUID REFERENCES principals(principal_id),
  ADD COLUMN IF NOT EXISTS tool TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS action_name TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS decision TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS args_hash TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS result_hash TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS financial_cents BIGINT,
  ADD COLUMN IF NOT EXISTS compensation TEXT NOT NULL DEFAULT 'none',
  ADD COLUMN IF NOT EXISTS started_at TIMESTAMPTZ,
  ADD COLUMN IF NOT EXISTS completed_at TIMESTAMPTZ NOT NULL DEFAULT now();

-- audit: structured actor columns
ALTER TABLE audit_events
  ADD COLUMN IF NOT EXISTS actor_type TEXT NOT NULL DEFAULT '',
  ADD COLUMN IF NOT EXISTS actor_id TEXT NOT NULL DEFAULT '';

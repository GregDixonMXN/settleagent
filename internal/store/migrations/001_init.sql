-- 001_init.sql — AgentGuard initial schema (Postgres)
-- Multi-tenant: every row carries org_id; enforcement is server-side.

CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE organizations (
  org_id      UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  name        TEXT NOT NULL,
  created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE principals (
  principal_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id       UUID NOT NULL REFERENCES organizations(org_id) ON DELETE CASCADE,
  kind         TEXT NOT NULL CHECK (kind IN ('human','service')),
  display_name TEXT NOT NULL,
  created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_principals_org ON principals(org_id);

CREATE TABLE agents (
  agent_id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id              UUID NOT NULL REFERENCES organizations(org_id) ON DELETE CASCADE,
  owner_principal_id  UUID NOT NULL REFERENCES principals(principal_id),
  agent_group         TEXT NOT NULL,
  display_name        TEXT NOT NULL,
  status              TEXT NOT NULL DEFAULT 'active',
  created_at          TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_agents_org ON agents(org_id);
CREATE INDEX idx_agents_group ON agents(org_id, agent_group);

CREATE TABLE agent_credentials (
  credential_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id        UUID NOT NULL REFERENCES organizations(org_id) ON DELETE CASCADE,
  agent_id      UUID NOT NULL REFERENCES agents(agent_id) ON DELETE CASCADE,
  secret_hash   TEXT NOT NULL, -- hashed secret only; never plaintext
  scopes        TEXT[] NOT NULL DEFAULT '{}',
  expires_at    TIMESTAMPTZ,
  revoked_at    TIMESTAMPTZ,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_agent_credentials_org_agent ON agent_credentials(org_id, agent_id);

CREATE TABLE transactions (
  txn_id          UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id          UUID NOT NULL REFERENCES organizations(org_id) ON DELETE CASCADE,
  agent_id        UUID NOT NULL REFERENCES agents(agent_id),
  state           TEXT NOT NULL DEFAULT 'CREATED'
                  CHECK (state IN ('CREATED','PLANNING','AWAITING_APPROVAL','EXECUTING','COMMITTED','ABORTING','COMPENSATING','ROLLED_BACK','PARTIALLY_COMPENSATED','FAILED')),
  idempotency_key TEXT NOT NULL,
  correlation_id  UUID,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (org_id, idempotency_key)
);
CREATE INDEX idx_transactions_org_state ON transactions(org_id, state);
CREATE INDEX idx_transactions_agent ON transactions(org_id, agent_id);

CREATE TABLE actions (
  action_id       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id          UUID NOT NULL REFERENCES organizations(org_id) ON DELETE CASCADE,
  txn_id          UUID NOT NULL REFERENCES transactions(txn_id) ON DELETE CASCADE,
  tool            TEXT NOT NULL,
  action          TEXT NOT NULL,
  args            JSONB NOT NULL DEFAULT '{}',
  classes         TEXT[] NOT NULL DEFAULT '{UNKNOWN}',
  policy_result   TEXT CHECK (policy_result IN ('ALLOW','DENY','REQUIRE_APPROVAL','ALLOW_WITH_CONSTRAINTS')),
  status          TEXT NOT NULL DEFAULT 'pending'
                  CHECK (status IN ('pending','approved','denied','executing','succeeded','failed','compensated')),
  compensation    JSONB,
  idempotency_key TEXT NOT NULL,
  created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (org_id, idempotency_key)
);
CREATE INDEX idx_actions_txn ON actions(txn_id);
CREATE INDEX idx_actions_org_tool ON actions(org_id, tool);

CREATE TABLE policies (
  policy_id  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id     UUID NOT NULL REFERENCES organizations(org_id) ON DELETE CASCADE,
  version    INT NOT NULL DEFAULT 1,
  name       TEXT NOT NULL,
  rules      JSONB NOT NULL DEFAULT '[]',
  is_active  BOOLEAN NOT NULL DEFAULT false,
  created_by UUID REFERENCES principals(principal_id),
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_policies_org_active ON policies(org_id, is_active);

CREATE TABLE approvals (
  approval_id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id                   UUID NOT NULL REFERENCES organizations(org_id) ON DELETE CASCADE,
  txn_id                   UUID NOT NULL REFERENCES transactions(txn_id) ON DELETE CASCADE,
  action_id                UUID REFERENCES actions(action_id) ON DELETE CASCADE,
  requested_by_agent_id    UUID NOT NULL REFERENCES agents(agent_id),
  decided_by_principal_id  UUID REFERENCES principals(principal_id),
  decision                 TEXT NOT NULL DEFAULT 'pending'
                           CHECK (decision IN ('pending','granted','denied','expired')),
  reason                   TEXT,
  expires_at               TIMESTAMPTZ,
  created_at               TIMESTAMPTZ NOT NULL DEFAULT now(),
  decided_at               TIMESTAMPTZ
);
CREATE INDEX idx_approvals_txn ON approvals(txn_id);
CREATE INDEX idx_approvals_org_decision ON approvals(org_id, decision);

CREATE TABLE receipts (
  receipt_id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id     UUID NOT NULL REFERENCES organizations(org_id) ON DELETE CASCADE,
  txn_id     UUID NOT NULL REFERENCES transactions(txn_id) ON DELETE CASCADE,
  action_id  UUID REFERENCES actions(action_id) ON DELETE SET NULL,
  payload    JSONB NOT NULL,
  prev_hash  TEXT NOT NULL, -- previous receipt hash; 'GENESIS' for chain head
  hash       TEXT NOT NULL, -- sha256(prev_hash + canonical payload)
  created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_receipts_txn ON receipts(txn_id);
CREATE INDEX idx_receipts_org ON receipts(org_id);

CREATE TABLE audit_events (
  event_id       UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id         UUID NOT NULL REFERENCES organizations(org_id) ON DELETE CASCADE,
  txn_id         UUID REFERENCES transactions(txn_id) ON DELETE SET NULL,
  actor          TEXT NOT NULL,
  kind           TEXT NOT NULL,
  detail         JSONB NOT NULL DEFAULT '{}',
  correlation_id UUID,
  created_at     TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX idx_audit_org_txn ON audit_events(org_id, txn_id);
CREATE INDEX idx_audit_correlation ON audit_events(correlation_id);

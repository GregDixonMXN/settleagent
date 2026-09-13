-- 004_mcp.sql — registered upstream MCP servers per org.
-- auth_token is the credential we present UPSTREAM. It is never returned
-- by any API or written to logs; rotation is re-register with a new token.
-- (Envelope encryption for stored tokens is a known gap; see THREAT_MODEL.)

CREATE TABLE IF NOT EXISTS mcp_servers (
  server_id  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  org_id     UUID NOT NULL REFERENCES organizations(org_id) ON DELETE CASCADE,
  name       TEXT NOT NULL,
  url        TEXT NOT NULL,
  auth_token TEXT NOT NULL DEFAULT '',
  tools      JSONB NOT NULL DEFAULT '[]',
  classes    JSONB NOT NULL DEFAULT '{}',
  created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (org_id, name)
);
CREATE INDEX IF NOT EXISTS idx_mcp_servers_org ON mcp_servers(org_id);

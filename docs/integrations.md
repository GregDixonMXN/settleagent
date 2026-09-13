# Integrations

Two kinds: native mock tools (built into the gateway registry) and the MCP
proxy for upstream MCP servers.

## Native mock tools

`internal/actions`: `crm.*`, `stripe.refund`, `email.send`, `fs.*`,
`http.fetch`, `github.create_issue`, `deploy.deploy`, plus always-deny
`shell.exec`, `postgres.delete_database`, `iam.grant`. Each carries action
classes (see README table). Mocks are labeled as mocks; real connectors
implement `ToolHandler` + optional `Compensator`.

## MCP proxy

The gateway acts as an MCP client to registered upstream servers and
intercepts every call through the normal engine path:

    Agent -> POST /v1/mcp/call -> policy evaluate -> approval? -> execute -> receipt

- `POST /v1/mcp/servers` (operator): `{name, url, auth_token?, classes?}`.
  Name must match `^[a-z0-9][a-z0-9-]{0,63}$`; URL must be http(s).
  Registration fetches live `tools/list` (proves reachability, caches the
  catalog) and registers each tool in the engine as
  `tool=mcp:<name>, action=<tool>`, so policy rules target MCP tools
  directly, e.g. `{tool: "mcp:docs-mcp", action: "delete_repo"} -> DENY`.
  The upstream token is stored, never returned or logged.
- `GET /v1/mcp/servers`, `GET /v1/mcp/tools[?server=]`: catalog views.
- `POST /v1/mcp/call`: `{transaction_id, agent_id, server, tool,
  arguments, idempotency_key}`. Rejects unlisted tools before policy.
  Returns the action record; execution uses the standard
  `POST /v1/actions/:id/execute`, approvals use the standard decide flow,
  receipts and audit are identical to native calls.
- Upstream failures (unreachable, malformed, JSON-RPC error, `isError`)
  fail the action — never recorded as success.
- MCP tools have no compensators by default: after an irreversible upstream
  side effect, compensation honestly reports partial rather than fake
  rollback.

## Limits (v0.1)

- Streamable HTTP transport only, no stdio servers, no notifications.
- Tool catalog is cached at registration; re-register to refresh.
- Upstream tokens sealed at rest since M10 (AES-256-GCM); plaintext legacy
  rows read through and reseal on next write.
- Scheme guard only for SSRF (http/https); no allowlist or DNS pinning yet.

---

# Real integrations (M10)

Native tools backed by live providers, per-org credentials sealed at rest
(`integration_credentials`, AES-256-GCM via keys.Provider; `AG_DATA_KEY`
explicit or derived from the signing key):

- `stripe.refund` / `stripe.read_customer` — test-mode only (`sk_test_`;
  live keys refused at registration and at call time). Refund passes the
  action idempotency key as Stripe's `Idempotency-Key`; reconciliation
  matches refunds by charge + amount.
- `github.read_repo/read_issue/create_issue/comment/open_pr/merge_pr` —
  personal access token per org; merges are PRIVILEGED.
- `postgres.query` — SELECT/WITH/VALUES/TABLE/EXPLAIN only, single
  statement, read-only transaction, 10s statement timeout, 100-row cap.
  Anything else is rejected with an explanation. Writes are future work.
- `http.request` — default-deny against an operator-managed domain
  allowlist (`POST /v1/integrations/http/domains`); non-public IPs refused
  (DNS-rebinding residual documented); GET/HEAD are reads, other methods
  carry an explicit unknown-side-effects warning.

Manage: `POST /v1/integrations/:name/credentials` (operator;
stripe|github|postgres), `GET /v1/integrations` (names only, never
secrets). Unconfigured integrations fail loudly at execution —
never silent mock fallback — except `AG_DEMO_MOCKS=1`, which keeps the
local compose demo on mocks with a loud boot log.

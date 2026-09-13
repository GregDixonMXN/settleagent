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
- Upstream tokens are stored reversibly (no envelope encryption yet).
- Scheme guard only for SSRF (http/https); no allowlist or DNS pinning yet.

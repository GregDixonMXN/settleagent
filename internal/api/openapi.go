package api

const openAPISpec = `{
  "openapi": "3.0.3",
  "info": {"title": "AgentGuard API", "version": "v1"},
  "paths": {
    "/v1/agents": {"post": {"summary": "Register agent"}},
    "/v1/transactions": {
      "post": {"summary": "Create transaction"},
      "get": {"summary": "List transactions"}
    },
    "/v1/transactions/{id}": {"get": {"summary": "Get transaction timeline"}},
    "/v1/actions": {"post": {"summary": "Propose action (policy evaluated pre-execution)"}},
    "/v1/actions/{id}/execute": {"post": {"summary": "Execute allowed/approved action"}},
    "/v1/actions/{id}/reconcile": {"post": {"summary": "Reconcile uncertain execution (operator)"}},
    "/v1/receipts/verify": {
      "get": {"summary": "Verify receipt chain + signatures for a transaction"},
      "post": {"summary": "Verify one receipt object"}
    },
    "/v1/mcp/servers": {
      "post": {"summary": "Register upstream MCP server (operator)"},
      "get": {"summary": "List registered MCP servers"}
    },
    "/v1/mcp/tools": {"get": {"summary": "Aggregated MCP tool catalog"}},
    "/v1/mcp/call": {"post": {"summary": "Proxied MCP call (policy evaluated pre-execution)"}},
    "/v1/approvals": {"get": {"summary": "List pending approvals"}},
    "/v1/approvals/{id}/decide": {"post": {"summary": "Approve or deny"}},
    "/v1/receipts": {"get": {"summary": "List receipts"}},
    "/v1/audit": {"get": {"summary": "Audit events"}},
    "/v1/policies": {"get": {"summary": "List policies"}}
  }
}`

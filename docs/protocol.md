# Protocol — REST API v1

Base path: `/v1`. All resources org-scoped (org derived server-side from credential, never from client input).

## Resources

| Resource | Endpoints |
|---|---|
| agents | `POST /v1/agents`, `GET /v1/agents`, `GET /v1/agents/{id}`, `PATCH /v1/agents/{id}`, `POST /v1/agents/{id}/credentials`, `DELETE /v1/agents/{id}/credentials/{credId}` |
| transactions | `POST /v1/transactions`, `GET /v1/transactions`, `GET /v1/transactions/{id}`, `POST /v1/transactions/{id}/cancel` |
| actions | `POST /v1/transactions/{id}/actions`, `GET /v1/transactions/{id}/actions`, `GET /v1/actions/{id}` |
| approvals | `GET /v1/approvals`, `GET /v1/approvals/{id}`, `POST /v1/approvals/{id}/grant`, `POST /v1/approvals/{id}/deny` |
| policies | `POST /v1/policies`, `GET /v1/policies`, `GET /v1/policies/{id}`, `POST /v1/policies/{id}/activate` |
| receipts | `GET /v1/transactions/{id}/receipts`, `GET /v1/receipts/{id}` |

## Headers

- `Idempotency-Key: <uuid>` — **required** on `POST /v1/transactions` and `POST .../actions`. Retries with the same key return the original record; they never create a duplicate.
- `X-Correlation-Id: <uuid>` — optional on any request; generated server-side if absent. Returned on every response and stamped on all resulting `audit_events`.

## Error shape

```json
{
  "code": "APPROVAL_REQUIRED",
  "message": "Transaction needs approval before it can execute.",
  "why": "Policy 'prod-guard' rule 3 matched deploy to production, which requires human approval.",
  "correlation_id": "9f3a…",
  "details": {}
}
```

- `code`: stable machine code (`DENIED`, `APPROVAL_REQUIRED`, `APPROVAL_EXPIRED`, `INVALID_STATE`, `NOT_FOUND`, `CONFLICT`, …).
- `message`: short summary.
- `why`: **human-readable explanation** — which policy/rule/approval caused this, in plain language. Always present on `DENY` and approval errors.
- `correlation_id`: echoes the request's correlation ID for tracing.

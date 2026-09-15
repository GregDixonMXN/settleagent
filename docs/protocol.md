# Protocol — REST API v1

Base path: `/v1`. All resources org-scoped (org derived server-side from credential, never from client input).

> v0.1 implements the subset listed in `GET /openapi.json`. The wider
> resource table below is the target surface; endpoint shapes there are
> aspirational until marked implemented.

## Authentication (implemented)

- `Authorization: Bearer <secret>` required on all `/v1/*` (`/health` and
  `/openapi.json` stay open).
- Agent secret `st_<keyid>_<random>`: bound to one agent; `agent_id` in
  request bodies must equal the credential's agent (operators exempt).
- Operator secret `sto_<keyid>_<random>`: human/dashboard access; required
  for `POST /v1/agents` (register), `POST /v1/approvals/:id/decide`,
  grant management, and credential rotation/revocation.
- Authorization order per action: authenticate → resolve principal →
  authority grants (step 4) → policy → decision. Agents with no grants run
  policy-only (audit-logged); once grants exist, uncovered operations deny
  with reason code `AUTHORITY_EXCEEDED` before policy runs.
- Decisions carry machine-readable `reason_code` (`ALLOWED`,
  `AUTHORITY_EXCEEDED`, `POLICY_DENIED`, `APPROVAL_REQUIRED`) alongside the
  human `why`.
- Pre-key-ID secrets (`st_<hex>`) still work via bounded per-org scan when
  `X-Org-ID` is supplied; new secrets ignore that header entirely.
- Failures are 401 (`missing_credentials`, `invalid_credentials`) or 403
  (`operator_required`, `agent_mismatch`), each with a human-readable `why`.
- Per-credential fixed-window rate limiting (120 req/min default); 429
  `rate_limited` when exceeded. Single-process buckets in v0.1.

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

# Observability

Tracing is OpenTelemetry from the start; metrics/logging stay JSON structured.

## Spans

Every request opens a server span (`METHOD path`) carrying
`http.request_id`. The gateway adds children:

- `policy.evaluate` — attrs: tool, action, agent, effect
- `action.execute` — attrs: tool, action, txn; records tool errors
- `transaction.compensate` — attrs: txn

`action.executing`, `action.executed`, and `action.failed` audit events
carry `trace_id` in their payload, joining the audit timeline to traces.

## Running locally

Trace and span IDs are always generated (local SDK); spans export OTLP/HTTP
only when `OTEL_EXPORTER_OTLP_ENDPOINT` is set (e.g. `http://jaeger:4318`).
`docker compose up` includes Jaeger all-in-one: traces at
http://localhost:16686, request/trace IDs on every response
(`X-Request-ID`, `X-Trace-ID`). The Python SDK surfaces them as
`guard.last_request_id` / `guard.last_trace_id`.

## Limits (v0.1)

- No metrics endpoint yet; no log-trace correlation beyond IDs above.
- Single-process rate-limiter buckets (see protocol.md) are not exported.
- DB query spans are not instrumented; add when slow-query debugging needs it.

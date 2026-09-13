# ADR-005: No Message Queue

Decision: No Kafka or job queue; approval waits and step execution use Postgres
state + API polling.

Why: MVP volume doesn't justify queue ops burden; extract an async worker behind
the existing `actions` interface when step volume requires it.

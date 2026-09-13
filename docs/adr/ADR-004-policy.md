# ADR-004: Deterministic Policy Engine

Decision: Policy evaluation is pure functions over (agent, action, args, org limits);
first matching rule in priority order wins; no LLM in the decision path.

Why: same input must always give the same ALLOW/DENY/REQUIRE_APPROVAL/
ALLOW_WITH_CONSTRAINTS outcome so decisions are auditable and testable.

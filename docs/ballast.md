# Ballast × AgentGuard — governed execution

Ballast runs tasks (one command in an isolated worktree). AgentGuard
governs what those commands may do. The pairing, proven by joint test:

    Ballast task command → AgentGuard-guarded agent → policy/approval →
    execution → receipts + audit → Ballast changeset

## Pattern

The task command carries its own AgentGuard env (Ballast scrubs the
environment for tasks by design):

    API_URL=https://guard.internal ORG=<org> PRINCIPAL=<prin>
    OPERATOR_TOKEN=<operator-token>
    python3 agent.py

`agent.py` registers (operator token) or reuses an agent credential,
creates a transaction per unit of work, and executes tool calls through
`/v1/actions` (or `/v1/mcp/call`). The changeset then contains both the
work output AND the transaction ID; reviewers check the AgentGuard
timeline before integrating.

## Rules that fell out of practice

- One transaction per task: the changeset maps 1:1 to governed work.
- Demanding tools (refunds, deploys, deletes) require approval BEFORE the
  runner's timeout expires, or the task fails closed (BLOCKED, no changeset).
- A failing command means BLOCKED and no changeset — the audit trail still
  shows what was attempted and denied.
- Run AgentGuard on Postgres (not memory) for any pairing that must
  survive restarts; memory is demo-only.

## Production shape

Persistent compose stack (Postgres + API), Ballast pointed at the API URL,
operator token in the task command or a_task-local env file the runner
reads. Token rotation: revoke + reissue via
`DELETE /v1/credentials/:keyID` without touching Ballast config beyond
the command string.

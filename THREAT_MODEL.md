# AgentGuard Threat Model

Scope: the gateway path — agent → `apps/api` → policy → approval → adapter/MCP
proxy → receipt + audit. Trust boundary: everything outside `apps/api` +
Postgres is untrusted (agents, tools, MCP servers, network, dashboard clients).

## 1. Stolen credential (agent API key)

Attack: attacker copies an agent's API key, calls the gateway directly.
Mitigation: keys are random 256-bit, bcrypt-hashed at rest, per-agent with
least-privilege scope; revocation endpoint; rate limiting per key; anomalous
velocity rules can force `require_approval`.
MVP: hashed keys, per-agent scope, revocation, rate limit. Defers: rotation
schedule automation, anomaly detection, mTLS for agents.

## 2. Malicious / compromised agent

Attack: legitimate agent goes rogue, requests harmful or out-of-scope actions.
Mitigation: deny-by-default policies, per-agent allow-lists, amount/velocity
caps, approvals for sensitive actions; every decision logged with reason.
MVP: policy engine (M1) + approvals (M2) + audit. Defers: behavior baselining,
agent reputation, automatic suspension.

## 3. Prompt injection → unauthorized action

Attack: tool output or page content tricks the agent into requesting a
privileged action (e.g. "refund all customers").
Mitigation: policy binds to authenticated `agent_id`, never to LLM-claimed
intent; sensitive actions always require human approval regardless of how
confident the agent sounds; tool responses never auto-execute.
MVP: `require_approval` on sensitive verbs; tool output quarantined from action
parameters (M4). Defers: prompt-injection classifiers, dual-LLM review.

## 4. Cross-tenant access

Attack: agent of tenant A reads/approves/executes tenant B's actions.
Mitigation: `tenant_id` derived server-side from the API key, never from request
body; every store query scoped by tenant; approval tokens bound to tenant.
MVP: server-side tenant binding + tests at store and API layers (M5).
Defers: per-tenant encryption keys, tenant-level rate quotas.

## 5. Replay of requests

Attack: attacker captures a valid `execute` call and replays it.
Mitigation: idempotency keys + short-lived approval tokens (single-use, TTL);
replayed `execute` with the same key returns the original receipt without
re-executing; replay without a valid approval token is denied.
MVP: idempotency enforcement + single-use approvals (M2–M3).
Defers: request signing with nonces/timestamps.

## 6. Double-spend / duplicate refund

Attack: two concurrent refund requests for the same charge both succeed.
Mitigation: idempotency key unique constraint + ledger transaction that debits
at most once per key; concurrent duplicates serialize on the constraint, losers
read the winner's receipt.
MVP: DB-backed exactly-once ledger + parallel-duplicate test (M3).
Defers: distributed locking across gateway replicas.

## 7. Policy bypass

Attack: unknown action name, case tricks, or extra parameters dodge the rules.
Mitigation: deny-by-default; policy matches on normalized `(verb, resource
type)` against a closed action registry (`internal/actions`) — unregistered
action cannot execute; parameters validated against schema before evaluation.
MVP: closed registry + normalization + default-deny test (M1).
Defers: formal policy verification, Rego/Cedar backend.

## 8. Privilege escalation

Attack: agent self-approves, widens its own scope, or approves another agent.
Mitigation: approval requires a dashboard role distinct from agent keys;
approval tokens bound to `(action_id, tenant_id)`, single-use; agents have no
policy-write endpoint in MVP.
MVP: role separation (agent key ≠ approver), token binding (M2).
Defers: RBAC granularity, quorum approvals, break-glass flow.

## 9. Tampered audit log

Attack: attacker (or rogue admin) edits history to hide an action.
Mitigation: hash-chained append-only table (each row commits to the previous
hash); startup and on-demand verification; append-only DB role for the API
(no UPDATE/DELETE grants); verification failure blocks boot with an alert.
MVP: hash chain + verification + least-privilege DB role (M5).
Defers: KMS-signed checkpoints, external WORM/SIEM mirroring.

## 10. Malicious tool / MCP response

Attack: MCP server returns oversized payload, SSRF URL, or embedded
instructions to exfiltrate data.
Mitigation: MCP proxy stub — tool allow-list, response size caps, timeouts,
schema validation; tool output treated as data (never executed); SSRF guard
(blocks metadata IPs / internal ranges) on adapter fetches.
MVP: allow-list + caps + validation + SSRF guard (M4).
Defers: full MCP spec coverage, content sandboxing, DLP scanning.

## 11. Secret exfiltration

Attack: agent or tool tricks the gateway into leaking adapter credentials or
other tenants' keys.
Mitigation: secrets live in env/secret-manager, injected server-side into
adapters, never echoed into logs, receipts, errors, or agent-visible fields;
structured logging with redaction of `authorization`, `api_key`, `secret`;
error messages to agents are generic (detail in server logs only).
MVP: env-only secrets + log redaction + generic agent-facing errors (M4–M5).
Defers: per-tenant vault, secret rotation automation, egress proxy.

## Residual risks (accepted for MVP)

- No protection against a compromised gateway host — relies on standard
  hardening (M5 container posture) rather than TEEs/attestation.
- Dashboard auth is a stub role — production needs real OIDC + MFA.
- Single gateway replica — no Byzantine/failover story yet (see M6).

# SettleAgent Python SDK

Stdlib only — no dependencies.

    from settleagent import SettleAgent

    guard = SettleAgent("http://localhost:8080", org_id="ORG", agent_id="AG")
    txn = guard.create_transaction(principal_id="P", session_id="s1",
                                   objective="resolve_ticket_9182")

    # Propose + execute when policy allows:
    act = guard.execute(txn["id"], tool="stripe", action="refund",
                        arguments={"amount_cents": 8000},
                        idempotency_key="refund-t9182-80")

    # The returned record may be `allowed`/`executed`, `awaiting_approval`
    # (then approve via guard.decide and run guard.execute_action), or
    # `denied` (never executes — the `why` is in decision.explanation).

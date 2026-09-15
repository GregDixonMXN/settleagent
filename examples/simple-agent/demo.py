"""MVP success demo: support agent resolves ticket_9182.

Covers: CRM lookup + update (ALLOW), $80 refund (ALLOW), $300 refund
(REQUIRE_APPROVAL -> approve -> execute), $2000 refund (DENY, never
executes). Prints the audit timeline at the end.

Usage: API_URL=http://127.0.0.1:8080 ORG=<org> PRINCIPAL=<principal> python3 demo.py
"""
import os
import sys

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "../../packages/sdk-python"))
from settleagent import SettleAgent

API = os.environ.get("API_URL", "http://127.0.0.1:8080")
ORG = os.environ["ORG"]
PRINCIPAL = os.environ["PRINCIPAL"]
OPERATOR = os.environ.get("OPERATOR_TOKEN", "")

op = SettleAgent(API, org_id=ORG, token=OPERATOR or None)
g = SettleAgent(API, org_id=ORG)
reg = op.register_agent(PRINCIPAL, "support-agent-14", groups=["support"])
agent_id = reg["agent"]["id"]
agent_secret = reg["api_secret"]
print("agent:", agent_id, "(secret hidden)")
g.token = agent_secret
g.agent_id = agent_id

txn = g.create_transaction(PRINCIPAL, "sess_9182", "resolve_ticket_9182")
tx = txn["id"]
print("transaction:", tx)


def step(tool, action, args, key):
    a = g.execute(tx, tool, action, args, idempotency_key=key)
    dec = (a.get("decision") or {}).get("effect")
    print(f"{tool}.{action} {args} -> {a['status']} (policy {dec})")
    if dec and dec != "ALLOW" and "explanation" in (a.get("decision") or {}):
        print(f"   why: {a['decision']['explanation']}")
    return a


step("crm", "lookup_customer", {"customer": "cust_9182"}, "demo-lookup-1")
step("crm", "update_record", {"ticket": 9182}, "demo-crm-1")
step("stripe", "refund", {"amount_cents": 8000}, "demo-ref80-1")
step("email", "send", {"to": "cust@x.com"}, "demo-email-1")

big = step("stripe", "refund", {"amount_cents": 30000}, "demo-ref300-1")
if big["status"] == "awaiting_approval":
    pend = g.pending_approvals()
    mine = [p for p in pend if p.get("action_id") == big["id"]]
    print(f"approval pending ({len(pend)} total); human approves...")
    ap = op.decide(mine[0]["id"], True, decided_by="manager")
    print("approval:", ap["status"])
    done = g.execute_action(big["id"])
    print("after approval:", done["status"])

huge = step("stripe", "refund", {"amount_cents": 200000}, "demo-ref2000-1")
assert huge["status"] == "denied", "expected DENY"

full = g.get_transaction(tx)
print(f"\ntimeline: {len(full['audit'])} events, {len(full['receipts'])} receipts")
print(f"trace of last call: {g.last_trace_id} (request {g.last_request_id})")
for e in full["audit"][:8]:
    print(" -", e["type"])
print("DEMO OK")

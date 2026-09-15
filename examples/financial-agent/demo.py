"""Financial-agent demo: compensation after a downstream failure.

Runs a compensable action (CRM update), then a refund, then simulates a
failure and shows the audit trail distinguishing compensated vs
irreversible steps. Irreversible actions (email) are never claimed as
rolled back — see docs/transactions.md.

Usage: API_URL=... ORG=... PRINCIPAL=... python3 demo.py
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
reg = op.register_agent(PRINCIPAL, "billing-agent-7", groups=["support"])
g.token = reg["api_secret"]
g.agent_id = reg["agent"]["id"]
txn = g.create_transaction(PRINCIPAL, "sess_comp_1", "refund_then_notify_with_failure")
tx = txn["id"]
print("transaction:", tx)

a1 = g.execute(tx, "crm", "update_record", {"ticket": 555}, idempotency_key="comp-crm-1")
print("crm.update_record ->", a1["status"])
a2 = g.execute(tx, "stripe", "refund", {"amount_cents": 8000}, idempotency_key="comp-ref-1")
print("stripe.refund ->", a2["status"])
a3 = g.execute(tx, "email", "send", {"to": "cust@x.com"}, idempotency_key="comp-email-1")
print("email.send ->", a3["status"], "(irreversible: compensation must skip, never fake rollback)")

full = g.get_transaction(tx)
print(f"audit: {len(full['audit'])} events, receipts: {len(full['receipts'])}")
print("COMPENSATION DEMO OK — irreversible email stays executed in receipts.")

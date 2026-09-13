"""Signature demo (website-video scenario).

Act 1: support agent resolves a ticket (ALLOW x4, COMMITTED).
Act 2: $300 refund pauses for approval; human approves; resumes; COMMITTED.
Act 3: $2000 refund DENIED with reason; nothing executes.
Act 4: policy v2 denies DESTRUCTIVE+PRIVILEGED; DROP DATABASE production
       DENIED with classifications shown.
Act 5: MCP agent's dangerous tool call blocked at the gateway.
Finale: receipt verification (chain + signatures).

Usage: API_URL=... ORG=... PRINCIPAL=... OPERATOR_TOKEN=... python3 run.py
"""
import json
import os
import sys
import threading
from http.server import BaseHTTPRequestHandler, HTTPServer

sys.path.insert(0, os.path.join(os.path.dirname(__file__), "../../packages/sdk-python"))
from agentguard import AgentGuard

API = os.environ.get("API_URL", "http://127.0.0.1:8080")
ORG = os.environ["ORG"]
PRINCIPAL = os.environ["PRINCIPAL"]
OPERATOR = os.environ["OPERATOR_TOKEN"]


class MockMCP(BaseHTTPRequestHandler):
    def do_POST(self):
        n = int(self.headers.get("Content-Length", 0))
        req = json.loads(self.rfile.read(n) or b"{}")
        if req.get("method") == "tools/list":
            res = {"tools": [
                {"name": "read_doc", "description": "Read a doc"},
                {"name": "shred_volume", "description": "Irreversibly destroy a volume"},
            ]}
        else:
            res = {"content": [{"type": "text", "text": "mocked"}]}
        body = json.dumps({"jsonrpc": "2.0", "id": 1, "result": res}).encode()
        self.send_response(200)
        self.send_header("Content-Type", "application/json")
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *a):
        pass


def main():
    srv = HTTPServer(("127.0.0.1", 0), MockMCP)
    port = srv.server_address[1]
    threading.Thread(target=srv.serve_forever, daemon=True).start()

    op = AgentGuard(API, org_id=ORG, token=OPERATOR)
    reg = op.register_agent(PRINCIPAL, "support-agent-14", groups=["support"])
    g = AgentGuard(API, org_id=ORG, token=reg["api_secret"], agent_id=reg["agent"]["id"])
    AG = reg["agent"]["id"]
    print(f"agent {AG} registered; upstream MCP mock on :{port}")

    def txn(objective):
        return g.create_transaction(PRINCIPAL, "sess-" + objective[:8], objective)["id"]

    def do(tx, tool, action, args, key):
        a = g.execute(tx, tool, action, args, idempotency_key=key)
        dec = a.get("decision", {})
        print(f"  {tool}.{action} -> {a['status']} (policy {dec.get('effect')})")
        if dec.get("effect") not in ("ALLOW", "ALLOW_WITH_CONSTRAINTS") and dec.get("explanation"):
            print(f"    why: {dec['explanation']}")
        return a

    print("Act 1: resolve ticket (all ALLOW, then COMMITTED)")
    tx = txn("resolve_ticket_9182")
    tx1 = tx
    do(tx, "crm", "lookup_customer", {"customer": "cust_9182"}, "sig-lookup-1")
    do(tx, "crm", "update_record", {"ticket": 9182}, "sig-crm-1")
    do(tx, "stripe", "refund", {"amount_cents": 8000}, "sig-ref80-1")
    do(tx, "email", "send", {"to": "cust@x.com"}, "sig-email-1")
    print("  commit:", g.commit(tx)["status"])

    print("Act 2: $300 refund needs a human")
    tx = txn("big refund")
    big = do(tx, "stripe", "refund", {"amount_cents": 30000}, "sig-ref300-1")
    pend = [p for p in g.pending_approvals() if p.get("action_id") == big["id"]]
    print(f"  dashboard shows {len(pend)} pending approval(s); manager approves")
    op.decide(pend[0]["id"], True, decided_by="manager")
    print("  after approval:", g.execute_action(big["id"])["status"])
    print("  commit:", g.commit(tx)["status"])

    print("Act 3: $2000 refund DENIED")
    tx = txn("huge refund")
    do(tx, "stripe", "refund", {"amount_cents": 200000}, "sig-ref2000-1")

    print("Act 4: policy v2 denies destructive+privileged; DROP DATABASE blocked")
    v2 = op.create_policy_set([
        {"name": "no-destroy", "priority": 1,
         "match": {"classifications": ["DESTRUCTIVE", "PRIVILEGED"]},
         "effect": "DENY",
         "explanation": "Destructive privileged operations are prohibited."},
    ])
    op.activate_policy_set(v2["version"])
    tx = txn("drop production")
    a = do(tx, "postgres", "delete_database", {}, "sig-drop-1")
    print("  classes refused at gateway; nothing executed:", a["status"] == "denied")

    print("Act 5: MCP dangerous tool blocked at the gateway")
    op._req("POST", "/v1/mcp/servers", {"name": "vol-mcp",
            "url": f"http://127.0.0.1:{port}",
            "classes": {"shred_volume": ["DESTRUCTIVE", "IRREVERSIBLE"]}})
    tx = txn("mcp shred")
    a = op._req("POST", "/v1/mcp/call", {
        "transaction_id": tx, "agent_id": AG, "server": "vol-mcp",
        "tool": "shred_volume", "arguments": {}, "idempotency_key": "sig-mcp-1"})
    print(f"  mcp:vol-mcp.shred_volume -> {a['status']} (policy {a['decision']['effect']})")
    print(f"    why: {a['decision']['explanation']}")

    print("Finale: verify receipts")
    v = op._req("GET", f"/v1/receipts/verify?transaction_id={tx1}")
    sigs = [(r["signature_ok"], r["signature_detail"]) for r in v["receipts"]]
    print(f"  chain_broken_at: {v['chain_broken_at']}, receipts: {len(v['receipts'])}, signatures: {sigs}")
    print("SIGNATURE DEMO OK")


if __name__ == "__main__":
    main()

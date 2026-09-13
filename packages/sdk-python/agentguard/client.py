"""AgentGuard Python SDK. Stdlib only (urllib) — no dependencies."""
import json
import urllib.request
import urllib.error
import uuid


class AgentGuardError(Exception):
    def __init__(self, status, payload):
        super().__init__(f"agentguard {status}: {payload}")
        self.status = status
        self.payload = payload


class AgentGuard:
    """Minimal client for the AgentGuard v1 API.

    guard = AgentGuard("http://localhost:8080", org_id="ORG", agent_id="AG")
    txn = guard.create_transaction(principal_id="P", session_id="s", objective="o")
    act = guard.execute(tool="stripe", action="refund",
                        arguments={"amount_cents": 8000},
                        transaction_id=txn["id"])
    """

    def __init__(self, base_url, org_id, agent_id=None, timeout=15):
        self.base_url = base_url.rstrip("/")
        self.org_id = org_id
        self.agent_id = agent_id
        self.timeout = timeout

    def _req(self, method, path, body=None):
        data = json.dumps(body).encode() if body is not None else None
        req = urllib.request.Request(
            self.base_url + path, method=method, data=data,
            headers={"Content-Type": "application/json", "X-Org-ID": self.org_id},
        )
        try:
            with urllib.request.urlopen(req, timeout=self.timeout) as r:
                return json.loads(r.read().decode() or "null")
        except urllib.error.HTTPError as e:
            raise AgentGuardError(e.code, e.read().decode()[:2000])

    def register_agent(self, principal_id, name, environment="production", groups=None):
        return self._req("POST", "/v1/agents", {
            "principal_id": principal_id, "name": name,
            "environment": environment, "groups": groups or [],
        })

    def create_transaction(self, principal_id, session_id, objective, agent_id=None):
        return self._req("POST", "/v1/transactions", {
            "agent_id": agent_id or self.agent_id, "principal_id": principal_id,
            "session_id": session_id, "objective": objective,
        })

    def get_transaction(self, txn_id):
        return self._req("GET", f"/v1/transactions/{txn_id}")

    def propose(self, transaction_id, tool, action, arguments,
                idempotency_key=None, agent_id=None):
        return self._req("POST", "/v1/actions", {
            "transaction_id": transaction_id, "agent_id": agent_id or self.agent_id,
            "tool": tool, "action": action, "arguments": arguments,
            "idempotency_key": idempotency_key or f"py-{uuid.uuid4()}",
        })

    def execute_action(self, action_id):
        return self._req("POST", f"/v1/actions/{action_id}/execute")

    def execute(self, transaction_id, tool, action, arguments,
                idempotency_key=None, agent_id=None):
        """Propose + run if allowed. Returns the action record (which may be
        awaiting_approval or denied — the caller decides what to do next)."""
        a = self.propose(transaction_id, tool, action, arguments,
                         idempotency_key, agent_id)
        if a.get("status") in ("allowed", "approved"):
            return self.execute_action(a["id"])
        return a

    def pending_approvals(self):
        return self._req("GET", "/v1/approvals")

    def decide(self, approval_id, approve, decided_by="human"):
        return self._req("POST", f"/v1/approvals/{approval_id}/decide",
                         {"approve": approve, "decided_by": decided_by})

    def receipts(self, transaction_id):
        return self._req("GET", f"/v1/receipts?transaction_id={transaction_id}")

    def audit(self, transaction_id="", limit=200):
        q = f"?transaction_id={transaction_id}" if transaction_id else ""
        return self._req("GET", f"/v1/audit{q}")

    def policies(self):
        return self._req("GET", "/v1/policies")

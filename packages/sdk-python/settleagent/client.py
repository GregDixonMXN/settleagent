"""SettleAgent Python SDK. Stdlib only (urllib) — no dependencies."""
import json
import urllib.request
import urllib.error
import uuid


class SettleAgentError(Exception):
    def __init__(self, status, payload):
        super().__init__(f"settleagent {status}: {payload}")
        self.status = status
        self.payload = payload


class SettleAgent:
    """Minimal client for the SettleAgent v1 API.

    guard = SettleAgent("http://localhost:8080", org_id="ORG", agent_id="AG")
    txn = guard.create_transaction(principal_id="P", session_id="s", objective="o")
    act = guard.execute(tool="stripe", action="refund",
                        arguments={"amount_cents": 8000},
                        transaction_id=txn["id"])
    """

    def __init__(self, base_url, org_id, agent_id=None, timeout=15, token=None):
        self.base_url = base_url.rstrip("/")
        self.org_id = org_id
        self.agent_id = agent_id
        self.timeout = timeout
        self.token = token
        self.last_request_id = None
        self.last_trace_id = None

    def _req(self, method, path, body=None):
        data = json.dumps(body).encode() if body is not None else None
        headers = {"Content-Type": "application/json", "X-Org-ID": self.org_id}
        if self.token:
            headers["Authorization"] = f"Bearer {self.token}"
        req = urllib.request.Request(
            self.base_url + path, method=method, data=data, headers=headers
        )
        try:
            with urllib.request.urlopen(req, timeout=self.timeout) as r:
                self.last_request_id = r.headers.get("X-Request-ID")
                self.last_trace_id = r.headers.get("X-Trace-ID")
                return json.loads(r.read().decode() or "null")
        except urllib.error.HTTPError as e:
            raise SettleAgentError(e.code, e.read().decode()[:2000])

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

    def create_grant(self, agent_id, scope, constraints=None, environment="",
                     expires_at=None):
        return self._req("POST", "/v1/grants", {
            "agent_id": agent_id, "scope": scope,
            "constraints": constraints or {}, "environment": environment,
            "expires_at": expires_at,
        })

    def grants(self, agent_id):
        return self._req("GET", f"/v1/grants?agent_id={agent_id}")

    def revoke_grant(self, grant_id):
        return self._req("POST", f"/v1/grants/{grant_id}/revoke")

    def rotate_credential(self, agent_id):
        return self._req("POST", f"/v1/agents/{agent_id}/credentials")

    def revoke_credential(self, key_id):
        return self._req("DELETE", f"/v1/credentials/{key_id}")

    def reconcile(self, action_id):
        return self._req("POST", f"/v1/actions/{action_id}/reconcile")

    def verify_receipts(self, transaction_id):
        return self._req("GET", f"/v1/receipts/verify?transaction_id={transaction_id}")

    def verify_receipt(self, receipt):
        return self._req("POST", "/v1/receipts/verify", receipt)

    def simulate(self, agent_id, tool, action, arguments=None):
        return self._req("POST", "/v1/policies/evaluate", {
            "agent_id": agent_id, "tool": tool, "action": action,
            "arguments": arguments or {},
        })

    def create_policy_set(self, rules):
        return self._req("POST", "/v1/policies/sets", {"rules": rules})

    def policy_sets(self):
        return self._req("GET", "/v1/policies/sets")

    def activate_policy_set(self, version):
        return self._req("POST", f"/v1/policies/sets/{version}/activate")

    def set_integration_credential(self, name, secret):
        return self._req("POST", f"/v1/integrations/{name}/credentials", {"secret": secret})

    def integrations(self):
        return self._req("GET", "/v1/integrations")

    def set_http_domains(self, domains, methods=None):
        return self._req("POST", "/v1/integrations/http/domains",
                         {"domains": domains, "methods": methods or ["GET", "HEAD"]})

    def commit(self, transaction_id):
        return self._req("POST", f"/v1/transactions/{transaction_id}/commit")

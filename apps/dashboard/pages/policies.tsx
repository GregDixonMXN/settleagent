import { useEffect, useState } from "react";
import { api, PolicySet } from "../lib/api";

const RULE_TEMPLATE = JSON.stringify(
  [
    {
      name: "example-deny",
      priority: 1,
      match: { tool: "iam" },
      effect: "DENY",
      explanation: "Agents cannot modify IAM permissions.",
    },
  ],
  null,
  2
);

export default function Policies() {
  const [sets, setSets] = useState<PolicySet[]>([]);
  const [error, setError] = useState("");
  const [draft, setDraft] = useState(RULE_TEMPLATE);
  const [agentId, setAgentId] = useState("");
  const [tool, setTool] = useState("stripe");
  const [action, setAction] = useState("refund");
  const [args, setArgs] = useState('{"amount_cents": 30000}');
  const [sim, setSim] = useState("");

  async function refresh() {
    try {
      setSets((await api.listPolicySets()) || []);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }

  useEffect(() => {
    refresh();
  }, []);

  async function create() {
    setError("");
    try {
      const rules = JSON.parse(draft);
      await api.createPolicySet(rules);
      setDraft("[]");
      await refresh();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }

  async function activate(version: number) {
    setError("");
    try {
      await api.activatePolicySet(version);
      await refresh();
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }

  async function simulate() {
    setError("");
    try {
      const out = await api.simulate({
        agent_id: agentId,
        tool,
        action,
        arguments: args ? JSON.parse(args) : {},
      });
      setSim(JSON.stringify(out, null, 2));
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }

  return (
    <div>
      <h1>Policies</h1>
      <p className="sub">
        Versioned rule sets — one active. Drafts never affect traffic until
        activated. History is immutable; receipts stamp the version.
      </p>
      {error && <div className="err">{error}</div>}
      <table className="table">
        <thead>
          <tr>
            <th>Version</th>
            <th>Status</th>
            <th>Rules</th>
            <th>By</th>
            <th></th>
          </tr>
        </thead>
        <tbody>
          {sets.map((s) => (
            <tr key={s.id}>
              <td>v{s.version}</td>
              <td>
                <span className="pill">{s.status}</span>
              </td>
              <td className="muted">{s.rules.length}</td>
              <td className="muted">{s.created_by || "—"}</td>
              <td>
                {s.status === "draft" && (
                  <button className="btn" onClick={() => activate(s.version)}>
                    Activate
                  </button>
                )}
              </td>
            </tr>
          ))}
        </tbody>
      </table>

      <h2>Draft new version (raw JSON)</h2>
      <textarea
        className="input"
        style={{ minHeight: 140, fontFamily: "monospace" }}
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
      />
      <button className="btn" onClick={create} style={{ marginTop: 8 }}>
        Create draft
      </button>

      <h2>Test mode (no side effects)</h2>
      <div style={{ display: "flex", gap: 8, flexWrap: "wrap", maxWidth: 720 }}>
        <input className="input" placeholder="Agent ID" value={agentId} onChange={(e) => setAgentId(e.target.value)} />
        <input className="input" placeholder="Tool" value={tool} onChange={(e) => setTool(e.target.value)} />
        <input className="input" placeholder="Action" value={action} onChange={(e) => setAction(e.target.value)} />
        <input className="input" placeholder="Arguments JSON" value={args} onChange={(e) => setArgs(e.target.value)} />
        <button className="btn" onClick={simulate}>
          Evaluate
        </button>
      </div>
      {sim && (
        <pre style={{ background: "#111", padding: 12, overflow: "auto" }}>{sim}</pre>
      )}
    </div>
  );
}

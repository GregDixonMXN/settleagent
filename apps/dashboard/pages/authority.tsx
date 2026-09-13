import { useState } from "react";
import { api, Grant } from "../lib/api";

export default function Authority() {
  const [agentId, setAgentId] = useState("");
  const [grants, setGrants] = useState<Grant[]>([]);
  const [error, setError] = useState("");

  async function load() {
    setError("");
    try {
      setGrants((await api.listGrants(agentId)) || []);
    } catch (e) {
      setError(e instanceof Error ? e.message : String(e));
    }
  }

  return (
    <div>
      <h1>Authority</h1>
      <p className="sub">
        Delegated authority grants: what each agent may attempt. Policy still
        decides the outcome. Agents with no grants run policy-only.
      </p>
      {error && <div className="err">{error}</div>}
      <div style={{ display: "flex", gap: 8, maxWidth: 560, marginBottom: 12 }}>
        <input
          className="input"
          placeholder="Agent ID"
          value={agentId}
          onChange={(e) => setAgentId(e.target.value)}
        />
        <button className="btn" onClick={load}>
          Load grants
        </button>
      </div>
      <table className="table">
        <thead>
          <tr>
            <th>Scope</th>
            <th>Max amount</th>
            <th>Environment</th>
            <th>Status</th>
            <th>Issued</th>
          </tr>
        </thead>
        <tbody>
          {grants.map((g) => (
            <tr key={g.id}>
              <td>{g.scope.join(", ")}</td>
              <td className="muted">
                {g.constraints?.max_amount_cents != null
                  ? `$${(g.constraints.max_amount_cents / 100).toFixed(2)}`
                  : "—"}
              </td>
              <td className="muted">{g.environment || "any"}</td>
              <td>
                <span className="pill">
                  {g.revoked_at ? "revoked" : g.bootstrap ? "bootstrap" : "active"}
                </span>
              </td>
              <td className="muted">{g.issued_at || "—"}</td>
            </tr>
          ))}
          {grants.length === 0 && (
            <tr>
              <td colSpan={5} className="muted">
                No grants — agent runs policy-only.
              </td>
            </tr>
          )}
        </tbody>
      </table>
    </div>
  );
}

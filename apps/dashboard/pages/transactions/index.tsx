import { useEffect, useState } from "react";
import Link from "next/link";
import { api, Transaction } from "../../lib/api";

export default function Transactions() {
  const [txns, setTxns] = useState<Transaction[]>([]);
  const [error, setError] = useState("");
  const [filter, setFilter] = useState("");

  useEffect(() => {
    api.listTxns().then((t) => setTxns(t || [])).catch((e) => setError(e.message));
  }, []);

  const rows = txns.filter(
    (t) =>
      !filter ||
      t.id.includes(filter) ||
      t.status.toLowerCase().includes(filter.toLowerCase()) ||
      (t.agent_id || "").includes(filter)
  );

  return (
    <div>
      <h1>Transactions</h1>
      <p className="sub">{txns.length} total. Click an ID for the timeline view.</p>
      {error && <div className="err">{error}</div>}
      <div style={{ marginBottom: 12, maxWidth: 320 }}>
        <input
          className="input"
          placeholder="Filter by id, status, agent…"
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
        />
      </div>
      <table className="table">
        <thead><tr><th>ID</th><th>Agent</th><th>Status</th><th>Created</th></tr></thead>
        <tbody>
          {rows.map((t) => (
            <tr key={t.id}>
              <td><Link href={`/transactions/${t.id}`}>{t.id}</Link></td>
              <td className="muted">{t.agent_id || "—"}</td>
              <td><span className="pill">{t.status}</span></td>
              <td className="muted">{t.created_at || "—"}</td>
            </tr>
          ))}
          {rows.length === 0 && <tr><td colSpan={4} className="muted">None.</td></tr>}
        </tbody>
      </table>
    </div>
  );
}

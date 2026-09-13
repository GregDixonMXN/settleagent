import { useEffect, useState } from "react";
import { api, AuditEvent } from "../lib/api";

export default function Audit() {
  const [rows, setRows] = useState<AuditEvent[]>([]);
  const [error, setError] = useState("");
  const [txn, setTxn] = useState("");

  const load = (txnId?: string) =>
    api.listAudit(txnId || undefined).then((r) => setRows(r || [])).catch((e) => setError(e.message));

  useEffect(() => { load(); }, []);

  return (
    <div>
      <h1>Audit log</h1>
      <p className="sub">Append-only event stream. Filter by transaction ID.</p>
      {error && <div className="err">{error}</div>}
      <div className="row" style={{ marginBottom: 12, maxWidth: 480 }}>
        <input className="input" placeholder="transaction_id (optional)" value={txn} onChange={(e) => setTxn(e.target.value)} />
        <button className="btn" onClick={() => load(txn || undefined)}>Filter</button>
      </div>
      <table className="table">
        <thead><tr><th>Type</th><th>Actor</th><th>Transaction</th><th>At</th></tr></thead>
        <tbody>
          {rows.map((a, i) => (
            <tr key={i}>
              <td>{a.type}</td>
              <td className="muted">{a.actor_type}/{a.actor_id}</td>
              <td className="muted">{a.transaction_id?.slice(0, 8) || "—"}</td>
              <td className="muted">{a.created_at || "—"}</td>
            </tr>
          ))}
          {rows.length === 0 && <tr><td colSpan={4} className="muted">No events.</td></tr>}
        </tbody>
      </table>
    </div>
  );
}

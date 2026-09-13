import { useEffect, useState } from "react";
import { api, Receipt } from "../lib/api";

export default function Receipts() {
  const [rows, setRows] = useState<Receipt[]>([]);
  const [error, setError] = useState("");
  const [txn, setTxn] = useState("");

  const load = (txnId?: string) =>
    api.listReceipts(txnId || undefined).then((r) => setRows(r || [])).catch((e) => setError(e.message));

  useEffect(() => { load(); }, []);

  return (
    <div>
      <h1>Receipts</h1>
      <p className="sub">Hash-chained execution receipts. Filter by transaction ID.</p>
      {error && <div className="err">{error}</div>}
      <div className="row" style={{ marginBottom: 12, maxWidth: 480 }}>
        <input className="input" placeholder="transaction_id (optional)" value={txn} onChange={(e) => setTxn(e.target.value)} />
        <button className="btn" onClick={() => load(txn || undefined)}>Filter</button>
      </div>
      <table className="table">
        <thead><tr><th>ID</th><th>Transaction</th><th>Action</th><th>Status</th><th>Hash</th></tr></thead>
        <tbody>
          {rows.map((r) => (
            <tr key={r.id}>
              <td className="muted">{r.id.slice(0, 8)}</td>
              <td className="muted">{r.transaction_id.slice(0, 8)}</td>
              <td className="muted">{r.action_id?.slice(0, 8) || "—"}</td>
              <td><span className="pill">{r.status || "—"}</span></td>
              <td className="muted"><code>{r.hash?.slice(0, 16) || "—"}</code></td>
            </tr>
          ))}
          {rows.length === 0 && <tr><td colSpan={5} className="muted">No receipts.</td></tr>}
        </tbody>
      </table>
    </div>
  );
}

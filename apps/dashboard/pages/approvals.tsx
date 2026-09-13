import { useEffect, useState } from "react";
import Link from "next/link";
import { api, Approval } from "../lib/api";

export default function Approvals() {
  const [items, setItems] = useState<Approval[]>([]);
  const [error, setError] = useState("");
  const [busy, setBusy] = useState<string | null>(null);

  const load = () =>
    api.listApprovals().then((a) => setItems(a || [])).catch((e) => setError(e.message));

  useEffect(() => { load(); }, []);

  const decide = async (id: string, approve: boolean) => {
    setBusy(id);
    setError("");
    try {
      await api.decideApproval(id, approve);
      await load();
    } catch (e: any) {
      setError(e.message);
    } finally {
      setBusy(null);
    }
  };

  return (
    <div>
      <h1>Approvals</h1>
      <p className="sub">{items.length} pending. Decisions are recorded with decided_by=dashboard.</p>
      {error && <div className="err">{error}</div>}
      <table className="table">
        <thead><tr><th>ID</th><th>Transaction</th><th>Action</th><th>Reason</th><th>Decide</th></tr></thead>
        <tbody>
          {items.map((a) => (
            <tr key={a.id}>
              <td className="muted">{a.id.slice(0, 8)}</td>
              <td><Link href={`/transactions/${a.transaction_id}`}>{a.transaction_id.slice(0, 8)}</Link></td>
              <td className="muted">{a.action_id.slice(0, 8)}</td>
              <td className="muted">{a.reason || "—"}</td>
              <td>
                <div className="row">
                  <button className="btn primary" disabled={busy === a.id} onClick={() => decide(a.id, true)}>Approve</button>
                  <button className="btn danger" disabled={busy === a.id} onClick={() => decide(a.id, false)}>Deny</button>
                </div>
              </td>
            </tr>
          ))}
          {items.length === 0 && <tr><td colSpan={5} className="muted">Queue is empty.</td></tr>}
        </tbody>
      </table>
    </div>
  );
}

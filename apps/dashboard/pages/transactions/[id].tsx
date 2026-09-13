import { useRouter } from "next/router";
import { useEffect, useState } from "react";
import Link from "next/link";
import { api, TxnDetail } from "../../lib/api";

export default function TxnDetailPage() {
  const router = useRouter();
  const { id } = router.query;
  const [data, setData] = useState<TxnDetail | null>(null);
  const [error, setError] = useState("");

  useEffect(() => {
    if (typeof id !== "string") return;
    api.getTxn(id).then(setData).catch((e) => setError(e.message));
  }, [id]);

  if (error) return <div><h1>Transaction</h1><div className="err">{error}</div></div>;
  if (!data) return <div><h1>Transaction</h1><p className="muted">Loading…</p></div>;

  const { transaction: t, actions, receipts, audit } = data;
  const events = [
    ...(audit || []).map((a) => ({ ts: a.created_at || "", label: a.type, detail: JSON.stringify(a.payload || {}) })),
    ...(actions || []).map((a) => ({ ts: a.created_at || "", label: `action ${a.status}: ${a.tool}.${a.action}`, detail: a.id })),
    ...(receipts || []).map((r) => ({ ts: r.created_at || "", label: `receipt ${r.status || ""}`, detail: r.id })),
  ].sort((x, y) => x.ts.localeCompare(y.ts));

  return (
    <div>
      <p className="muted"><Link href="/transactions">← Transactions</Link></p>
      <h1>{t.id}</h1>
      <p className="sub">
        <span className="pill">{t.status}</span>
        {"  "}agent <code>{t.agent_id}</code>
        {t.objective ? ` — ${t.objective}` : ""}
      </p>

      <div className="section">
        <h2>Timeline</h2>
        <ul className="timeline">
          {events.map((e, i) => (
            <li key={i}>
              <span className="muted">{e.ts || "—"}</span>
              <span><strong>{e.label}</strong><br /><span className="muted">{e.detail}</span></span>
            </li>
          ))}
          {events.length === 0 && <li><span className="muted">—</span><span className="muted">No events.</span></li>}
        </ul>
      </div>

      <div className="section">
        <h2>Actions ({(actions || []).length})</h2>
        <table className="table">
          <thead><tr><th>ID</th><th>Tool.Action</th><th>Status</th></tr></thead>
          <tbody>
            {(actions || []).map((a) => (
              <tr key={a.id}><td className="muted">{a.id}</td><td>{a.tool}.{a.action}</td><td><span className="pill">{a.status}</span></td></tr>
            ))}
          </tbody>
        </table>
      </div>

      <div className="section">
        <h2>Receipts ({(receipts || []).length})</h2>
        <pre className="code">{JSON.stringify(receipts, null, 2)}</pre>
      </div>

      <div className="section">
        <h2>Audit ({(audit || []).length})</h2>
        <table className="table">
          <thead><tr><th>Type</th><th>Actor</th><th>At</th></tr></thead>
          <tbody>
            {(audit || []).map((a, i) => (
              <tr key={i}><td>{a.type}</td><td className="muted">{a.actor_type}/{a.actor_id}</td><td className="muted">{a.created_at}</td></tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

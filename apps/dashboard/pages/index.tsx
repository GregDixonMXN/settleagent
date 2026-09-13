import { useEffect, useState } from "react";
import Link from "next/link";
import { api, Transaction, Approval } from "../lib/api";

function pill(status: string) {
  const s = status.toLowerCase();
  if (["succeeded", "approved", "executed", "allowed", "complete", "completed"].includes(s))
    return "pill ok";
  if (["denied", "failed", "blocked", "rejected"].includes(s)) return "pill bad";
  if (["pending", "awaiting_approval", "planning", "proposed"].includes(s)) return "pill warn";
  return "pill";
}

export default function Overview() {
  const [txns, setTxns] = useState<Transaction[]>([]);
  const [approvals, setApprovals] = useState<Approval[]>([]);
  const [error, setError] = useState("");

  useEffect(() => {
    (async () => {
      try {
        const [t, a] = await Promise.all([api.listTxns(), api.listApprovals()]);
        setTxns(t || []);
        setApprovals(a || []);
      } catch (e: any) {
        setError(e.message);
      }
    })();
  }, []);

  const byStatus: Record<string, number> = {};
  txns.forEach((t) => {
    byStatus[t.status] = (byStatus[t.status] || 0) + 1;
  });

  return (
    <div>
      <h1>Overview</h1>
      <p className="sub">Live state pulled from the AgentGuard API.</p>
      {error && <div className="err">{error}</div>}
      <div className="grid">
        <div className="card"><div className="k">Transactions</div><div className="v">{txns.length}</div></div>
        <div className="card"><div className="k">Pending approvals</div><div className="v">{approvals.length}</div></div>
        <div className="card"><div className="k">Blocked / denied</div><div className="v">{(byStatus["blocked"] || 0) + (byStatus["denied"] || 0)}</div></div>
        <div className="card"><div className="k">Succeeded</div><div className="v">{byStatus["succeeded"] || byStatus["completed"] || 0}</div></div>
      </div>
      <div className="section">
        <h2>Recent transactions</h2>
        <table className="table">
          <thead><tr><th>ID</th><th>Agent</th><th>Status</th><th>Objective</th></tr></thead>
          <tbody>
            {(txns || []).slice(0, 10).map((t) => (
              <tr key={t.id}>
                <td><Link href={`/transactions/${t.id}`}>{t.id.slice(0, 8)}</Link></td>
                <td className="muted">{t.agent_id?.slice(0, 8) || "—"}</td>
                <td><span className={pill(t.status)}>{t.status}</span></td>
                <td className="muted">{t.objective?.slice(0, 80) || "—"}</td>
              </tr>
            ))}
            {txns.length === 0 && <tr><td colSpan={4} className="muted">No transactions yet.</td></tr>}
          </tbody>
        </table>
      </div>
    </div>
  );
}

export const API_URL =
  process.env.NEXT_PUBLIC_API_URL || "http://localhost:8080";

export const ORG_ID =
  process.env.NEXT_PUBLIC_ORG_ID || "org_demo";

export const API_TOKEN =
  process.env.NEXT_PUBLIC_API_TOKEN || "";

async function req<T>(path: string, init?: RequestInit): Promise<T> {
  const res = await fetch(`${API_URL}${path}`, {
    ...init,
    headers: {
      "Content-Type": "application/json",
      "X-Org-ID": ORG_ID,
      ...(API_TOKEN ? { Authorization: `Bearer ${API_TOKEN}` } : {}),
      ...(init?.headers || {}),
    },
  });
  if (!res.ok) {
    const text = await res.text().catch(() => "");
    throw new Error(`${res.status} ${path}: ${text}`);
  }
  const ct = res.headers.get("content-type") || "";
  if (!ct.includes("json")) return undefined as T;
  return (await res.json()) as T;
}

export interface Transaction {
  id: string;
  org_id?: string;
  agent_id: string;
  principal_id?: string;
  session_id?: string;
  objective?: string;
  status: string;
  created_at?: string;
  updated_at?: string;
}

export interface ActionItem {
  id: string;
  transaction_id: string;
  tool: string;
  action: string;
  status: string;
  arguments?: Record<string, unknown>;
  created_at?: string;
}

export interface Approval {
  id: string;
  action_id: string;
  transaction_id: string;
  status: string;
  reason?: string;
  created_at?: string;
}

export interface Receipt {
  id: string;
  transaction_id: string;
  action_id?: string;
  status?: string;
  result?: unknown;
  hash?: string;
  prev_hash?: string;
  created_at?: string;
}

export interface AuditEvent {
  id?: string;
  type: string;
  actor_type?: string;
  actor_id?: string;
  transaction_id?: string;
  payload?: unknown;
  created_at?: string;
}

export interface Agent {
  id: string;
  name: string;
  principal_id?: string;
  environment?: string;
  groups?: string[];
  created_at?: string;
}

export interface TxnDetail {
  transaction: Transaction;
  actions: ActionItem[];
  receipts: Receipt[];
  audit: AuditEvent[];
}

export const api = {
  health: () => req<{ ok: boolean }>(`/health`),
  listTxns: () => req<Transaction[]>(`/v1/transactions`),
  getTxn: (id: string) => req<TxnDetail>(`/v1/transactions/${id}`),
  listApprovals: () => req<Approval[]>(`/v1/approvals`),
  decideApproval: (id: string, approve: boolean, decided_by = "dashboard") =>
    req<unknown>(`/v1/approvals/${id}/decide`, {
      method: "POST",
      body: JSON.stringify({ approve, decided_by }),
    }),
  listReceipts: (txnId?: string) =>
    req<Receipt[]>(
      txnId ? `/v1/receipts?transaction_id=${encodeURIComponent(txnId)}` : `/v1/receipts`
    ),
  listAudit: (txnId?: string) =>
    req<AuditEvent[]>(
      txnId ? `/v1/audit?transaction_id=${encodeURIComponent(txnId)}` : `/v1/audit`
    ),
  listPolicies: () => req<unknown[]>(`/v1/policies`),
  registerAgent: (body: {
    principal_id: string;
    name: string;
    environment: string;
    groups: string[];
  }) =>
    req<{ agent: Agent }>(`/v1/agents`, {
      method: "POST",
      body: JSON.stringify(body),
    }),
};

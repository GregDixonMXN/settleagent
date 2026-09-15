import type { NextApiRequest, NextApiResponse } from "next";

const API_URL = process.env.SETTLEAGENT_API_URL || "http://localhost:8080";
const OPERATOR_TOKEN = process.env.SETTLEAGENT_OPERATOR_TOKEN || "";
const ORG_ID = process.env.SETTLEAGENT_ORG_ID || "org_demo";

async function expectedSession(password: string): Promise<string> {
  const data = new TextEncoder().encode("agdash:" + password);
  const digest = await crypto.subtle.digest("SHA-256", data);
  return [...new Uint8Array(digest)]
    .map((b) => b.toString(16).padStart(2, "0"))
    .join("");
}

async function sessionOK(req: NextApiRequest): Promise<boolean> {
  const password = process.env.DASHBOARD_PASSWORD || "";
  if (!password) return true; // local dev, same rule as middleware
  const cookie = req.cookies["st_session"];
  return !!cookie && cookie === (await expectedSession(password));
}

// Browser never holds the operator token: it calls same-origin
// /api/backend/* with its session cookie, and this route attaches the
// server-side credential before forwarding to the gateway.
export default async function handler(req: NextApiRequest, res: NextApiResponse) {
  if (!(await sessionOK(req))) {
    res.status(401).json({ error: "locked" });
    return;
  }
  if (req.method !== "GET" && req.method !== "POST") {
    res.status(405).end();
    return;
  }
  const segs = req.query.path;
  const path = Array.isArray(segs) ? segs.join("/") : String(segs ?? "");
  if (path.includes("..") || path.includes("//")) {
    res.status(400).json({ error: "bad path" });
    return;
  }
  const qs = req.url && req.url.includes("?") ? req.url.slice(req.url.indexOf("?")) : "";
  const headers: Record<string, string> = { "X-Org-ID": ORG_ID };
  if (OPERATOR_TOKEN) headers["Authorization"] = `Bearer ${OPERATOR_TOKEN}`;
  const init: RequestInit = { method: req.method, headers };
  if (req.method === "POST") {
    headers["Content-Type"] = "application/json";
    init.body = JSON.stringify(req.body ?? {});
  }
  let upstream: Response;
  try {
    upstream = await fetch(`${API_URL}/${path}${qs}`, init);
  } catch {
    res.status(502).json({ error: "gateway unreachable" });
    return;
  }
  const text = await upstream.text().catch(() => "");
  res.status(upstream.status);
  const ct = upstream.headers.get("content-type") || "";
  if (ct.includes("json")) {
    res.setHeader("Content-Type", "application/json");
  } else if (text) {
    res.setHeader("Content-Type", "text/plain; charset=utf-8");
  }
  res.send(text);
}

export const config = {
  api: { bodyParser: { sizeLimit: "2mb" } },
};

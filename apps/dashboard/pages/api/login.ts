import type { NextApiRequest, NextApiResponse } from "next";

async function expectedSession(password: string): Promise<string> {
  const data = new TextEncoder().encode("agdash:" + password);
  const digest = await crypto.subtle.digest("SHA-256", data);
  return [...new Uint8Array(digest)].map((b) => b.toString(16).padStart(2, "0")).join("");
}

export default async function handler(req: NextApiRequest, res: NextApiResponse) {
  if (req.method !== "POST") {
    res.status(405).end();
    return;
  }
  const password = process.env.DASHBOARD_PASSWORD || "";
  const { password: given } = req.body || {};
  if (!password || given !== password) {
    res.status(401).json({ error: "bad password" });
    return;
  }
  const session = await expectedSession(password);
  res.setHeader(
    "Set-Cookie",
    `st_session=${session}; Path=/; HttpOnly; SameSite=Lax; Max-Age=86400${
      process.env.NODE_ENV === "production" ? "; Secure" : ""
    }`
  );
  res.status(200).json({ ok: true });
}

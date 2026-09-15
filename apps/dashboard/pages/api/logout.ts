import type { NextApiRequest, NextApiResponse } from "next";

export default function handler(_req: NextApiRequest, res: NextApiResponse) {
  res.setHeader("Set-Cookie", "st_session=; Path=/; HttpOnly; SameSite=Lax; Max-Age=0");
  res.status(200).json({ ok: true });
}

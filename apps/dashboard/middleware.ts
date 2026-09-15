import { NextResponse } from "next/server";
import type { NextRequest } from "next/server";

async function expectedSession(password: string): Promise<string> {
  const data = new TextEncoder().encode("agdash:" + password);
  const digest = await crypto.subtle.digest("SHA-256", data);
  return [...new Uint8Array(digest)].map((b) => b.toString(16).padStart(2, "0")).join("");
}

// Single-operator gate (interim: not multi-user, no SSO — those are
// enterprise-tier per the open-source strategy). With DASHBOARD_PASSWORD
// unset the dashboard is open (local dev only).
export async function middleware(req: NextRequest) {
  const password = process.env.DASHBOARD_PASSWORD || "";
  const path = req.nextUrl.pathname;
  if (!password || path === "/login" || path.startsWith("/api/login")) {
    return NextResponse.next();
  }
  const cookie = req.cookies.get("st_session")?.value;
  if (cookie && cookie === (await expectedSession(password))) {
    return NextResponse.next();
  }
  return NextResponse.redirect(new URL("/login", req.url));
}

export const config = {
  matcher: ["/((?!_next/static|_next/image|favicon.ico).*)"],
};

import Link from "next/link";
import { useRouter } from "next/router";
import { ReactNode } from "react";
import { API_URL, ORG_ID } from "../lib/api";

const NAV = [
  { href: "/", label: "Overview" },
  { href: "/transactions", label: "Transactions" },
  { href: "/approvals", label: "Approvals" },
  { href: "/agents", label: "Agents" },
  { href: "/authority", label: "Authority" },
  { href: "/policies", label: "Policies" },
  { href: "/receipts", label: "Receipts" },
  { href: "/audit", label: "Audit log" },
];

export default function Layout({ children }: { children: ReactNode }) {
  const router = useRouter();
  const active = (href: string) =>
    href === "/" ? router.pathname === "/" : router.pathname.startsWith(href);
  return (
    <div className="shell">
      <aside className="side">
        <div className="brand">SettleAgent</div>
        <nav>
          {NAV.map((n) => (
            <Link key={n.href} href={n.href} className={active(n.href) ? "nav on" : "nav"}>
              {n.label}
            </Link>
          ))}
        </nav>
        <div className="meta">
          <div>{ORG_ID}</div>
          <div className="muted">{API_URL}</div>
        </div>
      </aside>
      <main className="main">{children}</main>
    </div>
  );
}

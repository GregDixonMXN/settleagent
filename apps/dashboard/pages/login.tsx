import { useState } from "react";
import { useRouter } from "next/router";

export default function Login() {
  const [password, setPassword] = useState("");
  const [error, setError] = useState("");
  const router = useRouter();

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setError("");
    const res = await fetch("/api/login", {
      method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify({ password }),
    });
    if (res.ok) {
      router.push("/");
    } else {
      setError("Wrong password.");
    }
  }

  return (
    <div style={{ maxWidth: 360, margin: "15vh auto" }}>
      <h1>SettleAgent</h1>
      <p className="sub">Operator sign-in. Single-operator gate for v0.1.</p>
      {error && <div className="err">{error}</div>}
      <form onSubmit={submit}>
        <input
          className="input"
          type="password"
          placeholder="Operator password"
          value={password}
          onChange={(e) => setPassword(e.target.value)}
          autoFocus
        />
        <button className="btn" type="submit" style={{ marginTop: 12 }}>
          Sign in
        </button>
      </form>
    </div>
  );
}

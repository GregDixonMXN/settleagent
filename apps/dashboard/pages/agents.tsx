import { useState } from "react";
import { api } from "../lib/api";

export default function Agents() {
  const [form, setForm] = useState({ principal_id: "", name: "", environment: "prod", groups: "" });
  const [result, setResult] = useState("");
  const [error, setError] = useState("");

  const submit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError(""); setResult("");
    try {
      const r = await api.registerAgent({
        principal_id: form.principal_id,
        name: form.name,
        environment: form.environment,
        groups: form.groups.split(",").map((g) => g.trim()).filter(Boolean),
      });
      setResult(JSON.stringify(r, null, 2));
    } catch (err: any) {
      setError(err.message);
    }
  };

  return (
    <div>
      <h1>Agents</h1>
      <p className="sub">Register a new agent. The API secret is shown once — store it.</p>
      {error && <div className="err">{error}</div>}
      <form className="form" onSubmit={submit}>
        <input className="input" placeholder="name" value={form.name} onChange={(e) => setForm({ ...form, name: e.target.value })} required />
        <input className="input" placeholder="principal_id" value={form.principal_id} onChange={(e) => setForm({ ...form, principal_id: e.target.value })} />
        <input className="input" placeholder="environment (prod)" value={form.environment} onChange={(e) => setForm({ ...form, environment: e.target.value })} />
        <input className="input" placeholder="groups (comma separated)" value={form.groups} onChange={(e) => setForm({ ...form, groups: e.target.value })} />
        <button className="btn primary" type="submit">Register agent</button>
      </form>
      {result && <div className="section"><pre className="code">{result}</pre></div>}
    </div>
  );
}

import { useState } from "react";
import { useNavigate } from "react-router-dom";
import { ApiError, api, tokens } from "../api/client";
import type { LoginResponse } from "../api/types";

export default function Login() {
  const nav = useNavigate();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [totp, setTotp] = useState("");
  const [needTotp, setNeedTotp] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function submit(e: React.FormEvent) {
    e.preventDefault();
    setErr(null);
    setBusy(true);
    try {
      const r = await api.post<LoginResponse>("/auth/login", { email, password, totp: totp || undefined });
      if (r.mfaRequired) {
        setNeedTotp(true);
      } else {
        tokens.set({ accessToken: r.accessToken, refreshToken: r.refreshToken, expiresAt: r.expiresAt });
        nav("/", { replace: true });
      }
    } catch (e) {
      if (e instanceof ApiError) setErr(e.message);
      else setErr("Login failed");
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="grid min-h-screen place-items-center px-4">
      <form
        onSubmit={submit}
        className="w-full max-w-sm space-y-4 rounded-xl border border-ink-800 bg-ink-900 p-6 shadow-xl"
      >
        <div className="space-y-1 text-center">
          <h1 className="text-xl font-semibold">
            <span className="text-sonar-400">ScanRay</span> Sonar
          </h1>
          <p className="text-xs text-slate-500">Sign in to the console</p>
        </div>

        <label className="block space-y-1">
          <span className="text-xs uppercase tracking-wide text-slate-400">Email</span>
          <input
            type="email"
            autoComplete="username"
            className="w-full rounded-md border border-ink-700 bg-ink-950 px-3 py-2 text-sm focus:border-sonar-500 focus:outline-none"
            value={email}
            onChange={(e) => setEmail(e.target.value)}
            required
          />
        </label>

        <label className="block space-y-1">
          <span className="text-xs uppercase tracking-wide text-slate-400">Password</span>
          <input
            type="password"
            autoComplete="current-password"
            className="w-full rounded-md border border-ink-700 bg-ink-950 px-3 py-2 text-sm focus:border-sonar-500 focus:outline-none"
            value={password}
            onChange={(e) => setPassword(e.target.value)}
            required
          />
        </label>

        {needTotp && (
          <label className="block space-y-1">
            <span className="text-xs uppercase tracking-wide text-slate-400">Authenticator code</span>
            <input
              inputMode="numeric"
              pattern="[0-9]{6}"
              maxLength={6}
              className="w-full rounded-md border border-ink-700 bg-ink-950 px-3 py-2 text-sm tracking-widest focus:border-sonar-500 focus:outline-none"
              value={totp}
              onChange={(e) => setTotp(e.target.value)}
              required
            />
          </label>
        )}

        {err && (
          <div className="rounded-md border border-red-900/60 bg-red-950/40 px-3 py-2 text-xs text-red-300">{err}</div>
        )}

        <button
          type="submit"
          disabled={busy}
          className="w-full rounded-md bg-sonar-600 px-3 py-2 text-sm font-medium text-white shadow hover:bg-sonar-500 disabled:opacity-50"
        >
          {busy ? "Signing in…" : "Sign in"}
        </button>
      </form>
    </div>
  );
}

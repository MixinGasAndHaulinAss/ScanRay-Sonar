import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { ApiError, api } from "../api/client";
import type { User } from "../api/types";

interface SMTPSettings {
  host: string;
  port: number;
  user: string;
  fromAddr: string;
  useTls: boolean;
  passwordSet: boolean;
}

interface WebhookRow {
  id: string;
  name: string;
  url: string;
  isActive: boolean;
  createdAt: string;
}

export default function Settings() {
  const qc = useQueryClient();
  const me = useQuery({ queryKey: ["me"], queryFn: () => api.get<User>("/auth/me") });
  const canEdit = me.data?.role === "siteadmin" || me.data?.role === "superadmin";

  const smtp = useQuery({
    queryKey: ["settings-smtp"],
    queryFn: () => api.get<SMTPSettings>("/settings/smtp"),
    enabled: !!me.data && canEdit,
  });

  const webhooks = useQuery({
    queryKey: ["settings-webhooks"],
    queryFn: () => api.get<WebhookRow[]>("/settings/webhooks"),
    enabled: !!me.data && canEdit,
  });

  const [host, setHost] = useState("");
  const [port, setPort] = useState(587);
  const [user, setUser] = useState("");
  const [password, setPassword] = useState("");
  const [fromAddr, setFromAddr] = useState("");
  const [useTls, setUseTls] = useState(true);
  const [smtpErr, setSmtpErr] = useState<string | null>(null);

  const [testTo, setTestTo] = useState("");
  const [whName, setWhName] = useState("");
  const [whUrl, setWhUrl] = useState("");
  const [whSecret, setWhSecret] = useState("");

  useEffect(() => {
    if (!smtp.data) return;
    setHost(smtp.data.host ?? "");
    setPort(smtp.data.port || 587);
    setUser(smtp.data.user ?? "");
    setFromAddr(smtp.data.fromAddr ?? "");
    setUseTls(!!smtp.data.useTls);
  }, [smtp.data]);

  const saveSmtp = useMutation({
    mutationFn: () =>
      api.put("/settings/smtp", {
        host,
        port,
        user,
        password,
        fromAddr,
        useTls,
      }),
    onSuccess: async () => {
      setPassword("");
      setSmtpErr(null);
      await qc.invalidateQueries({ queryKey: ["settings-smtp"] });
    },
    onError: (e: unknown) =>
      setSmtpErr(e instanceof ApiError ? e.message : "Save failed"),
  });

  const testSmtp = useMutation({
    mutationFn: () => api.post<{ ok: boolean }>("/settings/smtp/test", { to: testTo }),
    onError: (e: unknown) =>
      setSmtpErr(e instanceof ApiError ? e.message : "SMTP test failed"),
    onSuccess: () => setSmtpErr(null),
  });

  const createWh = useMutation({
    mutationFn: () =>
      api.post("/settings/webhooks", {
        name: whName,
        url: whUrl,
        signingSecret: whSecret || undefined,
      }),
    onSuccess: async () => {
      setWhName("");
      setWhUrl("");
      setWhSecret("");
      await qc.invalidateQueries({ queryKey: ["settings-webhooks"] });
    },
  });

  const patchWh = useMutation({
    mutationFn: (p: { id: string; body: Record<string, unknown> }) =>
      api.patch(`/settings/webhooks/${p.id}`, p.body),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["settings-webhooks"] }),
  });

  const deleteWh = useMutation({
    mutationFn: (id: string) => api.del(`/settings/webhooks/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["settings-webhooks"] }),
  });

  const testWh = useMutation({
    mutationFn: (id: string) => api.post(`/settings/webhooks/${id}/test`, {}),
  });

  if (!me.data) return <p className="text-sm text-slate-500">Loading…</p>;

  if (!canEdit) {
    return (
      <div className="rounded-lg border border-ink-800 bg-ink-900 px-4 py-6 text-sm text-slate-400">
        SMTP and outbound webhooks are limited to site administrators.
      </div>
    );
  }

  return (
    <div className="space-y-8">
      <div>
        <h2 className="text-2xl font-semibold tracking-tight">Settings</h2>
        <p className="mt-0.5 text-xs text-slate-500">
          Outbound email and signed webhook endpoints used by notifications.
        </p>
      </div>

      <section className="space-y-3 rounded-xl border border-ink-800 bg-ink-900 p-5">
        <h3 className="text-sm font-semibold text-slate-200">SMTP</h3>
        {smtp.data?.passwordSet && (
          <p className="text-xs text-slate-500">A stored password exists; leave blank to keep it.</p>
        )}
        {smtpErr && <p className="text-xs text-red-400">{smtpErr}</p>}
        <div className="grid gap-3 md:grid-cols-2">
          <label className="block text-xs text-slate-400">
            Host
            <input
              className="mt-1 w-full rounded-md border border-ink-700 bg-ink-950 px-3 py-2 text-sm"
              value={host}
              onChange={(e) => setHost(e.target.value)}
            />
          </label>
          <label className="block text-xs text-slate-400">
            Port
            <input
              type="number"
              className="mt-1 w-full rounded-md border border-ink-700 bg-ink-950 px-3 py-2 text-sm"
              value={port}
              onChange={(e) => setPort(Number(e.target.value))}
            />
          </label>
          <label className="block text-xs text-slate-400">
            User
            <input
              className="mt-1 w-full rounded-md border border-ink-700 bg-ink-950 px-3 py-2 text-sm"
              value={user}
              onChange={(e) => setUser(e.target.value)}
            />
          </label>
          <label className="block text-xs text-slate-400">
            Password
            <input
              type="password"
              autoComplete="new-password"
              className="mt-1 w-full rounded-md border border-ink-700 bg-ink-950 px-3 py-2 text-sm"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              placeholder="••••••••"
            />
          </label>
          <label className="block text-xs text-slate-400 md:col-span-2">
            From address
            <input
              className="mt-1 w-full rounded-md border border-ink-700 bg-ink-950 px-3 py-2 text-sm"
              value={fromAddr}
              onChange={(e) => setFromAddr(e.target.value)}
            />
          </label>
        </div>
        <label className="inline-flex items-center gap-2 text-xs text-slate-400">
          <input
            type="checkbox"
            className="accent-sonar-500"
            checked={useTls}
            onChange={(e) => setUseTls(e.target.checked)}
          />
          Use TLS (STARTTLS)
        </label>
        <div className="flex flex-wrap gap-2">
          <button
            type="button"
            disabled={saveSmtp.isPending}
            onClick={() => saveSmtp.mutate()}
            className="rounded-full border border-ink-700 bg-ink-950 px-4 py-1.5 text-xs text-slate-200 hover:bg-ink-800"
          >
            Save SMTP
          </button>
        </div>
        <div className="flex flex-wrap items-end gap-2 border-t border-ink-800 pt-4">
          <label className="block text-xs text-slate-400">
            Send test to
            <input
              className="mt-1 w-56 rounded-md border border-ink-700 bg-ink-950 px-3 py-2 text-sm"
              placeholder="you@example.com"
              value={testTo}
              onChange={(e) => setTestTo(e.target.value)}
            />
          </label>
          <button
            type="button"
            disabled={testSmtp.isPending || !testTo}
            onClick={() => testSmtp.mutate()}
            className="rounded-full border border-sonar-700 bg-sonar-950/40 px-4 py-2 text-xs text-sonar-200 hover:bg-sonar-900/40 disabled:opacity-40"
          >
            Send test
          </button>
          {testSmtp.isSuccess && <span className="text-xs text-emerald-400">Sent.</span>}
        </div>
      </section>

      <section className="space-y-3 rounded-xl border border-ink-800 bg-ink-900 p-5">
        <h3 className="text-sm font-semibold text-slate-200">Webhooks</h3>
        <div className="flex flex-wrap gap-2">
          <input
            placeholder="Name"
            className="rounded-md border border-ink-700 bg-ink-950 px-3 py-2 text-sm"
            value={whName}
            onChange={(e) => setWhName(e.target.value)}
          />
          <input
            placeholder="https://…"
            className="min-w-[240px] flex-1 rounded-md border border-ink-700 bg-ink-950 px-3 py-2 font-mono text-sm"
            value={whUrl}
            onChange={(e) => setWhUrl(e.target.value)}
          />
          <input
            placeholder="Signing secret (optional)"
            type="password"
            className="rounded-md border border-ink-700 bg-ink-950 px-3 py-2 text-sm"
            value={whSecret}
            onChange={(e) => setWhSecret(e.target.value)}
          />
          <button
            type="button"
            disabled={createWh.isPending || !whName || !whUrl}
            onClick={() => createWh.mutate()}
            className="rounded-full border border-ink-700 bg-ink-950 px-4 py-2 text-xs text-slate-200 hover:bg-ink-800 disabled:opacity-40"
          >
            Add webhook
          </button>
        </div>

        <div className="overflow-hidden rounded-lg border border-ink-800">
          <table className="w-full text-left text-sm">
            <thead className="bg-ink-800/60 text-xs uppercase text-slate-400">
              <tr>
                <th className="px-3 py-2">Name</th>
                <th className="px-3 py-2">URL</th>
                <th className="px-3 py-2 text-right">Actions</th>
              </tr>
            </thead>
            <tbody>
              {webhooks.data?.map((w) => (
                <tr key={w.id} className="border-t border-ink-800">
                  <td className="px-3 py-2">{w.name}</td>
                  <td className="max-w-md truncate px-3 py-2 font-mono text-xs text-slate-400">{w.url}</td>
                  <td className="px-3 py-2 text-right">
                    <button
                      type="button"
                      className="mr-2 text-xs text-sonar-300 hover:underline"
                      onClick={() =>
                        patchWh.mutate({
                          id: w.id,
                          body: { isActive: !w.isActive },
                        })
                      }
                    >
                      {w.isActive ? "Disable" : "Enable"}
                    </button>
                    <button
                      type="button"
                      className="mr-2 text-xs text-sonar-300 hover:underline"
                      onClick={() => testWh.mutate(w.id)}
                      disabled={testWh.isPending}
                    >
                      Test
                    </button>
                    <button
                      type="button"
                      className="text-xs text-red-400 hover:underline"
                      onClick={() => {
                        if (confirm("Delete this webhook?")) deleteWh.mutate(w.id);
                      }}
                    >
                      Delete
                    </button>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>
    </div>
  );
}

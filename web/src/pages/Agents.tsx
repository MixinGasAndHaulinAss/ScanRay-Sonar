import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useState } from "react";
import { Link } from "react-router-dom";
import { ApiError, api } from "../api/client";
import type { Agent, EnrollmentToken, NewEnrollmentToken, Site } from "../api/types";
import { formatBytes, formatPct, formatRelative, pctBarColor } from "../lib/format";

interface TokenForm {
  siteId: string;
  label: string;
  ttlHours: number;
  maxUses: number;
}

const EMPTY_TOKEN_FORM: TokenForm = {
  siteId: "",
  label: "",
  ttlHours: 24,
  maxUses: 1,
};

export default function Agents() {
  const qc = useQueryClient();
  const agents = useQuery({
    queryKey: ["agents"],
    queryFn: () => api.get<Agent[]>("/agents"),
    refetchInterval: 30_000,
  });
  const sites = useQuery({ queryKey: ["sites"], queryFn: () => api.get<Site[]>("/sites") });
  const tokens = useQuery({
    queryKey: ["enrollment-tokens"],
    queryFn: () => api.get<EnrollmentToken[]>("/agents/enrollment-tokens"),
  });

  const [open, setOpen] = useState(false);
  const [form, setForm] = useState<TokenForm>(EMPTY_TOKEN_FORM);
  const [err, setErr] = useState<string | null>(null);
  const [issued, setIssued] = useState<NewEnrollmentToken | null>(null);
  const [issuedOS, setIssuedOS] = useState<"linux" | "windows">("linux");
  const [copied, setCopied] = useState(false);

  useEffect(() => {
    if (!sites.data) return;
    setForm((f) => (f.siteId ? f : { ...f, siteId: sites.data?.[0]?.id ?? "" }));
  }, [sites.data]);

  const create = useMutation({
    mutationFn: (b: TokenForm) =>
      api.post<NewEnrollmentToken>("/agents/enrollment-tokens", b),
    onSuccess: (t) => {
      qc.invalidateQueries({ queryKey: ["enrollment-tokens"] });
      setIssued(t);
      setIssuedOS("linux");
      setOpen(false);
    },
    onError: (e) => setErr(e instanceof ApiError ? e.message : "Failed to issue token"),
  });

  const revoke = useMutation({
    mutationFn: (id: string) => api.del<void>(`/agents/enrollment-tokens/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["enrollment-tokens"] }),
  });

  const delAgent = useMutation({
    mutationFn: (id: string) => api.del<void>(`/agents/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["agents"] }),
  });

  const siteName = (id: string) => sites.data?.find((s) => s.id === id)?.name ?? id.slice(0, 8);

  return (
    <div className="space-y-6">
      <div className="flex items-end justify-between gap-4">
        <div>
          <h2 className="text-2xl font-semibold tracking-tight">Agents</h2>
          <p className="text-sm text-slate-400">
            Hosts running the Sonar Probe. Enroll a new host with a single-use install token.
          </p>
        </div>
        <button
          className="rounded-md bg-sonar-600 px-3 py-1.5 text-sm font-medium hover:bg-sonar-500"
          onClick={() => {
            setForm({ ...EMPTY_TOKEN_FORM, siteId: sites.data?.[0]?.id ?? "" });
            setErr(null);
            setOpen(true);
          }}
        >
          Add agent
        </button>
      </div>

      {issued && (
        <div className="rounded-xl border border-emerald-700 bg-emerald-950/40 p-4">
          <div className="flex items-center justify-between gap-3">
            <div className="text-sm font-medium text-emerald-200">
              New enrollment token — copy the install command now (token is shown only once)
            </div>
            <button
              onClick={() => setIssued(null)}
              className="text-xs text-emerald-300 hover:underline"
            >
              Dismiss
            </button>
          </div>

          <div className="mt-3 flex items-center gap-1 text-xs">
            {(["linux", "windows"] as const).map((os) => (
              <button
                key={os}
                onClick={() => {
                  setIssuedOS(os);
                  setCopied(false);
                }}
                className={
                  "rounded-md px-3 py-1 font-medium " +
                  (issuedOS === os
                    ? "bg-emerald-900/60 text-emerald-100"
                    : "border border-emerald-800/60 text-emerald-300 hover:bg-emerald-900/30")
                }
              >
                {os === "linux" ? "Linux (bash)" : "Windows (PowerShell)"}
              </button>
            ))}
          </div>

          <pre className="mt-2 max-h-48 overflow-auto whitespace-pre-wrap break-all rounded bg-ink-950 p-3 font-mono text-xs text-emerald-100">
            {issuedOS === "linux"
              ? issued.installCmds?.linux ?? issued.installCmd
              : issued.installCmds?.windows ?? ""}
          </pre>

          <p className="mt-2 text-xs text-emerald-300/80">
            {issuedOS === "linux"
              ? "Paste into a root shell on the Linux host. Downloads the probe, enrolls, and starts the systemd unit."
              : "Paste into an elevated cmd.exe or PowerShell prompt on the Windows host. Downloads sonar-probe.exe, enrolls, and registers the SonarProbe service via the SCM."}
          </p>

          <div className="mt-2 flex items-center gap-3 text-xs text-emerald-300">
            <button
              onClick={() => {
                const cmd =
                  issuedOS === "linux"
                    ? issued.installCmds?.linux ?? issued.installCmd
                    : issued.installCmds?.windows ?? "";
                navigator.clipboard.writeText(cmd);
                setCopied(true);
                setTimeout(() => setCopied(false), 1500);
              }}
              className="rounded-md border border-emerald-700 px-2 py-1 text-emerald-200 hover:bg-emerald-900/40"
            >
              {copied ? "Copied!" : "Copy install command"}
            </button>
            <span>
              Site: {siteName(issued.siteId)} · Expires{" "}
              {new Date(issued.expiresAt).toLocaleString()} ·{" "}
              {issued.maxUses === 1 ? "single-use" : `${issued.maxUses} uses`}
            </span>
          </div>
        </div>
      )}

      <div className="space-y-2">
        <h3 className="text-sm font-semibold uppercase tracking-wide text-slate-400">Hosts</h3>
        <div className="overflow-hidden rounded-xl border border-ink-800 bg-ink-900">
          <table className="w-full text-left text-sm">
            <thead className="bg-ink-800/60 text-xs uppercase tracking-wide text-slate-400">
              <tr>
                <th className="px-4 py-2">Hostname</th>
                <th className="px-4 py-2">Site</th>
                <th className="px-4 py-2">OS</th>
                <th className="px-4 py-2">CPU</th>
                <th className="px-4 py-2">Memory</th>
                <th className="px-4 py-2">Disk</th>
                <th className="px-4 py-2">IP</th>
                <th className="px-4 py-2">Last seen</th>
                <th className="px-4 py-2">Status</th>
                <th className="px-4 py-2 text-right">Actions</th>
              </tr>
            </thead>
            <tbody>
              {agents.isLoading && (
                <tr>
                  <td colSpan={10} className="px-4 py-6 text-center text-slate-500">
                    Loading…
                  </td>
                </tr>
              )}
              {agents.data?.length === 0 && (
                <tr>
                  <td colSpan={10} className="px-4 py-6 text-center text-slate-500">
                    No agents enrolled yet. Click <strong>Add agent</strong> for an install
                    one-liner.
                  </td>
                </tr>
              )}
              {agents.data?.map((a) => {
                const online =
                  a.lastSeenAt && Date.now() - new Date(a.lastSeenAt).getTime() < 5 * 60_000;
                const memPct =
                  a.memUsedBytes != null && a.memTotalBytes && a.memTotalBytes > 0
                    ? (Number(a.memUsedBytes) / Number(a.memTotalBytes)) * 100
                    : null;
                const diskPct =
                  a.rootDiskUsedBytes != null &&
                  a.rootDiskTotalBytes &&
                  a.rootDiskTotalBytes > 0
                    ? (Number(a.rootDiskUsedBytes) / Number(a.rootDiskTotalBytes)) * 100
                    : null;
                return (
                  <tr key={a.id} className="border-t border-ink-800 hover:bg-ink-800/30">
                    <td className="px-4 py-2">
                      <Link
                        to={`/agents/${a.id}`}
                        className="font-medium text-sonar-300 hover:underline"
                      >
                        {a.hostname}
                      </Link>
                      {a.pendingReboot && (
                        <span
                          title="Reboot pending"
                          className="ml-2 rounded bg-amber-900/50 px-1.5 py-0.5 text-[10px] text-amber-300"
                        >
                          reboot
                        </span>
                      )}
                    </td>
                    <td className="px-4 py-2 text-slate-400">{siteName(a.siteId)}</td>
                    <td className="px-4 py-2 text-slate-400">
                      <div>
                        {a.os} {a.osVersion}
                      </div>
                      <div className="text-[10px] text-slate-600">
                        v{a.agentVersion || "?"}
                      </div>
                    </td>
                    <td className="px-4 py-2">
                      <MetricCell pct={a.cpuPct ?? null} />
                    </td>
                    <td className="px-4 py-2">
                      <MetricCell
                        pct={memPct}
                        sub={a.memTotalBytes ? formatBytes(Number(a.memTotalBytes)) : ""}
                      />
                    </td>
                    <td className="px-4 py-2">
                      <MetricCell
                        pct={diskPct}
                        sub={
                          a.rootDiskTotalBytes ? formatBytes(Number(a.rootDiskTotalBytes)) : ""
                        }
                      />
                    </td>
                    <td className="px-4 py-2 font-mono text-xs text-slate-400">
                      {a.primaryIp || "—"}
                    </td>
                    <td className="px-4 py-2 text-xs text-slate-500" title={a.lastSeenAt ?? ""}>
                      {formatRelative(a.lastSeenAt)}
                    </td>
                    <td className="px-4 py-2">
                      <span
                        className={
                          online
                            ? "rounded bg-emerald-900/40 px-2 py-0.5 text-xs text-emerald-300"
                            : "rounded bg-slate-800 px-2 py-0.5 text-xs text-slate-400"
                        }
                      >
                        {online ? "online" : "offline"}
                      </span>
                    </td>
                    <td className="px-4 py-2 text-right">
                      <button
                        onClick={() => {
                          if (confirm(`Remove agent "${a.hostname}"?`)) delAgent.mutate(a.id);
                        }}
                        className="rounded-md border border-ink-700 px-2 py-1 text-xs text-red-300 hover:bg-red-900/30"
                      >
                        Remove
                      </button>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </div>

      <div className="space-y-2">
        <h3 className="text-sm font-semibold uppercase tracking-wide text-slate-400">
          Outstanding enrollment tokens
        </h3>
        <div className="overflow-hidden rounded-xl border border-ink-800 bg-ink-900">
          <table className="w-full text-left text-sm">
            <thead className="bg-ink-800/60 text-xs uppercase tracking-wide text-slate-400">
              <tr>
                <th className="px-4 py-2">Label</th>
                <th className="px-4 py-2">Site</th>
                <th className="px-4 py-2">Uses</th>
                <th className="px-4 py-2">Expires</th>
                <th className="px-4 py-2">Status</th>
                <th className="px-4 py-2 text-right">Actions</th>
              </tr>
            </thead>
            <tbody>
              {tokens.data?.length === 0 && (
                <tr>
                  <td colSpan={6} className="px-4 py-6 text-center text-slate-500">
                    No outstanding tokens.
                  </td>
                </tr>
              )}
              {tokens.data?.map((t) => (
                <tr key={t.id} className="border-t border-ink-800 hover:bg-ink-800/30">
                  <td className="px-4 py-2">{t.label}</td>
                  <td className="px-4 py-2 text-slate-400">{siteName(t.siteId)}</td>
                  <td className="px-4 py-2 text-slate-400">
                    {t.usedCount} / {t.maxUses}
                  </td>
                  <td className="px-4 py-2 text-slate-500">
                    {new Date(t.expiresAt).toLocaleString()}
                  </td>
                  <td className="px-4 py-2">
                    <span
                      className={
                        t.isValid
                          ? "rounded bg-emerald-900/40 px-2 py-0.5 text-xs text-emerald-300"
                          : "rounded bg-slate-800 px-2 py-0.5 text-xs text-slate-400"
                      }
                    >
                      {t.isValid ? "valid" : t.revokedAt ? "revoked" : "expired/used"}
                    </span>
                  </td>
                  <td className="px-4 py-2 text-right">
                    {t.isValid && (
                      <button
                        onClick={() => revoke.mutate(t.id)}
                        className="rounded-md border border-ink-700 px-2 py-1 text-xs text-red-300 hover:bg-red-900/30"
                      >
                        Revoke
                      </button>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </div>

      {open && (
        <div className="fixed inset-0 z-20 grid place-items-center bg-black/60 px-4">
          <form
            className="w-full max-w-md space-y-3 rounded-xl border border-ink-800 bg-ink-900 p-5"
            onSubmit={(e) => {
              e.preventDefault();
              create.mutate(form);
            }}
          >
            <h3 className="text-lg font-semibold">Issue enrollment token</h3>
            <p className="text-xs text-slate-400">
              Generates a one-time install command you paste on the target host. After
              issuance, pick the OS tab (Linux or Windows) — Linux installs as a systemd
              unit (<code>sudo</code>), Windows installs as the <code>SonarProbe</code>{" "}
              service (elevated PowerShell).
            </p>
            <label className="block text-xs text-slate-400">
              Site
              <select
                required
                value={form.siteId}
                onChange={(e) => setForm({ ...form, siteId: e.target.value })}
                className="mt-1 w-full rounded-md border border-ink-700 bg-ink-950 px-3 py-2 text-sm text-slate-100"
              >
                <option value="">— select site —</option>
                {sites.data?.map((s) => (
                  <option key={s.id} value={s.id}>
                    {s.name}
                  </option>
                ))}
              </select>
            </label>
            <label className="block text-xs text-slate-400">
              Label (free-form, helps audit)
              <input
                className="mt-1 w-full rounded-md border border-ink-700 bg-ink-950 px-3 py-2 text-sm"
                value={form.label}
                onChange={(e) => setForm({ ...form, label: e.target.value })}
                placeholder="e.g. server-room batch"
              />
            </label>
            <div className="grid grid-cols-2 gap-3">
              <label className="block text-xs text-slate-400">
                Valid for (hours)
                <input
                  type="number"
                  min={1}
                  max={720}
                  className="mt-1 w-full rounded-md border border-ink-700 bg-ink-950 px-3 py-2 text-sm"
                  value={form.ttlHours}
                  onChange={(e) => setForm({ ...form, ttlHours: Number(e.target.value) })}
                />
              </label>
              <label className="block text-xs text-slate-400">
                Max enrollments
                <input
                  type="number"
                  min={1}
                  max={100}
                  className="mt-1 w-full rounded-md border border-ink-700 bg-ink-950 px-3 py-2 text-sm"
                  value={form.maxUses}
                  onChange={(e) => setForm({ ...form, maxUses: Number(e.target.value) })}
                />
              </label>
            </div>
            {err && <div className="text-xs text-red-300">{err}</div>}
            <div className="flex justify-end gap-2 pt-2">
              <button
                type="button"
                className="rounded-md border border-ink-700 px-3 py-1.5 text-sm"
                onClick={() => setOpen(false)}
              >
                Cancel
              </button>
              <button
                type="submit"
                disabled={create.isPending}
                className="rounded-md bg-sonar-600 px-3 py-1.5 text-sm hover:bg-sonar-500 disabled:opacity-50"
              >
                {create.isPending ? "Issuing…" : "Issue token"}
              </button>
            </div>
          </form>
        </div>
      )}
    </div>
  );
}

// MetricCell renders a small "% + bar + sub" trio used by the agent
// list. Pulled out of the table body to keep that map() readable.
function MetricCell({ pct, sub }: { pct: number | null; sub?: string }) {
  if (pct == null) return <span className="text-xs text-slate-600">—</span>;
  const clamped = Math.min(100, Math.max(0, pct));
  return (
    <div className="min-w-[80px] space-y-1">
      <div className="text-xs tabular-nums text-slate-200">{formatPct(pct)}</div>
      <div className="h-1 w-20 overflow-hidden rounded bg-ink-800">
        <div
          className={"h-full " + pctBarColor(clamped)}
          style={{ width: `${clamped}%` }}
        />
      </div>
      {sub && <div className="text-[10px] text-slate-600">{sub}</div>}
    </div>
  );
}

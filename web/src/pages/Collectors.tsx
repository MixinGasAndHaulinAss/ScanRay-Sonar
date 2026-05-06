// Collectors — list, manage, and enroll remote sonar-collector agents.
//
// The page is split into four panels: a "How collectors work" docs
// strip at the top (collapsible), a "New enrollment token" issuance
// flow with prominent install-command display, an "Outstanding tokens"
// table, and the legacy "Enrolled collectors" table with inline
// rename / deactivate / delete actions.
//
// Data shape mirrors the agent enrollment flow: createCollectorEnrollment
// Token returns a one-shot token plus a `install` bundle (enrollCmd,
// runCmd, composeFile) that the operator pastes on the target host.
//
// We keep all of this on a single page rather than nesting under a site
// because collectors are normally a tiny inventory (one or two per
// site) and operators want everything in one glance.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";
import { ApiError, api } from "../api/client";
import type { Site } from "../api/types";
import { formatRelative } from "../lib/format";

interface CollectorRow {
  id: string;
  siteId: string;
  name: string;
  hostname: string;
  collectorVersion: string;
  lastSeenAt?: string | null;
  isActive: boolean;
  createdAt: string;
}

interface CollectorTokenRow {
  id: string;
  siteId: string;
  label: string;
  maxUses: number;
  usedCount: number;
  expiresAt: string;
  revokedAt?: string | null;
  createdAt: string;
  isValid: boolean;
}

interface CollectorInstallBundle {
  image: string;
  enrollCmd: string;
  runCmd: string;
  composeFile: string;
}

interface NewCollectorToken {
  id: string;
  siteId: string;
  label: string;
  token: string;
  expiresAt: string;
  maxUses: number;
  installCmd: string;
  install: CollectorInstallBundle;
}

interface TokenForm {
  siteId: string;
  label: string;
  ttlHours: number;
  maxUses: number;
}

const EMPTY_TOKEN_FORM: TokenForm = {
  siteId: "",
  label: "",
  ttlHours: 72,
  maxUses: 1,
};

export default function Collectors() {
  const qc = useQueryClient();
  const sites = useQuery({ queryKey: ["sites"], queryFn: () => api.get<Site[]>("/sites") });
  const collectors = useQuery({
    queryKey: ["collectors"],
    queryFn: () => api.get<CollectorRow[]>("/collectors"),
  });
  const tokens = useQuery({
    queryKey: ["collector-enrollment-tokens"],
    queryFn: () => api.get<CollectorTokenRow[]>("/collectors/enrollment-tokens"),
  });

  const [showDocs, setShowDocs] = useState(false);
  const [open, setOpen] = useState(false);
  const [form, setForm] = useState<TokenForm>(EMPTY_TOKEN_FORM);
  const [err, setErr] = useState<string | null>(null);
  const [issued, setIssued] = useState<NewCollectorToken | null>(null);
  const [issuedTab, setIssuedTab] = useState<"enroll" | "run" | "compose">("enroll");
  const [copied, setCopied] = useState<string | null>(null);

  useEffect(() => {
    if (!sites.data) return;
    setForm((f) => (f.siteId ? f : { ...f, siteId: sites.data?.[0]?.id ?? "" }));
  }, [sites.data]);

  const create = useMutation({
    mutationFn: (b: TokenForm) =>
      api.post<NewCollectorToken>("/collectors/enrollment-tokens", b),
    onSuccess: (t) => {
      qc.invalidateQueries({ queryKey: ["collector-enrollment-tokens"] });
      setIssued(t);
      setIssuedTab("enroll");
      setOpen(false);
    },
    onError: (e: unknown) =>
      setErr(e instanceof ApiError ? e.message : "Failed to issue token"),
  });

  const revokeToken = useMutation({
    mutationFn: (id: string) => api.del<void>(`/collectors/enrollment-tokens/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["collector-enrollment-tokens"] }),
  });

  const patchCollector = useMutation({
    mutationFn: (vars: { id: string; body: { name?: string; isActive?: boolean } }) =>
      api.patch<CollectorRow>(`/collectors/${vars.id}`, vars.body),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["collectors"] }),
  });

  const deleteCollector = useMutation({
    mutationFn: (id: string) => api.del<void>(`/collectors/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["collectors"] }),
  });

  const siteName = (id: string) => sites.data?.find((s) => s.id === id)?.name ?? id.slice(0, 8);

  const issuedSnippet = useMemo(() => {
    if (!issued) return "";
    if (issuedTab === "enroll") return issued.install.enrollCmd;
    if (issuedTab === "run") return issued.install.runCmd;
    return issued.install.composeFile;
  }, [issued, issuedTab]);

  return (
    <div className="space-y-6">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h2 className="text-2xl font-semibold tracking-tight">Collectors</h2>
          <p className="mt-0.5 text-xs text-slate-500">
            Remote SNMP/discovery agents enrolled per site. Mint a token here, paste the install
            command on a Linux host with Docker, and the collector pulls jobs over an outbound
            websocket.
          </p>
        </div>
        <div className="flex items-center gap-2">
          <button
            onClick={() => setShowDocs((v) => !v)}
            className="rounded-md border border-ink-700 px-3 py-1.5 text-xs text-slate-300 hover:bg-ink-800"
          >
            {showDocs ? "Hide docs" : "How collectors work"}
          </button>
          <button
            onClick={() => {
              setForm({ ...EMPTY_TOKEN_FORM, siteId: sites.data?.[0]?.id ?? "" });
              setErr(null);
              setOpen(true);
            }}
            disabled={!sites.data?.length}
            className="rounded-md bg-sonar-600 px-3 py-1.5 text-sm font-medium hover:bg-sonar-500 disabled:opacity-40"
          >
            Add collector
          </button>
        </div>
      </div>

      {showDocs && <DocsPanel />}

      {issued && (
        <div className="rounded-xl border border-emerald-700 bg-emerald-950/40 p-4">
          <div className="flex items-center justify-between gap-3">
            <div className="text-sm font-medium text-emerald-200">
              New collector token — copy the install steps now (token shown only once)
            </div>
            <button
              onClick={() => setIssued(null)}
              className="text-xs text-emerald-300 hover:underline"
            >
              Dismiss
            </button>
          </div>

          <div className="mt-3 grid gap-3 text-xs text-emerald-200 md:grid-cols-2">
            <div>
              <div className="text-[10px] uppercase tracking-wide text-emerald-400">Site</div>
              <div className="font-medium">{siteName(issued.siteId)}</div>
            </div>
            <div>
              <div className="text-[10px] uppercase tracking-wide text-emerald-400">Image</div>
              <code className="font-mono text-[11px]">{issued.install.image}</code>
            </div>
            <div>
              <div className="text-[10px] uppercase tracking-wide text-emerald-400">Expires</div>
              <div>
                {new Date(issued.expiresAt).toLocaleString()} ·{" "}
                {issued.maxUses === 1 ? "single-use" : `${issued.maxUses} uses`}
              </div>
            </div>
            <div>
              <div className="text-[10px] uppercase tracking-wide text-emerald-400">
                Token (raw)
              </div>
              <code className="break-all font-mono text-[11px]">{issued.token}</code>
            </div>
          </div>

          <div className="mt-3 flex flex-wrap items-center gap-1 text-xs">
            {(["enroll", "run", "compose"] as const).map((tab) => (
              <button
                key={tab}
                onClick={() => {
                  setIssuedTab(tab);
                  setCopied(null);
                }}
                className={
                  "rounded-md px-3 py-1 font-medium " +
                  (issuedTab === tab
                    ? "bg-emerald-900/60 text-emerald-100"
                    : "border border-emerald-800/60 text-emerald-300 hover:bg-emerald-900/30")
                }
              >
                {tab === "enroll"
                  ? "1. Enroll (one-shot)"
                  : tab === "run"
                    ? "2. Run (long-running)"
                    : "Or: docker-compose"}
              </button>
            ))}
          </div>

          <pre className="mt-2 max-h-64 overflow-auto whitespace-pre-wrap break-all rounded bg-ink-950 p-3 font-mono text-xs text-emerald-100">
            {issuedSnippet}
          </pre>

          <div className="mt-2 flex flex-wrap items-center gap-3 text-xs text-emerald-300">
            <button
              onClick={() => {
                navigator.clipboard.writeText(issuedSnippet);
                setCopied(issuedTab);
                setTimeout(() => setCopied(null), 1500);
              }}
              className="rounded-md border border-emerald-700 px-2 py-1 text-emerald-200 hover:bg-emerald-900/40"
            >
              {copied === issuedTab ? "Copied!" : "Copy"}
            </button>
            <span className="text-[11px] text-emerald-400/80">
              {issuedTab === "enroll" &&
                "Sets SONAR_MASTER_KEY in your shell first, then run this to write /etc/sonar/collector.json into the docker volume."}
              {issuedTab === "run" &&
                "Run after enrollment succeeds. Container reconnects automatically; auto-restarts on crash."}
              {issuedTab === "compose" &&
                "Drop into a directory next to a .env that exports SONAR_MASTER_KEY. Run docker compose run --rm sonar-collector enroll … once, then docker compose up -d."}
            </span>
          </div>
        </div>
      )}

      <section className="space-y-2">
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
                        onClick={() => {
                          if (confirm(`Revoke token "${t.label}"?`)) revokeToken.mutate(t.id);
                        }}
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
      </section>

      <section className="space-y-2">
        <h3 className="text-sm font-semibold uppercase tracking-wide text-slate-400">
          Enrolled collectors
        </h3>
        {collectors.isLoading && <p className="text-sm text-slate-500">Loading…</p>}
        {collectors.error && (
          <p className="text-sm text-red-400">Could not load collectors.</p>
        )}
        {collectors.data && (
          <div className="overflow-hidden rounded-xl border border-ink-800 bg-ink-900 shadow-sm">
            <table className="w-full text-left text-sm">
              <thead className="bg-ink-800/60 text-xs uppercase tracking-wide text-slate-400">
                <tr>
                  <th className="px-4 py-2">Name</th>
                  <th className="px-4 py-2">Site</th>
                  <th className="px-4 py-2">Hostname</th>
                  <th className="px-4 py-2">Version</th>
                  <th className="px-4 py-2">Last seen</th>
                  <th className="px-4 py-2">Active</th>
                  <th className="px-4 py-2 text-right">Actions</th>
                </tr>
              </thead>
              <tbody>
                {collectors.data.map((c) => (
                  <tr key={c.id} className="border-t border-ink-800 hover:bg-ink-800/30">
                    <td className="px-4 py-2 font-medium text-slate-100">{c.name}</td>
                    <td className="px-4 py-2 text-slate-400">{siteName(c.siteId)}</td>
                    <td className="px-4 py-2 font-mono text-xs text-slate-400">{c.hostname}</td>
                    <td className="px-4 py-2 text-slate-400">{c.collectorVersion || "—"}</td>
                    <td className="px-4 py-2 text-slate-500">
                      {c.lastSeenAt ? formatRelative(c.lastSeenAt) : "never"}
                    </td>
                    <td className="px-4 py-2">
                      <span
                        className={
                          c.isActive
                            ? "rounded-full bg-emerald-950/50 px-2 py-0.5 text-[10px] uppercase text-emerald-300"
                            : "rounded-full bg-slate-800 px-2 py-0.5 text-[10px] uppercase text-slate-500"
                        }
                      >
                        {c.isActive ? "yes" : "no"}
                      </span>
                    </td>
                    <td className="px-4 py-2 text-right">
                      <div className="flex justify-end gap-1">
                        <button
                          onClick={() => {
                            const next = prompt("Rename collector", c.name);
                            if (next && next !== c.name)
                              patchCollector.mutate({ id: c.id, body: { name: next } });
                          }}
                          className="rounded-md border border-ink-700 px-2 py-1 text-xs text-slate-300 hover:bg-ink-800"
                        >
                          Rename
                        </button>
                        <button
                          onClick={() =>
                            patchCollector.mutate({
                              id: c.id,
                              body: { isActive: !c.isActive },
                            })
                          }
                          className="rounded-md border border-ink-700 px-2 py-1 text-xs text-slate-300 hover:bg-ink-800"
                        >
                          {c.isActive ? "Deactivate" : "Reactivate"}
                        </button>
                        <button
                          onClick={() => {
                            if (confirm(`Permanently delete collector "${c.name}"?`))
                              deleteCollector.mutate(c.id);
                          }}
                          className="rounded-md border border-ink-700 px-2 py-1 text-xs text-red-300 hover:bg-red-900/30"
                        >
                          Delete
                        </button>
                      </div>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
            {collectors.data.length === 0 && (
              <div className="px-4 py-8 text-center text-sm text-slate-500">
                No collectors enrolled yet. Click "Add collector" to mint your first token.
              </div>
            )}
          </div>
        )}
      </section>

      {open && (
        <div className="fixed inset-0 z-20 grid place-items-center bg-black/60 px-4">
          <form
            className="w-full max-w-md space-y-3 rounded-xl border border-ink-800 bg-ink-900 p-5"
            onSubmit={(e) => {
              e.preventDefault();
              create.mutate(form);
            }}
          >
            <h3 className="text-lg font-semibold">Issue collector enrollment token</h3>
            <p className="text-xs text-slate-400">
              Mints a single-use token bound to one site. After issuance, copy the install
              steps (Enroll, then Run) onto the target Linux host with Docker installed and the
              same <code className="font-mono">SONAR_MASTER_KEY</code> exported in the shell.
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
              Label / collector name
              <input
                className="mt-1 w-full rounded-md border border-ink-700 bg-ink-950 px-3 py-2 text-sm"
                value={form.label}
                onChange={(e) => setForm({ ...form, label: e.target.value })}
                placeholder="e.g. site-a-collector"
              />
              <span className="mt-1 block text-[10px] text-slate-500">
                Becomes the collector display name. Defaults to "collector".
              </span>
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
                  max={50}
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
                disabled={create.isPending || !form.siteId}
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

function DocsPanel() {
  return (
    <div className="space-y-4 rounded-xl border border-ink-800 bg-ink-900/60 p-5 text-sm text-slate-300">
      <div>
        <h3 className="text-base font-semibold text-slate-100">How collectors work</h3>
        <p className="mt-1 text-xs text-slate-400">
          A <code className="font-mono">sonar-collector</code> is a tiny Go daemon you run on a
          Linux host inside a remote site. It connects outbound to the central Sonar API over
          HTTPS/WSS — no inbound firewall holes are required.
        </p>
      </div>

      <div className="grid gap-4 md:grid-cols-2">
        <DocCard title="1. Provision a token">
          Click <em>Add collector</em>, pick the site it should belong to, and submit. The API
          mints a <strong>single-use token</strong> with a TTL (default 72h) and returns
          ready-to-paste install commands.
        </DocCard>

        <DocCard title="2. Run the install commands">
          On the collector host (with Docker installed), <em>export</em>
          <code className="font-mono"> SONAR_MASTER_KEY</code> to the same value as your
          central Sonar, then run the <strong>Enroll</strong> step and the <strong>Run</strong>{" "}
          step in order. Or drop the supplied <code className="font-mono">docker-compose.yml</code>
          next to a <code className="font-mono">.env</code>.
        </DocCard>

        <DocCard title="3. Jobs flow over an outbound websocket">
          The collector authenticates via the JWT it earned at enrollment, registers as online,
          and pulls work — currently SNMP polling and IP/subnet discovery sweeps. Results stream
          back over the same connection.
        </DocCard>

        <DocCard title="4. Operate from this page">
          Watch the <em>Last seen</em> column to confirm the collector is online (touched every
          minute). Rename if you misnamed it, deactivate to stop it claiming new jobs, or delete
          to remove it from the inventory entirely.
        </DocCard>
      </div>

      <div className="space-y-2 rounded-md border border-ink-800 bg-ink-950/40 p-3 text-xs">
        <div className="font-semibold text-slate-200">Network requirements</div>
        <ul className="list-inside list-disc space-y-0.5 text-slate-400">
          <li>
            Outbound HTTPS to the central Sonar (the <code className="font-mono">--base</code>{" "}
            URL printed in the install command).
          </li>
          <li>
            Outbound websocket (<code className="font-mono">wss://…/collector/ws</code>) for
            job and result streaming.
          </li>
          <li>
            Inbound access from the collector to the network gear it polls (SNMP/UDP 161, SSH/22,
            ICMP, etc).
          </li>
          <li>
            <strong>SONAR_MASTER_KEY</strong> must match central Sonar so the collector can
            decrypt the SNMP/SSH credentials it pulls down.
          </li>
        </ul>
      </div>

      <div className="space-y-2 rounded-md border border-ink-800 bg-ink-950/40 p-3 text-xs">
        <div className="font-semibold text-slate-200">Tokens vs. collectors</div>
        <p className="text-slate-400">
          An <em>enrollment token</em> is a short-lived secret that lets a host claim a
          collector identity. Once consumed, the collector exists in the bottom table with a
          permanent <code className="font-mono">collectorId</code> and its own JWT. Tokens are
          listed in the middle table — revoke any you no longer plan to redeem.
        </p>
      </div>
    </div>
  );
}

function DocCard({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="rounded-md border border-ink-800 bg-ink-950/40 p-3">
      <div className="text-xs font-semibold uppercase tracking-wide text-sonar-300">{title}</div>
      <div className="mt-1 text-xs leading-relaxed text-slate-400">{children}</div>
    </div>
  );
}

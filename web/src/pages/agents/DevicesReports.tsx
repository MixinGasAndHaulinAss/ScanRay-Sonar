// DevicesReports — canned historical/fleet lists + agent Markdown report generate.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { Link } from "react-router-dom";
import { api } from "../../api/client";
import type {
  Agent,
  OverviewDevicesAveragesResponse,
  OverviewDevicesPerformanceResponse,
  OverviewUserExperienceResponse,
  Site,
} from "../../api/types";
import { formatRelative } from "../../lib/format";
import { Card, EmptyHint, ErrorHint } from "./common";

const AGENT_TEMPLATES = [
  { slug: "agent-fleet-summary", title: "Agent fleet summary" },
  { slug: "agent-compliance", title: "Agent compliance posture" },
  { slug: "agent-patches", title: "Missing patches by severity" },
];

export default function DevicesReports() {
  const qc = useQueryClient();
  const sites = useQuery({ queryKey: ["sites"], queryFn: () => api.get<Site[]>("/sites") });
  const [reportSite, setReportSite] = useState("");
  const [reportSlug, setReportSlug] = useState("agent-compliance");
  const generate = useMutation({
    mutationFn: () =>
      api.post<{ id: number }>("/reports", { templateSlug: reportSlug, siteId: reportSite }),
    onSuccess: async (r) => {
      await qc.invalidateQueries({ queryKey: ["reports"] });
      window.open(`/api/v1/reports/${r.id}/download`, "_blank");
    },
  });

  const agents = useQuery({
    queryKey: ["agents"],
    queryFn: () => api.get<Agent[]>("/agents"),
    refetchInterval: 60_000,
  });
  const avg = useQuery({
    queryKey: ["overview", "devices-averages"],
    queryFn: () => api.get<OverviewDevicesAveragesResponse>("/agents/overview/devices-averages"),
    refetchInterval: 60_000,
  });
  const perf = useQuery({
    queryKey: ["overview", "devices-performance"],
    queryFn: () =>
      api.get<OverviewDevicesPerformanceResponse>("/agents/overview/devices-performance"),
    refetchInterval: 60_000,
  });
  const ux = useQuery({
    queryKey: ["overview", "user-experience"],
    queryFn: () => api.get<OverviewUserExperienceResponse>("/agents/overview/user-experience"),
    refetchInterval: 60_000,
  });

  if (agents.isLoading) return <EmptyHint>Loading reports…</EmptyHint>;
  if (agents.isError) return <ErrorHint>Failed to load agents.</ErrorHint>;

  const list = agents.data ?? [];
  const byVersion = new Map<string, number>();
  for (const a of list) {
    const v = a.agentVersion || "unknown";
    byVersion.set(v, (byVersion.get(v) ?? 0) + 1);
  }
  const versionRows = Array.from(byVersion.entries())
    .sort((a, b) => b[1] - a[1])
    .slice(0, 12);

  const offline = list
    .filter((a) => !a.lastSeenAt || Date.now() - new Date(a.lastSeenAt).getTime() >= 5 * 60_000)
    .sort((a, b) => {
      const ta = a.lastSeenAt ? new Date(a.lastSeenAt).getTime() : 0;
      const tb = b.lastSeenAt ? new Date(b.lastSeenAt).getTime() : 0;
      return ta - tb;
    })
    .slice(0, 25);

  const pendingReboot = list.filter((a) => a.pendingReboot).slice(0, 25);

  const maxVer = versionRows[0]?.[0];
  const behind =
    maxVer && maxVer !== "unknown"
      ? list.filter((a) => {
          const v = a.agentVersion || "unknown";
          return v !== "unknown" && v !== maxVer;
        })
      : [];

  return (
    <div className="space-y-4">
      <Card title="Generate agent report">
        <div className="flex flex-wrap items-end gap-3">
          <label className="text-xs text-slate-400">
            Site
            <select
              className="mt-1 block rounded border border-ink-700 bg-ink-900 px-2 py-1.5 text-sm text-slate-100"
              value={reportSite}
              onChange={(e) => setReportSite(e.target.value)}
            >
              <option value="">Select site…</option>
              {(sites.data ?? []).map((s) => (
                <option key={s.id} value={s.id}>
                  {s.name}
                </option>
              ))}
            </select>
          </label>
          <label className="text-xs text-slate-400">
            Template
            <select
              className="mt-1 block rounded border border-ink-700 bg-ink-900 px-2 py-1.5 text-sm text-slate-100"
              value={reportSlug}
              onChange={(e) => setReportSlug(e.target.value)}
            >
              {AGENT_TEMPLATES.map((t) => (
                <option key={t.slug} value={t.slug}>
                  {t.title}
                </option>
              ))}
            </select>
          </label>
          <button
            type="button"
            disabled={!reportSite || generate.isPending}
            onClick={() => generate.mutate()}
            className="rounded bg-sonar-600 px-3 py-1.5 text-sm text-white disabled:opacity-40"
          >
            {generate.isPending ? "Generating…" : "Generate & download"}
          </button>
          <Link to="/reports" className="text-xs text-sonar-400 hover:underline">
            All report templates
          </Link>
        </div>
        {generate.isError && <p className="mt-2 text-xs text-rose-400">Generate failed.</p>}
      </Card>

      <div className="grid gap-4 lg:grid-cols-2">
        <ReportCard title="Probe versions">
          <div className="mb-3 flex flex-wrap gap-3 text-sm">
            <span className="rounded bg-ink-800 px-2 py-1 text-slate-300">
              Fleet versions: <strong className="text-slate-100">{byVersion.size}</strong>
            </span>
            <span className="rounded bg-ink-800 px-2 py-1 text-slate-300">
              Behind latest in fleet:{" "}
              <strong className={behind.length ? "text-amber-300" : "text-emerald-300"}>
                {behind.length}
              </strong>
            </span>
            {maxVer ? (
              <span className="rounded bg-ink-800 px-2 py-1 text-slate-300">
                Top version: <strong className="font-mono text-slate-100">{maxVer}</strong>
              </span>
            ) : null}
          </div>
          <SimpleTable
            headers={["Version", "Count"]}
            rows={versionRows.map(([v, n]) => [v, String(n)])}
          />
        </ReportCard>

        <ReportCard title="Offline / stale (>5m)">
          <HostTable
            rows={offline.map((a) => ({
              id: a.id,
              hostname: a.hostname,
              detail: a.lastSeenAt ? formatRelative(a.lastSeenAt) : "never",
            }))}
          />
        </ReportCard>

        <ReportCard title="Pending reboot">
          <HostTable
            rows={pendingReboot.map((a) => ({
              id: a.id,
              hostname: a.hostname,
              detail: a.lastSeenAt ? formatRelative(a.lastSeenAt) : "—",
            }))}
          />
        </ReportCard>

        <ReportCard title="Overview snapshots">
          <ul className="space-y-1 text-sm text-slate-300">
            <li>Devices averages: {avg.data ? "loaded" : avg.isError ? "error" : "…"}</li>
            <li>Devices performance: {perf.data ? "loaded" : perf.isError ? "error" : "…"}</li>
            <li>User experience: {ux.data ? "loaded" : ux.isError ? "error" : "…"}</li>
          </ul>
        </ReportCard>
      </div>
    </div>
  );
}

function ReportCard({ title, children }: { title: string; children: React.ReactNode }) {
  return <Card title={title}>{children}</Card>;
}

function HostTable({ rows }: { rows: { id: string; hostname: string; detail: string }[] }) {
  if (rows.length === 0) return <p className="text-sm text-slate-500">None.</p>;
  return (
    <table className="w-full text-left text-sm">
      <tbody>
        {rows.map((r) => (
          <tr key={r.id} className="border-t border-ink-800/80">
            <td className="py-1.5">
              <Link className="text-sonar-300 hover:underline" to={`/agents/${r.id}`}>
                {r.hostname}
              </Link>
            </td>
            <td className="py-1.5 text-right tabular-nums text-slate-400">{r.detail}</td>
          </tr>
        ))}
      </tbody>
    </table>
  );
}

function SimpleTable({ headers, rows }: { headers: string[]; rows: string[][] }) {
  if (rows.length === 0) return <p className="text-sm text-slate-500">No data yet.</p>;
  return (
    <table className="w-full text-left text-sm">
      <thead>
        <tr className="text-xs uppercase tracking-wide text-slate-500">
          {headers.map((h) => (
            <th key={h} className="pb-1 font-medium">
              {h}
            </th>
          ))}
        </tr>
      </thead>
      <tbody>
        {rows.map((row, i) => (
          <tr key={i} className="border-t border-ink-800/80">
            {row.map((cell, j) => (
              <td
                key={j}
                className={
                  "py-1.5 " +
                  (j === row.length - 1 ? "text-right tabular-nums text-slate-400" : "text-slate-200")
                }
              >
                {cell}
              </td>
            ))}
          </tr>
        ))}
      </tbody>
    </table>
  );
}

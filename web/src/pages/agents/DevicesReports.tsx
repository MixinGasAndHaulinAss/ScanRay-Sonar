// DevicesReports — canned historical/fleet lists (ControlUp Reports tab).

import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { api } from "../../api/client";
import type {
  Agent,
  OverviewDevicesAveragesResponse,
  OverviewDevicesPerformanceResponse,
  OverviewUserExperienceResponse,
} from "../../api/types";
import { formatRelative } from "../../lib/format";
import { Card, EmptyHint, ErrorHint } from "./common";

export default function DevicesReports() {
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

  return (
    <div className="grid gap-4 lg:grid-cols-2">
      <ReportCard title="Probe versions">
        <SimpleTable
          headers={["Version", "Devices"]}
          rows={versionRows.map(([v, n]) => [v, String(n)])}
        />
      </ReportCard>

      <ReportCard title="Offline / stale devices">
        {offline.length === 0 ? (
          <p className="text-sm text-slate-500">All devices seen in the last 5 minutes.</p>
        ) : (
          <HostTable
            rows={offline.map((a) => ({
              id: a.id,
              hostname: a.hostname,
              detail: formatRelative(a.lastSeenAt),
            }))}
          />
        )}
      </ReportCard>

      <ReportCard title="Pending reboot">
        {pendingReboot.length === 0 ? (
          <p className="text-sm text-slate-500">No devices reporting a pending reboot.</p>
        ) : (
          <HostTable
            rows={pendingReboot.map((a) => ({
              id: a.id,
              hostname: a.hostname,
              detail: a.os,
            }))}
          />
        )}
      </ReportCard>

      <ReportCard title="Most BSODs (24h)">
        {avg.isError && <ErrorHint>Failed to load averages.</ErrorHint>}
        <HostTable
          rows={(avg.data?.top.mostBSODs ?? []).map((r) => ({
            id: r.id,
            hostname: r.hostname,
            detail: String(r.value),
          }))}
        />
      </ReportCard>

      <ReportCard title="Most app crashes (24h)">
        <HostTable
          rows={(avg.data?.top.mostAppCrashes ?? []).map((r) => ({
            id: r.id,
            hostname: r.hostname,
            detail: String(r.value),
          }))}
        />
      </ReportCard>

      <ReportCard title="Most missing patches">
        <HostTable
          rows={(avg.data?.top.mostMissingPatches ?? []).map((r) => ({
            id: r.id,
            hostname: r.hostname,
            detail: String(r.value),
          }))}
        />
      </ReportCard>

      <ReportCard title="Lowest experience score">
        <HostTable
          rows={(ux.data?.worst ?? []).slice(0, 15).map((r) => ({
            id: r.id,
            hostname: r.hostname,
            detail: r.score != null ? r.score.toFixed(1) : "—",
          }))}
        />
      </ReportCard>

      <ReportCard title="Bottom device models (score)">
        <SimpleTable
          headers={["Model", "Count", "Score"]}
          rows={(perf.data?.bottom5Models ?? []).map((r) => [
            r.model,
            String(r.count),
            r.score != null ? r.score.toFixed(1) : "—",
          ])}
        />
      </ReportCard>
    </div>
  );
}

function ReportCard({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <Card>
      <h3 className="mb-3 text-sm font-semibold text-slate-200">{title}</h3>
      {children}
    </Card>
  );
}

function HostTable({
  rows,
}: {
  rows: { id: string; hostname: string; detail: string }[];
}) {
  if (rows.length === 0) {
    return <p className="text-sm text-slate-500">No data yet.</p>;
  }
  return (
    <table className="w-full text-left text-sm">
      <tbody>
        {rows.map((r) => (
          <tr key={r.id + r.hostname} className="border-t border-ink-800/80">
            <td className="py-1.5 pr-2">
              <Link to={`/agents/${r.id}`} className="text-sonar-300 hover:underline">
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
                className={"py-1.5 " + (j === row.length - 1 ? "text-right tabular-nums text-slate-400" : "text-slate-200")}
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

// AgentDetail — ControlUp-style device drill-down with category tabs.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { ApiError, api } from "../api/client";
import type {
  AgentDetail,
  MetricSeries,
  NetworkSeries,
  Site,
  Snapshot,
  SnapshotConversation,
  SnapshotProcess,
  SnapshotService,
} from "../api/types";
import AgentNetworkGraphSection from "../components/AgentNetworkGraph";
import LineChart from "../components/LineChart";
import {
  formatBytes,
  formatDuration,
  formatPct,
  formatRelative,
  pctBarColor,
} from "../lib/format";

type DeviceTab =
  | "performance"
  | "processes"
  | "network"
  | "storage"
  | "apps"
  | "patches"
  | "stopped"
  | "services"
  | "sessions"
  | "topapps"
  | "eventlog"
  | "power";

type NetSub = "tcp" | "map" | "topology";

const TABS: { id: DeviceTab; label: string }[] = [
  { id: "performance", label: "Performance" },
  { id: "processes", label: "Active Processes" },
  { id: "network", label: "Network" },
  { id: "storage", label: "Storage" },
  { id: "apps", label: "Installed Applications" },
  { id: "patches", label: "Missing Patches" },
  { id: "stopped", label: "Stopped Processes" },
  { id: "services", label: "Services" },
  { id: "sessions", label: "Sessions" },
  { id: "topapps", label: "Top Apps" },
  { id: "eventlog", label: "Windows Event Log" },
  { id: "power", label: "Power Events" },
];

const TAB_KEY = "sonar.device.tab";

export default function AgentDetailPage() {
  const { id = "" } = useParams<{ id: string }>();
  const qc = useQueryClient();
  const [tab, setTab] = useState<DeviceTab>(() => {
    try {
      const v = localStorage.getItem(TAB_KEY);
      if (TABS.some((t) => t.id === v)) return v as DeviceTab;
    } catch {
      /* ignore */
    }
    return "performance";
  });
  const [paused, setPaused] = useState(false);
  const [netSub, setNetSub] = useState<NetSub>("tcp");
  const [procSearch, setProcSearch] = useState("");
  const [connSearch, setConnSearch] = useState("");
  const [compareAvg, setCompareAvg] = useState(false);

  useEffect(() => {
    try {
      localStorage.setItem(TAB_KEY, tab);
    } catch {
      /* ignore */
    }
  }, [tab]);

  const refreshMs = paused ? false : 15_000;

  const agent = useQuery({
    queryKey: ["agent", id],
    queryFn: () => api.get<AgentDetail>(`/agents/${id}`),
    refetchInterval: refreshMs,
    enabled: !!id,
  });
  const metrics = useQuery({
    queryKey: ["agent-metrics", id, "24h"],
    queryFn: () => api.get<MetricSeries>(`/agents/${id}/metrics?range=24h`),
    refetchInterval: paused ? false : 60_000,
    enabled: !!id,
  });
  const network = useQuery({
    queryKey: ["agent-network", id, "24h"],
    queryFn: () => api.get<NetworkSeries>(`/agents/${id}/network?range=24h`),
    refetchInterval: paused ? false : 60_000,
    enabled: !!id,
  });
  const sites = useQuery({ queryKey: ["sites"], queryFn: () => api.get<Site[]>("/sites") });
  const allAgents = useQuery({
    queryKey: ["agents"],
    queryFn: () => api.get<AgentDetail[]>("/agents"),
  });

  const updateTags = useMutation({
    mutationFn: (tags: string[]) => api.patch<AgentDetail>(`/agents/${id}`, { tags }),
    onSuccess: (updated) => {
      qc.setQueryData(["agent", id], (prev: AgentDetail | undefined) =>
        prev ? { ...prev, tags: updated.tags } : prev,
      );
      qc.invalidateQueries({ queryKey: ["agents"] });
    },
  });

  const snap = agent.data?.lastMetrics ?? null;
  const online =
    !!agent.data?.lastSeenAt &&
    Date.now() - new Date(agent.data.lastSeenAt).getTime() < 5 * 60_000;

  const fleetAvg = useMemo(() => {
    const list = allAgents.data ?? [];
    let cpu = 0,
      mem = 0,
      nCpu = 0,
      nMem = 0;
    for (const a of list) {
      if (a.cpuPct != null) {
        cpu += a.cpuPct;
        nCpu++;
      }
      if (a.memUsedBytes != null && a.memTotalBytes && a.memTotalBytes > 0) {
        mem += (Number(a.memUsedBytes) / Number(a.memTotalBytes)) * 100;
        nMem++;
      }
    }
    return {
      cpu: nCpu ? cpu / nCpu : null,
      mem: nMem ? mem / nMem : null,
    };
  }, [allAgents.data]);

  if (agent.isLoading) {
    return <div className="text-sm text-slate-400">Loading device…</div>;
  }
  if (agent.isError || !agent.data) {
    return (
      <div className="space-y-3">
        <Link to="/agents" className="text-sm text-sonar-400 hover:underline">
          ← Back to devices
        </Link>
        <div className="rounded-md border border-red-800/60 bg-red-950/40 p-4 text-sm text-red-200">
          Could not load device: {(agent.error as Error)?.message ?? "unknown error"}
        </div>
      </div>
    );
  }

  const a = agent.data;
  const siteName = sites.data?.find((s) => s.id === a.siteId)?.name ?? a.siteId.slice(0, 8);

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div className="min-w-0 space-y-1">
          <Link to="/agents" className="text-xs text-sonar-400 hover:underline">
            ← Devices
          </Link>
          <h2 className="text-2xl font-semibold tracking-tight">{a.hostname}</h2>
          <p className="text-sm text-slate-400">
            {siteName} · {a.os} {a.osVersion} · probe {a.agentVersion || "?"}
          </p>
          <TagEditor
            tags={a.tags ?? []}
            suggestions={Array.from(
              new Set((allAgents.data ?? []).flatMap((x) => x.tags ?? [])),
            ).sort()}
            saving={updateTags.isPending}
            error={
              updateTags.isError
                ? updateTags.error instanceof ApiError
                  ? updateTags.error.message
                  : "Failed to save tags"
                : null
            }
            onChange={(next) => updateTags.mutate(next)}
          />
        </div>
        <div className="flex items-center gap-2">
          <span
            className={
              online
                ? "inline-flex items-center gap-1.5 rounded-full bg-emerald-900/40 px-2.5 py-1 text-xs text-emerald-300"
                : "rounded-full bg-slate-800 px-2.5 py-1 text-xs text-slate-400"
            }
          >
            {online && <span className="h-1.5 w-1.5 rounded-full bg-emerald-400" />}
            {online ? "Connected" : "Offline"}
          </span>
          <button
            type="button"
            onClick={() => setPaused((p) => !p)}
            className="inline-flex items-center gap-1.5 rounded-md bg-sonar-600 px-3 py-1.5 text-xs font-medium text-white hover:bg-sonar-500"
          >
            {paused ? "Resume Auto Refresh" : "Pause Auto Refresh"}
          </button>
        </div>
      </div>

      <div className="flex gap-1 overflow-x-auto border-b border-ink-800 pb-px">
        {TABS.map((t) => (
          <button
            key={t.id}
            type="button"
            onClick={() => setTab(t.id)}
            className={
              "whitespace-nowrap px-3 py-2 text-sm font-medium transition " +
              (tab === t.id
                ? "border-b-2 border-sonar-400 text-sonar-200"
                : "border-b-2 border-transparent text-slate-400 hover:text-slate-200")
            }
          >
            {t.label}
          </button>
        ))}
      </div>

      {!snap && (
        <div className="rounded-lg border border-ink-800 bg-ink-900/50 p-6 text-sm text-slate-400">
          No telemetry yet — waiting for the probe to send its first snapshot.
        </div>
      )}

      {snap && tab === "performance" && (
        <PerformanceTab
          agent={a}
          snap={snap}
          metrics={metrics.data}
          network={network.data}
          compareAvg={compareAvg}
          onCompareAvg={setCompareAvg}
          fleetAvg={fleetAvg}
        />
      )}
      {snap && tab === "processes" && (
        <ProcessesTab
          snap={snap}
          search={procSearch}
          onSearch={setProcSearch}
        />
      )}
      {snap && tab === "network" && (
        <NetworkTab
          id={id}
          snap={snap}
          netSub={netSub}
          onNetSub={setNetSub}
          search={connSearch}
          onSearch={setConnSearch}
          agent={a}
        />
      )}
      {snap && tab === "storage" && <StorageTab snap={snap} />}
      {snap && tab === "services" && <ServicesTab snap={snap} />}
      {snap && tab === "sessions" && <SessionsTab snap={snap} />}
      {snap && tab === "patches" && <PatchesTab snap={snap} />}
      {snap && tab === "apps" && <InstalledAppsTab snap={snap} />}
      {snap && tab === "stopped" && <StoppedProcessesTab snap={snap} />}
      {snap && tab === "topapps" && <TopAppsTab snap={snap} />}
      {snap && tab === "eventlog" && <EventLogTab snap={snap} />}
      {snap && tab === "power" && <PowerEventsTab snap={snap} />}
    </div>
  );
}

function PerformanceTab({
  agent,
  snap,
  metrics,
  network,
  compareAvg,
  onCompareAvg,
  fleetAvg,
}: {
  agent: AgentDetail;
  snap: Snapshot;
  metrics?: MetricSeries;
  network?: NetworkSeries;
  compareAvg: boolean;
  onCompareAvg: (v: boolean) => void;
  fleetAvg: { cpu: number | null; mem: number | null };
}) {
  const times = useMemo(
    () => (metrics?.samples ?? []).map((s) => s.time),
    [metrics],
  );
  const cpuVals = useMemo(
    () => (metrics?.samples ?? []).map((s) => Number(s.cpuPct ?? 0)),
    [metrics],
  );
  const memGb = useMemo(
    () =>
      (metrics?.samples ?? []).map((s) => Number(s.memUsedBytes ?? 0) / (1024 * 1024 * 1024)),
    [metrics],
  );
  const memTotalGb = snap.memory.totalBytes / (1024 * 1024 * 1024);
  const netTimes = useMemo(
    () => (network?.samples ?? []).map((s) => s.time),
    [network],
  );
  const netIn = useMemo(
    () => (network?.samples ?? []).map((s) => Number(s.inBps ?? 0) / (1024 * 1024)),
    [network],
  );
  const netOut = useMemo(
    () => (network?.samples ?? []).map((s) => Number(s.outBps ?? 0) / (1024 * 1024)),
    [network],
  );

  const scoreSeries = useMemo(() => {
    return cpuVals.map((cpu, i) => {
      const memPct =
        metrics?.samples[i]?.memTotalBytes && Number(metrics.samples[i].memTotalBytes) > 0
          ? (Number(metrics.samples[i].memUsedBytes ?? 0) /
              Number(metrics.samples[i].memTotalBytes)) *
            100
          : 0;
      // Lightweight 0–10 score proxy matching score.go spirit.
      let s = 10;
      if (cpu > 90) s -= 3;
      else if (cpu > 70) s -= 1.5;
      if (memPct > 90) s -= 2;
      else if (memPct > 75) s -= 1;
      return Math.max(0, Math.min(10, s));
    });
  }, [cpuVals, metrics]);

  const root = snap.disks.find((d) => d.mountpoint === "/" || d.mountpoint === "C:\\") ?? snap.disks[0];
  const hw = snap.hardware;
  const consoleUser = snap.loggedInUsers.find((u) => u.state === "Active")?.user
    ?? snap.loggedInUsers[0]?.user
    ?? "—";

  return (
    <div className="space-y-4">
      <div className="grid gap-3 lg:grid-cols-5">
        <MetaCard title="Location">
          {agent.geoLat != null && agent.geoLon != null ? (
            <p className="text-sm text-slate-300">
              {[agent.geoCity, agent.geoSubdivision, agent.geoCountryName].filter(Boolean).join(", ") ||
                "Mapped"}
              <br />
              <span className="text-xs text-slate-500">
                {agent.geoLat.toFixed(3)}, {agent.geoLon.toFixed(3)}
              </span>
            </p>
          ) : (
            <p className="text-sm text-slate-500">No GeoIP yet</p>
          )}
        </MetaCard>
        <MetaCard title="General">
          <KV label="Enrolled" value={formatRelative(agent.enrolledAt)} />
          <KV label="Last communication" value={formatRelative(agent.lastSeenAt)} />
          <KV label="Console user" value={consoleUser} />
          <KV label="Remote IP" value={agent.publicIp || "—"} />
          <KV label="ISP" value={agent.geoOrg || snap.health?.ispName || "—"} />
          <KV label="Agent version" value={agent.agentVersion || "—"} />
        </MetaCard>
        <MetaCard title="Operating System">
          <KV label="OS" value={`${snap.host.platform} ${snap.host.platformVersion}`} />
          <KV label="Architecture" value={snap.host.kernelArch} />
          <KV label="Kernel" value={snap.host.kernelVersion} />
          {root && (
            <div className="mt-2 space-y-1">
              <div className="flex justify-between text-[11px] text-slate-400">
                <span>Drive {root.mountpoint}</span>
                <span>
                  {formatBytes(root.freeBytes)} free / {formatBytes(root.totalBytes)}
                </span>
              </div>
              <div className="h-1.5 overflow-hidden rounded bg-ink-800">
                <div
                  className={"h-full " + pctBarColor(root.usedPct)}
                  style={{ width: `${Math.min(100, root.usedPct)}%` }}
                />
              </div>
            </div>
          )}
        </MetaCard>
        <MetaCard title="Hardware">
          <KV label="Manufacturer" value={hw?.system?.manufacturer || "—"} />
          <KV label="Model" value={hw?.system?.productName || "—"} />
          <KV label="Serial" value={hw?.system?.serialNumber || "—"} />
          <KV
            label="CPU"
            value={snap.cpu.model || hw?.cpu?.model || "—"}
          />
          <KV
            label="Cores"
            value={`${snap.cpu.cores} phys / ${snap.cpu.logicalCpus} logical`}
          />
          <KV label="Memory" value={formatBytes(snap.memory.totalBytes)} />
          <KV
            label="Battery health"
            value={
              snap.health?.batteryHealthPct != null
                ? formatPct(snap.health.batteryHealthPct)
                : "—"
            }
          />
        </MetaCard>
        <MetaCard title="Network">
          {snap.nics
            .filter((n) => n.kind !== "loopback" && n.up)
            .slice(0, 4)
            .map((n) => (
              <div key={n.name} className="mb-2 border-b border-ink-800/60 pb-2 last:mb-0 last:border-0 last:pb-0">
                <div className="text-xs font-medium text-slate-200">{n.name}</div>
                <div className="text-[11px] text-slate-500">
                  {n.kind || "adapter"} · {n.mac || "no MAC"}
                </div>
                <div className="font-mono text-[11px] text-slate-400">
                  {(n.addresses ?? []).join(", ") || "—"}
                </div>
              </div>
            ))}
          {snap.nics.filter((n) => n.kind !== "loopback" && n.up).length === 0 && (
            <p className="text-sm text-slate-500">No active adapters</p>
          )}
        </MetaCard>
      </div>

      <div className="flex flex-wrap items-center gap-3 text-xs text-slate-400">
        <label className="flex items-center gap-1.5">
          <input
            type="checkbox"
            checked={compareAvg}
            onChange={(e) => onCompareAvg(e.target.checked)}
          />
          Compare with averages
        </label>
        {compareAvg && (
          <span className="text-slate-500">
            Fleet avg CPU {fleetAvg.cpu != null ? formatPct(fleetAvg.cpu) : "—"} · Mem{" "}
            {fleetAvg.mem != null ? formatPct(fleetAvg.mem) : "—"}
          </span>
        )}
        <span className="ml-auto text-slate-500">Last 24h</span>
      </div>

      <div className="grid gap-3 md:grid-cols-2">
        <ChartCard title="Device Score">
          <LineChart
            times={times}
            series={[{ label: "Score", values: scoreSeries, color: "stroke-sky-400 text-sky-400" }]}
            height={180}
            yMin={0}
            yMax={10}
          />
        </ChartCard>
        <ChartCard title="CPU Usage (%)">
          <LineChart
            times={times}
            series={[
              { label: "CPU", values: cpuVals, color: "stroke-emerald-400 text-emerald-400" },
              ...(compareAvg && fleetAvg.cpu != null
                ? [
                    {
                      label: "Fleet avg",
                      values: cpuVals.map(() => fleetAvg.cpu),
                      color: "stroke-slate-500 text-slate-500",
                    },
                  ]
                : []),
            ]}
            height={180}
            yMin={0}
            yMax={100}
            yUnit="%"
          />
        </ChartCard>
        <ChartCard title="CPU Queue Length">
          <LineChart
            times={times}
            series={[
              {
                label: "Queue",
                values: times.map(() => snap.health?.cpuQueueLength ?? 0),
                color: "stroke-sky-400 text-sky-400",
              },
            ]}
            height={180}
            yMin={0}
          />
          <p className="mt-1 text-[10px] text-slate-600">
            Current sample: {snap.health?.cpuQueueLength ?? "—"} (history requires denser health ingest)
          </p>
        </ChartCard>
        <ChartCard title="Memory Usage (GB)">
          <LineChart
            times={times}
            series={[
              { label: "In use", values: memGb, color: "stroke-sky-400 text-sky-400" },
              {
                label: "Total",
                values: memGb.map(() => memTotalGb),
                color: "stroke-slate-600 text-slate-600",
              },
            ]}
            height={180}
            yMin={0}
            yMax={Math.max(memTotalGb * 1.05, 1)}
            yUnit="GB"
          />
        </ChartCard>
        <ChartCard title="Disk Queue Length">
          <LineChart
            times={times}
            series={[
              {
                label: "Queue",
                values: times.map(() => snap.health?.diskQueueLength ?? 0),
                color: "stroke-rose-400 text-rose-400",
              },
            ]}
            height={180}
            yMin={0}
          />
          <p className="mt-1 text-[10px] text-slate-600">
            Current sample: {snap.health?.diskQueueLength ?? "—"}
          </p>
        </ChartCard>
        <ChartCard title="Network Usage (MB/s)">
          <LineChart
            times={netTimes}
            series={[
              { label: "Received", values: netIn, color: "stroke-emerald-300 text-emerald-300" },
              { label: "Sent", values: netOut, color: "stroke-emerald-600 text-emerald-600" },
            ]}
            height={180}
            yMin={0}
            yUnit="MB/s"
          />
        </ChartCard>
      </div>

      {agent.pendingReboot && (
        <div className="rounded-md border border-amber-800/50 bg-amber-950/30 px-3 py-2 text-sm text-amber-200">
          Reboot pending{agent.lastMetrics?.pendingRebootReason ? `: ${agent.lastMetrics.pendingRebootReason}` : ""}
        </div>
      )}
    </div>
  );
}

function ProcessesTab({
  snap,
  search,
  onSearch,
}: {
  snap: Snapshot;
  search: string;
  onSearch: (v: string) => void;
}) {
  const rows = useMemo(() => {
    const base =
      snap.activeProcesses && snap.activeProcesses.length > 0
        ? snap.activeProcesses
        : mergeUnique(snap.topByCpu, snap.topByMem);
    const q = search.trim().toLowerCase();
    if (!q) return base;
    return base.filter(
      (p) =>
        p.name.toLowerCase().includes(q) ||
        String(p.pid).includes(q) ||
        (p.user ?? "").toLowerCase().includes(q),
    );
  }, [snap, search]);

  const [sortKey, setSortKey] = useState<"cpu" | "mem" | "name">("cpu");
  const sorted = useMemo(() => {
    const r = [...rows];
    r.sort((a, b) => {
      if (sortKey === "name") return a.name.localeCompare(b.name);
      if (sortKey === "mem") return (b.memPct ?? 0) - (a.memPct ?? 0);
      return b.cpuPct - a.cpuPct;
    });
    return r;
  }, [rows, sortKey]);

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        <input
          value={search}
          onChange={(e) => onSearch(e.target.value)}
          placeholder="Search by name or PID…"
          className="h-8 w-64 rounded-md border border-ink-700 bg-ink-950 px-2 text-xs"
        />
        <span className="text-xs text-slate-500">{sorted.length} processes</span>
      </div>
      <div className="overflow-auto rounded-xl border border-ink-800">
        <table className="w-full min-w-[1000px] text-left text-sm">
          <thead className="bg-ink-800/80 text-xs uppercase text-slate-400">
            <tr>
              <th className="cursor-pointer px-3 py-2" onClick={() => setSortKey("name")}>
                Name
              </th>
              <th className="px-3 py-2">Arch</th>
              <th className="px-3 py-2">Elevated</th>
              <th className="px-3 py-2">User</th>
              <th className="px-3 py-2">Duration</th>
              <th className="px-3 py-2">Priority</th>
              <th className="px-3 py-2">Service</th>
              <th className="cursor-pointer px-3 py-2" onClick={() => setSortKey("cpu")}>
                CPU %
              </th>
              <th className="cursor-pointer px-3 py-2" onClick={() => setSortKey("mem")}>
                Memory %
              </th>
              <th className="px-3 py-2">Disk I/O</th>
              <th className="px-3 py-2">Network I/O</th>
            </tr>
          </thead>
          <tbody>
            {sorted.map((p) => (
              <tr key={p.pid + p.name} className="border-t border-ink-800/70 even:bg-ink-950/30">
                <td className="px-3 py-1.5 font-medium text-slate-200">
                  {p.name}{" "}
                  <span className="font-normal text-slate-500">({p.pid})</span>
                </td>
                <td className="px-3 py-1.5 text-slate-400">{p.architecture || "—"}</td>
                <td className="px-3 py-1.5 text-slate-400">
                  {p.elevated == null ? "—" : p.elevated ? "Yes" : "No"}
                </td>
                <td className="max-w-[12rem] truncate px-3 py-1.5 text-xs text-slate-400" title={p.user}>
                  {p.user || "—"}
                </td>
                <td className="px-3 py-1.5 text-xs text-slate-400">
                  {p.startedAt ? formatDuration((Date.now() - new Date(p.startedAt).getTime()) / 1000) : "—"}
                </td>
                <td className="px-3 py-1.5">
                  <PriorityPill priority={p.priority} />
                </td>
                <td className="px-3 py-1.5 text-slate-400">
                  {p.isService == null ? "—" : p.isService ? "Yes" : "No"}
                </td>
                <td className="px-3 py-1.5 tabular-nums text-slate-200">{formatPct(p.cpuPct)}</td>
                <td className="px-3 py-1.5 tabular-nums text-slate-200">
                  {p.memPct != null ? formatPct(p.memPct) : "—"}
                </td>
                <td className="px-3 py-1.5 text-xs tabular-nums text-slate-400">
                  {formatBytes((p.diskReadBps ?? 0) + (p.diskWriteBps ?? 0))}/s
                </td>
                <td className="px-3 py-1.5 text-xs tabular-nums text-slate-400">
                  {formatBytes((p.netSentBps ?? 0) + (p.netRecvBps ?? 0))}/s
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function NetworkTab({
  id,
  snap,
  netSub,
  onNetSub,
  search,
  onSearch,
  agent,
}: {
  id: string;
  snap: Snapshot;
  netSub: NetSub;
  onNetSub: (v: NetSub) => void;
  search: string;
  onSearch: (v: string) => void;
  agent: AgentDetail;
}) {
  const conns = useMemo(() => {
    const list = snap.conversations ?? [];
    const q = search.trim().toLowerCase();
    if (!q) return list;
    return list.filter(
      (c) =>
        (c.processName ?? "").toLowerCase().includes(q) ||
        c.remoteIp.includes(q) ||
        (c.localIp ?? "").includes(q) ||
        String(c.pid ?? "").includes(q),
    );
  }, [snap.conversations, search]);

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-2">
        {(["tcp", "map", "topology"] as NetSub[]).map((s) => (
          <button
            key={s}
            type="button"
            onClick={() => onNetSub(s)}
            className={
              "rounded-md px-3 py-1.5 text-xs font-medium " +
              (netSub === s
                ? "bg-sonar-700/50 text-sonar-100"
                : "bg-ink-800 text-slate-400 hover:text-slate-200")
            }
          >
            {s === "tcp" ? "TCP Connections" : s === "map" ? "Map" : "Topology"}
          </button>
        ))}
      </div>

      {netSub === "tcp" && (
        <>
          <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
            {snap.nics
              .filter((n) => n.kind !== "loopback")
              .slice(0, 6)
              .map((n) => (
                <div key={n.name} className="rounded-lg border border-ink-800 bg-ink-900/40 p-3">
                  <div className="text-sm font-medium text-slate-200">{n.name}</div>
                  <div className="text-xs text-slate-500">
                    {n.kind || "adapter"} · {n.up ? "Up" : "Down"} · {n.mac || "—"}
                  </div>
                  <div className="mt-1 font-mono text-[11px] text-slate-400">
                    {(n.addresses ?? []).join(", ") || "no address"}
                  </div>
                  <div className="mt-1 text-[11px] text-slate-500">
                    ↓ {formatBytes(n.bytesRecvBps ?? 0)}/s · ↑ {formatBytes(n.bytesSentBps ?? 0)}/s
                  </div>
                </div>
              ))}
          </div>
          <input
            value={search}
            onChange={(e) => onSearch(e.target.value)}
            placeholder="Search processes…"
            className="h-8 w-64 rounded-md border border-ink-700 bg-ink-950 px-2 text-xs"
          />
          <div className="overflow-auto rounded-xl border border-ink-800">
            <table className="w-full min-w-[900px] text-left text-sm">
              <thead className="bg-ink-800/80 text-xs uppercase text-slate-400">
                <tr>
                  <th className="px-3 py-2">Process</th>
                  <th className="px-3 py-2">Local Address</th>
                  <th className="px-3 py-2">Remote Address</th>
                  <th className="px-3 py-2">User</th>
                  <th className="px-3 py-2">State</th>
                  <th className="px-3 py-2">Direction</th>
                  <th className="px-3 py-2">Count</th>
                </tr>
              </thead>
              <tbody>
                {conns.length === 0 && (
                  <tr>
                    <td colSpan={7} className="px-3 py-6 text-center text-slate-500">
                      No active conversations
                    </td>
                  </tr>
                )}
                {conns.map((c, i) => (
                  <ConnRow key={i} c={c} />
                ))}
              </tbody>
            </table>
          </div>
        </>
      )}

      {netSub === "map" && (
        <div className="space-y-3 rounded-xl border border-ink-800 bg-ink-900/40 p-4">
          <div className="text-sm text-slate-300">
            Device location:{" "}
            {[agent.geoCity, agent.geoSubdivision, agent.geoCountryName].filter(Boolean).join(", ") ||
              "unknown"}
            {agent.publicIp && (
              <span className="text-slate-500">
                {" "}
                · {agent.publicIp}
                {agent.geoOrg ? ` (${agent.geoOrg})` : ""}
              </span>
            )}
          </div>
          <p className="text-xs text-slate-500">
            Connection arcs use the per-device network graph (topology view). Remote peer GeoIP
            arcs are a future enhancement.
          </p>
          <AgentNetworkGraphSection agentId={id} />
        </div>
      )}

      {netSub === "topology" && <AgentNetworkGraphSection agentId={id} />}
    </div>
  );
}

function ConnRow({ c }: { c: SnapshotConversation }) {
  const local =
    c.localIp || c.localPort
      ? `${c.localIp || "*"}:${c.localPort ?? ""}`
      : "—";
  const remote = `${c.remoteIp}:${c.remotePort}`;
  return (
    <tr className="border-t border-ink-800/70 even:bg-ink-950/30">
      <td className="px-3 py-1.5">
        {c.processName || "?"}{" "}
        {c.pid != null && <span className="text-slate-500">({c.pid})</span>}
      </td>
      <td className="px-3 py-1.5 font-mono text-xs text-slate-400">{local}</td>
      <td className="px-3 py-1.5 font-mono text-xs text-slate-400">
        {remote}
        {c.remoteHost && <span className="text-slate-600"> ({c.remoteHost})</span>}
      </td>
      <td className="px-3 py-1.5 text-xs text-slate-400">{c.user || "—"}</td>
      <td className="px-3 py-1.5 text-xs text-slate-300">{c.state || "—"}</td>
      <td className="px-3 py-1.5 text-xs text-slate-400">{c.direction}</td>
      <td className="px-3 py-1.5 tabular-nums text-slate-400">{c.count}</td>
    </tr>
  );
}

function StorageTab({ snap }: { snap: Snapshot }) {
  const [procQ, setProcQ] = useState("");
  const [fileQ, setFileQ] = useState("");
  const events = useMemo(() => {
    const list = [...(snap.storageIo ?? [])].reverse();
    const pq = procQ.trim().toLowerCase();
    const fq = fileQ.trim().toLowerCase();
    return list.filter((e) => {
      if (pq && !(`${e.process} ${e.pid}`).toLowerCase().includes(pq)) return false;
      if (fq && !(e.file ?? "").toLowerCase().includes(fq)) return false;
      return true;
    });
  }, [snap.storageIo, procQ, fileQ]);

  return (
    <div className="space-y-4">
      <div className="overflow-auto rounded-xl border border-ink-800">
        <table className="w-full text-left text-sm">
          <thead className="bg-ink-800/80 text-xs uppercase text-slate-400">
            <tr>
              <th className="px-3 py-2">Mount</th>
              <th className="px-3 py-2">Device</th>
              <th className="px-3 py-2">FS</th>
              <th className="px-3 py-2">Used</th>
              <th className="px-3 py-2">Free</th>
              <th className="px-3 py-2">Total</th>
              <th className="px-3 py-2">%</th>
            </tr>
          </thead>
          <tbody>
            {snap.disks.map((d) => (
              <tr key={d.mountpoint + d.device} className="border-t border-ink-800/70">
                <td className="px-3 py-2 font-medium text-slate-200">{d.mountpoint}</td>
                <td className="px-3 py-2 text-slate-400">{d.device}</td>
                <td className="px-3 py-2 text-slate-400">{d.fsType}</td>
                <td className="px-3 py-2 tabular-nums">{formatBytes(d.usedBytes)}</td>
                <td className="px-3 py-2 tabular-nums">{formatBytes(d.freeBytes)}</td>
                <td className="px-3 py-2 tabular-nums">{formatBytes(d.totalBytes)}</td>
                <td className="px-3 py-2">
                  <div className="flex items-center gap-2">
                    <span className="tabular-nums text-slate-200">{formatPct(d.usedPct)}</span>
                    <div className="h-1 w-16 overflow-hidden rounded bg-ink-800">
                      <div
                        className={"h-full " + pctBarColor(d.usedPct)}
                        style={{ width: `${Math.min(100, d.usedPct)}%` }}
                      />
                    </div>
                  </div>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <div className="flex flex-wrap items-center gap-2">
        <input
          value={procQ}
          onChange={(e) => setProcQ(e.target.value)}
          placeholder="Search by Process Name or ID"
          className="h-8 w-56 rounded-md border border-ink-700 bg-ink-950 px-2 text-xs"
        />
        <input
          value={fileQ}
          onChange={(e) => setFileQ(e.target.value)}
          placeholder="Search by File (wildcard)"
          className="h-8 w-56 rounded-md border border-ink-700 bg-ink-950 px-2 text-xs"
        />
        <span className="text-xs text-slate-500">{events.length} I/O samples</span>
      </div>
      <div className="overflow-auto rounded-xl border border-ink-800">
        <table className="w-full min-w-[700px] text-left text-sm">
          <thead className="bg-ink-800/80 text-xs uppercase text-slate-400">
            <tr>
              <th className="px-3 py-2">Time</th>
              <th className="px-3 py-2">Process</th>
              <th className="px-3 py-2">File</th>
              <th className="px-3 py-2 text-right">Bytes</th>
            </tr>
          </thead>
          <tbody>
            {events.length === 0 && (
              <tr>
                <td colSpan={4} className="px-3 py-6 text-center text-slate-500">
                  No high disk-I/O samples yet. Samples appear when a process exceeds ~64 KiB/s.
                </td>
              </tr>
            )}
            {events.map((e, i) => (
              <tr key={i} className="border-t border-ink-800/70 even:bg-ink-950/30">
                <td className="px-3 py-1.5 text-xs tabular-nums text-slate-400">
                  {e.time ? new Date(e.time).toLocaleTimeString() : "—"}
                </td>
                <td className="px-3 py-1.5">
                  {e.process} <span className="text-slate-500">({e.pid})</span>
                </td>
                <td className="max-w-[28rem] truncate px-3 py-1.5 text-xs text-slate-400" title={e.file}>
                  {e.file || "—"}
                </td>
                <td className="px-3 py-1.5 text-right tabular-nums text-slate-300">
                  {formatBytes(e.bytes)}/s
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <p className="text-[11px] text-slate-600">
        Storage I/O is process-aggregate (read+write rate). Per-path ETW file tracing is not enabled
        in this probe build.
      </p>
    </div>
  );
}

function ServicesTab({ snap }: { snap: Snapshot }) {
  const rows: SnapshotService[] =
    snap.services && snap.services.length > 0
      ? snap.services
      : snap.stoppedAutoServices ?? [];
  const [q, setQ] = useState("");
  const filtered = rows.filter(
    (s) =>
      !q ||
      s.name.toLowerCase().includes(q.toLowerCase()) ||
      (s.displayName ?? "").toLowerCase().includes(q.toLowerCase()),
  );
  return (
    <div className="space-y-3">
      <input
        value={q}
        onChange={(e) => setQ(e.target.value)}
        placeholder="Search services…"
        className="h-8 w-64 rounded-md border border-ink-700 bg-ink-950 px-2 text-xs"
      />
      <div className="overflow-auto rounded-xl border border-ink-800">
        <table className="w-full text-left text-sm">
          <thead className="bg-ink-800/80 text-xs uppercase text-slate-400">
            <tr>
              <th className="px-3 py-2">Name</th>
              <th className="px-3 py-2">Display name</th>
              <th className="px-3 py-2">Start type</th>
              <th className="px-3 py-2">Status</th>
            </tr>
          </thead>
          <tbody>
            {filtered.length === 0 && (
              <tr>
                <td colSpan={4} className="px-3 py-6 text-center text-slate-500">
                  No services in snapshot (Windows probe required for full inventory)
                </td>
              </tr>
            )}
            {filtered.map((s) => (
              <tr key={s.name} className="border-t border-ink-800/70 even:bg-ink-950/30">
                <td className="px-3 py-1.5 font-mono text-xs text-slate-200">{s.name}</td>
                <td className="px-3 py-1.5 text-slate-400">{s.displayName || "—"}</td>
                <td className="px-3 py-1.5 text-slate-400">{s.startType || "—"}</td>
                <td className="px-3 py-1.5">
                  <span
                    className={
                      s.status === "running"
                        ? "text-emerald-300"
                        : s.status === "stopped"
                          ? "text-amber-300"
                          : "text-slate-400"
                    }
                  >
                    {s.status || "—"}
                  </span>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function SessionsTab({ snap }: { snap: Snapshot }) {
  return (
    <div className="overflow-auto rounded-xl border border-ink-800">
      <table className="w-full text-left text-sm">
        <thead className="bg-ink-800/80 text-xs uppercase text-slate-400">
          <tr>
            <th className="px-3 py-2">User</th>
            <th className="px-3 py-2">State</th>
            <th className="px-3 py-2">Source</th>
            <th className="px-3 py-2">Host / TTY</th>
            <th className="px-3 py-2">Started</th>
          </tr>
        </thead>
        <tbody>
          {snap.loggedInUsers.length === 0 && (
            <tr>
              <td colSpan={5} className="px-3 py-6 text-center text-slate-500">
                No interactive sessions
              </td>
            </tr>
          )}
          {snap.loggedInUsers.map((s, i) => (
            <tr key={i} className="border-t border-ink-800/70">
              <td className="px-3 py-2 font-medium text-slate-200">{s.user}</td>
              <td className="px-3 py-2 text-slate-400">{s.state || "—"}</td>
              <td className="px-3 py-2 text-slate-400">{s.source || "—"}</td>
              <td className="px-3 py-2 text-slate-400">
                {[s.host, s.tty].filter(Boolean).join(" · ") || "—"}
              </td>
              <td className="px-3 py-2 text-xs text-slate-500">
                {s.started ? formatRelative(s.started) : "—"}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function PatchesTab({ snap }: { snap: Snapshot }) {
  const patches = snap.missingPatches ?? [];
  const count = snap.health?.missingPatchCount ?? patches.length;
  const [q, setQ] = useState("");
  const filtered = patches.filter(
    (p) =>
      !q ||
      p.title.toLowerCase().includes(q.toLowerCase()) ||
      (p.kb ?? "").toLowerCase().includes(q.toLowerCase()),
  );
  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-center gap-3">
        <div className="text-sm text-slate-200">
          Missing patches:{" "}
          <span className="font-semibold tabular-nums">{count == null ? "—" : count}</span>
        </div>
        {snap.win11Readiness && (
          <div className="rounded-md border border-ink-700 bg-ink-900/50 px-3 py-1.5 text-xs text-slate-300">
            Win11:{" "}
            <span className={snap.win11Readiness.eligible ? "text-emerald-300" : "text-amber-300"}>
              {snap.win11Readiness.eligible ? "Ready / OK" : "Not ready"}
            </span>
            {snap.win11Readiness.reason && (
              <span className="text-slate-500"> — {snap.win11Readiness.reason}</span>
            )}
          </div>
        )}
        <input
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder="Search patches…"
          className="h-8 w-56 rounded-md border border-ink-700 bg-ink-950 px-2 text-xs"
        />
      </div>
      <div className="overflow-auto rounded-xl border border-ink-800">
        <table className="w-full text-left text-sm">
          <thead className="bg-ink-800/80 text-xs uppercase text-slate-400">
            <tr>
              <th className="px-3 py-2">Title</th>
              <th className="px-3 py-2">KB</th>
              <th className="px-3 py-2">Severity</th>
              <th className="px-3 py-2 text-right">Size</th>
            </tr>
          </thead>
          <tbody>
            {filtered.length === 0 && (
              <tr>
                <td colSpan={4} className="px-3 py-6 text-center text-slate-500">
                  {patches.length === 0
                    ? "No missing patches reported (or inventory not collected yet — waits for 5-min health cycle)."
                    : "No patches match the filter."}
                </td>
              </tr>
            )}
            {filtered.map((p, i) => (
              <tr key={i} className="border-t border-ink-800/70 even:bg-ink-950/30">
                <td className="px-3 py-1.5 text-slate-200">{p.title}</td>
                <td className="px-3 py-1.5 font-mono text-xs text-slate-400">{p.kb || "—"}</td>
                <td className="px-3 py-1.5 text-slate-400">{p.severity || "—"}</td>
                <td className="px-3 py-1.5 text-right tabular-nums text-slate-400">
                  {p.sizeMb != null ? `${p.sizeMb} MB` : "—"}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function InstalledAppsTab({ snap }: { snap: Snapshot }) {
  const apps = snap.installedApps ?? [];
  const [q, setQ] = useState("");
  const filtered = apps.filter(
    (a) =>
      !q ||
      a.name.toLowerCase().includes(q.toLowerCase()) ||
      (a.publisher ?? "").toLowerCase().includes(q.toLowerCase()),
  );
  return (
    <div className="space-y-3">
      <div className="flex items-center gap-2">
        <input
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder="Search applications…"
          className="h-8 w-64 rounded-md border border-ink-700 bg-ink-950 px-2 text-xs"
        />
        <span className="text-xs text-slate-500">{filtered.length} / {apps.length}</span>
      </div>
      <div className="overflow-auto rounded-xl border border-ink-800">
        <table className="w-full min-w-[800px] text-left text-sm">
          <thead className="bg-ink-800/80 text-xs uppercase text-slate-400">
            <tr>
              <th className="px-3 py-2">Name</th>
              <th className="px-3 py-2">Version</th>
              <th className="px-3 py-2">Publisher</th>
              <th className="px-3 py-2">Install date</th>
            </tr>
          </thead>
          <tbody>
            {filtered.length === 0 && (
              <tr>
                <td colSpan={4} className="px-3 py-6 text-center text-slate-500">
                  {apps.length === 0
                    ? "No installed-app inventory yet (Windows probe, 5-min inventory cycle)."
                    : "No applications match."}
                </td>
              </tr>
            )}
            {filtered.map((a, i) => (
              <tr key={i} className="border-t border-ink-800/70 even:bg-ink-950/30">
                <td className="px-3 py-1.5 text-slate-200">{a.name}</td>
                <td className="px-3 py-1.5 text-slate-400">{a.version || "—"}</td>
                <td className="px-3 py-1.5 text-slate-400">{a.publisher || "—"}</td>
                <td className="px-3 py-1.5 text-xs text-slate-500">{a.installDate || "—"}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function StoppedProcessesTab({ snap }: { snap: Snapshot }) {
  const rows = [...(snap.stoppedProcesses ?? [])].reverse();
  const [q, setQ] = useState("");
  const filtered = rows.filter(
    (r) =>
      !q ||
      r.name.toLowerCase().includes(q.toLowerCase()) ||
      String(r.pid).includes(q),
  );
  return (
    <div className="space-y-3">
      <input
        value={q}
        onChange={(e) => setQ(e.target.value)}
        placeholder="Search stopped processes…"
        className="h-8 w-64 rounded-md border border-ink-700 bg-ink-950 px-2 text-xs"
      />
      <div className="overflow-auto rounded-xl border border-ink-800">
        <table className="w-full text-left text-sm">
          <thead className="bg-ink-800/80 text-xs uppercase text-slate-400">
            <tr>
              <th className="px-3 py-2">Time</th>
              <th className="px-3 py-2">Process</th>
              <th className="px-3 py-2">User</th>
              <th className="px-3 py-2">Duration</th>
              <th className="px-3 py-2">Last CPU %</th>
              <th className="px-3 py-2">Last Mem %</th>
            </tr>
          </thead>
          <tbody>
            {filtered.length === 0 && (
              <tr>
                <td colSpan={6} className="px-3 py-6 text-center text-slate-500">
                  No process-stop events yet. Events appear after the probe sees a process exit.
                </td>
              </tr>
            )}
            {filtered.map((r, i) => (
              <tr key={i} className="border-t border-ink-800/70 even:bg-ink-950/30">
                <td className="px-3 py-1.5 text-xs text-slate-400">
                  {r.time ? new Date(r.time).toLocaleString() : "—"}
                </td>
                <td className="px-3 py-1.5">
                  {r.name} <span className="text-slate-500">({r.pid})</span>
                </td>
                <td className="px-3 py-1.5 text-xs text-slate-400">{r.user || "—"}</td>
                <td className="px-3 py-1.5 text-xs text-slate-400">{r.duration || "—"}</td>
                <td className="px-3 py-1.5 tabular-nums">{formatPct(r.cpuPct)}</td>
                <td className="px-3 py-1.5 tabular-nums">{formatPct(r.memPct)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function TopAppsTab({ snap }: { snap: Snapshot }) {
  const apps = snap.topApps ?? [];
  return (
    <div className="overflow-auto rounded-xl border border-ink-800">
      <table className="w-full text-left text-sm">
        <thead className="bg-ink-800/80 text-xs uppercase text-slate-400">
          <tr>
            <th className="px-3 py-2">Application</th>
            <th className="px-3 py-2">PID</th>
            <th className="px-3 py-2">Focus time</th>
            <th className="px-3 py-2">Focus %</th>
            <th className="px-3 py-2">Last seen</th>
          </tr>
        </thead>
        <tbody>
          {apps.length === 0 && (
            <tr>
              <td colSpan={5} className="px-3 py-6 text-center text-slate-500">
                No foreground focus samples yet. The probe samples the active window every 15s.
              </td>
            </tr>
          )}
          {apps.map((a, i) => (
            <tr key={i} className="border-t border-ink-800/70 even:bg-ink-950/30">
              <td className="px-3 py-1.5 font-medium text-slate-200">{a.name}</td>
              <td className="px-3 py-1.5 text-slate-400">{a.pid ?? "—"}</td>
              <td className="px-3 py-1.5 tabular-nums text-slate-300">
                {formatDuration(a.focusSeconds)}
              </td>
              <td className="px-3 py-1.5 tabular-nums text-slate-300">
                {a.focusPct != null ? formatPct(a.focusPct) : "—"}
              </td>
              <td className="px-3 py-1.5 text-xs text-slate-500">
                {a.lastSeen ? formatRelative(a.lastSeen) : "—"}
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function EventLogTab({ snap }: { snap: Snapshot }) {
  const rows = snap.eventLog ?? [];
  const [q, setQ] = useState("");
  const filtered = rows.filter(
    (r) =>
      !q ||
      (r.message ?? "").toLowerCase().includes(q.toLowerCase()) ||
      (r.provider ?? "").toLowerCase().includes(q.toLowerCase()) ||
      String(r.eventId ?? "").includes(q),
  );
  return (
    <div className="space-y-3">
      <div className="grid gap-2 sm:grid-cols-4">
        <Stat label="Event log errors (24h)" value={snap.health?.eventLogErrorCount24h} />
        <Stat label="App crashes (24h)" value={snap.health?.appCrashCount24h} />
        <Stat label="BSODs (24h)" value={snap.health?.bsodCount24h} />
        <Stat label="Rows loaded" value={rows.length} />
      </div>
      <input
        value={q}
        onChange={(e) => setQ(e.target.value)}
        placeholder="Search events…"
        className="h-8 w-64 rounded-md border border-ink-700 bg-ink-950 px-2 text-xs"
      />
      <div className="overflow-auto rounded-xl border border-ink-800">
        <table className="w-full min-w-[900px] text-left text-sm">
          <thead className="bg-ink-800/80 text-xs uppercase text-slate-400">
            <tr>
              <th className="px-3 py-2">Time</th>
              <th className="px-3 py-2">Level</th>
              <th className="px-3 py-2">Log</th>
              <th className="px-3 py-2">Provider</th>
              <th className="px-3 py-2">ID</th>
              <th className="px-3 py-2">Message</th>
            </tr>
          </thead>
          <tbody>
            {filtered.length === 0 && (
              <tr>
                <td colSpan={6} className="px-3 py-6 text-center text-slate-500">
                  No event-log rows yet (Windows inventory cycle).
                </td>
              </tr>
            )}
            {filtered.map((r, i) => (
              <tr key={i} className="border-t border-ink-800/70 even:bg-ink-950/30">
                <td className="whitespace-nowrap px-3 py-1.5 text-xs text-slate-400">
                  {r.time ? new Date(r.time).toLocaleString() : "—"}
                </td>
                <td className="px-3 py-1.5 text-xs text-slate-300">{r.level || "—"}</td>
                <td className="px-3 py-1.5 text-xs text-slate-400">{r.log || "—"}</td>
                <td className="px-3 py-1.5 text-xs text-slate-400">{r.provider || "—"}</td>
                <td className="px-3 py-1.5 tabular-nums text-slate-400">{r.eventId ?? "—"}</td>
                <td className="max-w-[28rem] truncate px-3 py-1.5 text-xs text-slate-300" title={r.message}>
                  {r.message || "—"}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function PowerEventsTab({ snap }: { snap: Snapshot }) {
  const rows = snap.powerEvents ?? [];
  return (
    <div className="space-y-3">
      <div className="grid gap-2 sm:grid-cols-3">
        <Stat label="User reboots (24h)" value={snap.health?.userRebootCount24h} />
        <Stat label="BSODs (24h)" value={snap.health?.bsodCount24h} />
        <Stat label="Power events loaded" value={rows.length} />
      </div>
      <div className="overflow-auto rounded-xl border border-ink-800">
        <table className="w-full text-left text-sm">
          <thead className="bg-ink-800/80 text-xs uppercase text-slate-400">
            <tr>
              <th className="px-3 py-2">Time</th>
              <th className="px-3 py-2">Kind</th>
              <th className="px-3 py-2">Event ID</th>
              <th className="px-3 py-2">Message</th>
            </tr>
          </thead>
          <tbody>
            {rows.length === 0 && (
              <tr>
                <td colSpan={4} className="px-3 py-6 text-center text-slate-500">
                  No power events yet (Windows inventory cycle).
                </td>
              </tr>
            )}
            {rows.map((r, i) => (
              <tr key={i} className="border-t border-ink-800/70 even:bg-ink-950/30">
                <td className="whitespace-nowrap px-3 py-1.5 text-xs text-slate-400">
                  {r.time ? new Date(r.time).toLocaleString() : "—"}
                </td>
                <td className="px-3 py-1.5 capitalize text-slate-200">{r.kind}</td>
                <td className="px-3 py-1.5 tabular-nums text-slate-400">{r.eventId ?? "—"}</td>
                <td className="max-w-[32rem] truncate px-3 py-1.5 text-xs text-slate-400" title={r.message}>
                  {r.message || "—"}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function Stat({ label, value }: { label: string; value?: number }) {
  return (
    <div className="rounded-lg border border-ink-800 bg-ink-950/40 p-3">
      <div className="text-[11px] uppercase tracking-wide text-slate-500">{label}</div>
      <div className="mt-1 text-lg tabular-nums text-slate-100">
        {value == null ? "—" : value}
      </div>
    </div>
  );
}

function MetaCard({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="rounded-xl border border-ink-800 bg-ink-900/50 p-3">
      <h3 className="mb-2 text-[11px] font-semibold uppercase tracking-wide text-slate-500">
        {title}
      </h3>
      <div className="space-y-0.5">{children}</div>
    </div>
  );
}

function KV({ label, value }: { label: string; value: string }) {
  return (
    <div className="flex justify-between gap-2 text-[11px]">
      <span className="shrink-0 text-slate-500">{label}</span>
      <span className="truncate text-right text-slate-300" title={value}>
        {value}
      </span>
    </div>
  );
}

function ChartCard({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="rounded-xl border border-ink-800 bg-ink-900/40 p-3">
      <h3 className="mb-2 text-xs font-semibold text-slate-300">{title}</h3>
      {children}
    </div>
  );
}

function PriorityPill({ priority }: { priority?: string }) {
  if (!priority) return <span className="text-slate-600">—</span>;
  const high = priority.toLowerCase().includes("high");
  return (
    <span
      className={
        "rounded-full px-2 py-0.5 text-[10px] font-medium " +
        (high ? "bg-red-900/50 text-red-200" : "bg-slate-800 text-slate-300")
      }
    >
      {priority}
    </span>
  );
}

function mergeUnique(a: SnapshotProcess[], b: SnapshotProcess[]): SnapshotProcess[] {
  const map = new Map<number, SnapshotProcess>();
  for (const p of [...a, ...b]) map.set(p.pid, p);
  return Array.from(map.values());
}

const MAX_TAG_LEN = 32;
const MAX_TAG_COUNT = 32;
function normalizeTag(s: string): string {
  return s.trim().toLowerCase().slice(0, MAX_TAG_LEN);
}

function TagEditor({
  tags,
  suggestions,
  saving,
  error,
  onChange,
}: {
  tags: string[];
  suggestions: string[];
  saving: boolean;
  error: string | null;
  onChange: (next: string[]) => void;
}) {
  const [draft, setDraft] = useState("");
  const filteredSuggestions = useMemo(() => {
    const q = draft.trim().toLowerCase();
    const own = new Set(tags);
    return suggestions.filter((s) => !own.has(s) && (q === "" || s.includes(q))).slice(0, 8);
  }, [draft, suggestions, tags]);

  const commitDraft = () => {
    const t = normalizeTag(draft);
    setDraft("");
    if (!t || tags.includes(t) || tags.length >= MAX_TAG_COUNT) return;
    onChange([...tags, t]);
  };

  return (
    <div className="space-y-1">
      <div className="flex flex-wrap items-center gap-1.5">
        <span className="text-[10px] uppercase tracking-wide text-slate-500">Tags</span>
        {tags.map((t) => (
          <span
            key={t}
            className="inline-flex items-center gap-1 rounded-full border border-sonar-700/60 bg-sonar-900/30 px-2 py-0.5 text-[11px] text-sonar-100"
          >
            {t}
            <button type="button" onClick={() => onChange(tags.filter((x) => x !== t))} className="hover:text-red-300">
              ×
            </button>
          </span>
        ))}
        <input
          value={draft}
          onChange={(e) => setDraft(e.target.value)}
          onKeyDown={(e) => {
            if (e.key === "Enter" || e.key === ",") {
              e.preventDefault();
              commitDraft();
            }
          }}
          onBlur={commitDraft}
          placeholder="add tag…"
          className="h-6 w-24 rounded border border-ink-700 bg-ink-950 px-1.5 text-[11px]"
          disabled={saving}
        />
        {filteredSuggestions.length > 0 && draft && (
          <span className="text-[10px] text-slate-600">
            {filteredSuggestions.slice(0, 3).join(", ")}
          </span>
        )}
      </div>
      {error && <div className="text-[11px] text-red-300">{error}</div>}
    </div>
  );
}

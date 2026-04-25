// AgentDetail — the system tab for a single host. Designed to answer
// the questions an operator asks first when they click an agent:
//   1. Is it healthy right now? (stat cards + pending-reboot banner)
//   2. What's been going on the last 24h? (sparklines)
//   3. What's actually running? (top procs, listeners, sessions)
//   4. What about disks/NICs? (tables)
//   5. What's broken / waiting? (failed units / stopped services)
//
// Everything renders from one /agents/{id} fetch + one
// /agents/{id}/metrics fetch — no per-cell pulls — so the page is
// fast even on a slow VPN.

import { useQuery } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { api } from "../api/client";
import type {
  AgentDetail,
  MetricSeries,
  Site,
  SnapshotConversation,
  SnapshotDisk,
  SnapshotListener,
  SnapshotNIC,
  SnapshotProcess,
  SnapshotSession,
  SnapshotService,
} from "../api/types";
import Sparkline from "../components/Sparkline";
import {
  formatBytes,
  formatDuration,
  formatPct,
  formatRelative,
  pctBarColor,
} from "../lib/format";

export default function AgentDetailPage() {
  const { id = "" } = useParams<{ id: string }>();

  const agent = useQuery({
    queryKey: ["agent", id],
    queryFn: () => api.get<AgentDetail>(`/agents/${id}`),
    refetchInterval: 30_000,
    enabled: !!id,
  });
  const metrics = useQuery({
    queryKey: ["agent-metrics", id, "24h"],
    queryFn: () => api.get<MetricSeries>(`/agents/${id}/metrics?range=24h`),
    refetchInterval: 60_000,
    enabled: !!id,
  });
  const sites = useQuery({ queryKey: ["sites"], queryFn: () => api.get<Site[]>("/sites") });

  const snap = agent.data?.lastMetrics ?? null;

  const cpuSeries = useMemo(
    () => (metrics.data?.samples ?? []).map((s) => Number(s.cpuPct ?? 0)),
    [metrics.data],
  );
  const memSeries = useMemo(() => {
    if (!metrics.data) return [];
    return metrics.data.samples.map((s) => {
      const used = Number(s.memUsedBytes ?? 0);
      const total = Number(s.memTotalBytes ?? 0);
      return total > 0 ? (used / total) * 100 : 0;
    });
  }, [metrics.data]);

  if (agent.isLoading) {
    return <div className="text-sm text-slate-400">Loading agent…</div>;
  }
  if (agent.isError || !agent.data) {
    return (
      <div className="space-y-3">
        <Link to="/agents" className="text-sm text-sonar-400 hover:underline">
          ← Back to agents
        </Link>
        <div className="rounded-md border border-red-800/60 bg-red-950/40 p-4 text-sm text-red-200">
          Could not load agent: {(agent.error as Error)?.message ?? "unknown error"}
        </div>
      </div>
    );
  }

  const a = agent.data;
  const siteName = sites.data?.find((s) => s.id === a.siteId)?.name ?? a.siteId.slice(0, 8);
  const memPct =
    a.memUsedBytes != null && a.memTotalBytes && a.memTotalBytes > 0
      ? (Number(a.memUsedBytes) / Number(a.memTotalBytes)) * 100
      : null;
  const diskPct =
    a.rootDiskUsedBytes != null && a.rootDiskTotalBytes && a.rootDiskTotalBytes > 0
      ? (Number(a.rootDiskUsedBytes) / Number(a.rootDiskTotalBytes)) * 100
      : null;
  const online =
    a.lastSeenAt && Date.now() - new Date(a.lastSeenAt).getTime() < 5 * 60_000;

  return (
    <div className="space-y-6">
      <div className="flex items-end justify-between gap-4">
        <div>
          <Link to="/agents" className="text-xs text-sonar-400 hover:underline">
            ← All agents
          </Link>
          <h2 className="mt-1 text-2xl font-semibold tracking-tight">{a.hostname}</h2>
          <p className="text-sm text-slate-400">
            {siteName} · {a.os} {a.osVersion} · agent {a.agentVersion || "?"}
            {a.primaryIp && <> · {a.primaryIp}</>}
          </p>
        </div>
        <div className="text-right text-xs">
          <div>
            <span
              className={
                online
                  ? "rounded bg-emerald-900/40 px-2 py-0.5 text-emerald-300"
                  : "rounded bg-slate-800 px-2 py-0.5 text-slate-400"
              }
            >
              {online ? "online" : "offline"}
            </span>
          </div>
          <div className="mt-1 text-slate-500">
            last seen {formatRelative(a.lastSeenAt)}
          </div>
          <div className="text-slate-600">
            metrics {formatRelative(a.lastMetricsAt)}
          </div>
        </div>
      </div>

      {a.pendingReboot && (
        <div className="rounded-xl border border-amber-700/60 bg-amber-950/40 p-3 text-sm text-amber-200">
          <strong className="font-semibold">Reboot pending.</strong>{" "}
          {snap?.pendingRebootReason ||
            "The host has reported a pending reboot — patches or driver installs need this machine restarted to take effect."}
        </div>
      )}

      {snap == null ? (
        <div className="rounded-xl border border-ink-800 bg-ink-900 p-6 text-sm text-slate-400">
          No telemetry yet. The probe checks in every 60 seconds; if nothing
          appears within a couple of minutes, check the agent service is
          running and that <code>/agent/ws</code> is reachable.
        </div>
      ) : (
        <>
          <StatCards
            cpuPct={a.cpuPct ?? snap.cpu.usagePct}
            memPct={memPct ?? snap.memory.usedPct}
            diskPct={diskPct}
            uptime={a.uptimeSeconds ?? snap.host.uptimeSeconds}
            cores={snap.cpu.logicalCpus}
            memTotal={Number(a.memTotalBytes ?? snap.memory.totalBytes)}
            diskTotal={Number(a.rootDiskTotalBytes ?? 0)}
            cpuModel={snap.cpu.model}
          />

          <Charts cpu={cpuSeries} mem={memSeries} loading={metrics.isLoading} />

          <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
            <DisksTable disks={snap.disks ?? []} />
            <NicsTable nics={snap.nics ?? []} />
          </div>

          <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
            <ProcessTable title="Top processes by CPU" rows={snap.topByCpu ?? []} sortBy="cpu" />
            <ProcessTable title="Top processes by memory" rows={snap.topByMem ?? []} sortBy="mem" />
          </div>

          <ListenersTable listeners={snap.listeners ?? []} />

          <ConversationsTable conversations={snap.conversations ?? []} />

          <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
            <SessionsTable sessions={snap.loggedInUsers ?? []} />
            {snap.stoppedAutoServices && snap.stoppedAutoServices.length > 0 && (
              <ServicesTable services={snap.stoppedAutoServices} />
            )}
            {snap.failedUnits && snap.failedUnits.length > 0 && (
              <FailedUnitsTable units={snap.failedUnits} />
            )}
          </div>

          <HostMeta snap={snap} />

          {snap.collectionWarnings && snap.collectionWarnings.length > 0 && (
            <div className="rounded-xl border border-amber-800/40 bg-amber-950/20 p-3 text-xs text-amber-200">
              <div className="mb-1 font-semibold">Collection warnings</div>
              <ul className="list-inside list-disc space-y-0.5">
                {snap.collectionWarnings.map((w) => (
                  <li key={w}>{w}</li>
                ))}
              </ul>
            </div>
          )}
        </>
      )}
    </div>
  );
}

// ---- Stat cards ----------------------------------------------------------

interface StatCardsProps {
  cpuPct: number;
  memPct: number;
  diskPct: number | null;
  uptime: number;
  cores: number;
  memTotal: number;
  diskTotal: number;
  cpuModel: string;
}

function StatCards(p: StatCardsProps) {
  return (
    <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
      <Stat
        label="CPU"
        value={formatPct(p.cpuPct)}
        bar={p.cpuPct}
        sub={`${p.cores} logical · ${truncate(p.cpuModel, 28)}`}
      />
      <Stat
        label="Memory"
        value={formatPct(p.memPct)}
        bar={p.memPct}
        sub={`${formatBytes(p.memTotal)} total`}
      />
      <Stat
        label="Root disk"
        value={p.diskPct == null ? "—" : formatPct(p.diskPct)}
        bar={p.diskPct ?? 0}
        sub={p.diskTotal ? `${formatBytes(p.diskTotal)} total` : "—"}
      />
      <Stat
        label="Uptime"
        value={formatDuration(p.uptime)}
        sub={`since ${new Date(Date.now() - p.uptime * 1000).toLocaleDateString()}`}
      />
    </div>
  );
}

function Stat({
  label,
  value,
  sub,
  bar,
}: {
  label: string;
  value: string;
  sub?: string;
  bar?: number;
}) {
  return (
    <div className="rounded-xl border border-ink-800 bg-ink-900 p-4">
      <div className="text-xs uppercase tracking-wide text-slate-500">{label}</div>
      <div className="mt-1 text-2xl font-semibold">{value}</div>
      {bar != null && (
        <div className="mt-2 h-1.5 w-full overflow-hidden rounded bg-ink-800">
          <div
            className={"h-full " + pctBarColor(bar)}
            style={{ width: `${Math.min(100, Math.max(0, bar))}%` }}
          />
        </div>
      )}
      {sub && <div className="mt-2 text-xs text-slate-500">{sub}</div>}
    </div>
  );
}

// ---- Sparkline charts ----------------------------------------------------

function Charts({ cpu, mem, loading }: { cpu: number[]; mem: number[]; loading: boolean }) {
  return (
    <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
      <ChartCard title="CPU utilization (24h)" values={cpu} loading={loading} suffix="%" />
      <ChartCard title="Memory utilization (24h)" values={mem} loading={loading} suffix="%" />
    </div>
  );
}

function ChartCard({
  title,
  values,
  loading,
  suffix,
}: {
  title: string;
  values: number[];
  loading: boolean;
  suffix: string;
}) {
  const last = values[values.length - 1];
  const lastTxt = last == null ? "—" : `${last.toFixed(1)}${suffix}`;
  return (
    <div className="rounded-xl border border-ink-800 bg-ink-900 p-4">
      <div className="flex items-baseline justify-between">
        <div className="text-xs uppercase tracking-wide text-slate-500">{title}</div>
        <div className="text-sm tabular-nums text-slate-300">{lastTxt}</div>
      </div>
      <div className="mt-2">
        {loading ? (
          <div className="h-14 animate-pulse rounded bg-ink-800/50" />
        ) : (
          <Sparkline
            values={values}
            width={520}
            height={56}
            min={0}
            max={100}
            className="w-full"
            ariaLabel={title}
          />
        )}
      </div>
      <div className="mt-1 text-xs text-slate-600">
        {values.length} samples · 1/min cadence
      </div>
    </div>
  );
}

// ---- Disks ----------------------------------------------------------------

function DisksTable({ disks }: { disks: SnapshotDisk[] }) {
  return (
    <Section title="Disks">
      {disks.length === 0 ? (
        <Empty>No mounted volumes reported.</Empty>
      ) : (
        <Table head={["Mount", "FS", "Used", "Total", ""]}>
          {disks.map((d) => (
            <tr key={d.device + d.mountpoint} className="border-t border-ink-800">
              <td className="px-3 py-2 font-mono text-xs">{d.mountpoint}</td>
              <td className="px-3 py-2 text-xs text-slate-400">{d.fsType}</td>
              <td className="px-3 py-2 text-xs tabular-nums">{formatBytes(d.usedBytes)}</td>
              <td className="px-3 py-2 text-xs tabular-nums">{formatBytes(d.totalBytes)}</td>
              <td className="px-3 py-2">
                <UsageBar pct={d.usedPct} />
              </td>
            </tr>
          ))}
        </Table>
      )}
    </Section>
  );
}

// ---- NICs -----------------------------------------------------------------

function NicsTable({ nics }: { nics: SnapshotNIC[] }) {
  // Hide down loopback noise so the table tells a useful story.
  const visible = nics.filter(
    (n) =>
      n.up ||
      (n.addresses && n.addresses.some((a) => !a.startsWith("127.") && a !== "::1")),
  );
  return (
    <Section title="Network interfaces">
      {visible.length === 0 ? (
        <Empty>No up interfaces.</Empty>
      ) : (
        <Table head={["NIC", "Addresses", "TX", "RX"]}>
          {visible.map((n) => (
            <tr key={n.name} className="border-t border-ink-800 align-top">
              <td className="px-3 py-2">
                <div className="font-mono text-xs">{n.name}</div>
                <div className="text-[10px] text-slate-500">
                  {n.up ? "up" : "down"}
                  {n.mac && <> · {n.mac}</>}
                  {n.mtu ? <> · MTU {n.mtu}</> : null}
                </div>
              </td>
              <td className="px-3 py-2">
                <div className="flex flex-wrap gap-1">
                  {(n.addresses ?? []).map((a) => (
                    <span
                      key={a}
                      className="rounded bg-ink-800 px-1.5 py-0.5 font-mono text-[10px] text-slate-300"
                    >
                      {a}
                    </span>
                  ))}
                </div>
              </td>
              <td className="px-3 py-2 text-xs tabular-nums">{formatBytes(n.bytesSent)}</td>
              <td className="px-3 py-2 text-xs tabular-nums">{formatBytes(n.bytesRecv)}</td>
            </tr>
          ))}
        </Table>
      )}
    </Section>
  );
}

// ---- Processes ------------------------------------------------------------

function ProcessTable({
  title,
  rows,
  sortBy,
}: {
  title: string;
  rows: SnapshotProcess[];
  sortBy: "cpu" | "mem";
}) {
  return (
    <Section title={title}>
      {rows.length === 0 ? (
        <Empty>No processes to show.</Empty>
      ) : (
        <Table head={["PID", "Name", "User", sortBy === "cpu" ? "CPU%" : "RSS"]}>
          {rows.map((r) => (
            <tr key={`${r.pid}-${r.name}`} className="border-t border-ink-800">
              <td className="px-3 py-1.5 font-mono text-xs text-slate-500">{r.pid}</td>
              <td className="px-3 py-1.5 text-xs">
                <div className="font-medium">{r.name}</div>
                {r.cmdline && (
                  <div className="truncate text-[10px] text-slate-500" title={r.cmdline}>
                    {r.cmdline}
                  </div>
                )}
              </td>
              <td className="px-3 py-1.5 text-xs text-slate-400">{r.user || "—"}</td>
              <td className="px-3 py-1.5 text-right text-xs tabular-nums">
                {sortBy === "cpu" ? formatPct(r.cpuPct) : formatBytes(r.rssBytes)}
              </td>
            </tr>
          ))}
        </Table>
      )}
    </Section>
  );
}

// ---- Listeners ------------------------------------------------------------

function ListenersTable({ listeners }: { listeners: SnapshotListener[] }) {
  return (
    <Section title="Listening ports">
      {listeners.length === 0 ? (
        <Empty>No listening sockets.</Empty>
      ) : (
        <div className="max-h-72 overflow-auto">
          <Table head={["Proto", "Address", "Port", "PID", "Process"]}>
            {listeners.map((l, i) => (
              <tr key={i} className="border-t border-ink-800">
                <td className="px-3 py-1.5 font-mono text-xs text-slate-400">{l.proto}</td>
                <td className="px-3 py-1.5 font-mono text-xs">{l.address || "*"}</td>
                <td className="px-3 py-1.5 text-xs tabular-nums">{l.port}</td>
                <td className="px-3 py-1.5 text-xs text-slate-500">{l.pid || "—"}</td>
                <td className="px-3 py-1.5 text-xs">{l.processName || "—"}</td>
              </tr>
            ))}
          </Table>
        </div>
      )}
    </Section>
  );
}

// ---- Conversations ("talking to") ----------------------------------------

function ConversationsTable({ conversations }: { conversations: SnapshotConversation[] }) {
  const [filter, setFilter] = useState("");
  const [dir, setDir] = useState<"all" | "outbound" | "inbound">("all");

  const filtered = useMemo(() => {
    const q = filter.trim().toLowerCase();
    return conversations.filter((c) => {
      if (dir !== "all" && c.direction !== dir) return false;
      if (!q) return true;
      const peer = (c.remoteHost ?? c.remoteIp).toLowerCase();
      return (
        peer.includes(q) ||
        c.remoteIp.includes(q) ||
        String(c.remotePort).includes(q) ||
        (c.processName ?? "").toLowerCase().includes(q)
      );
    });
  }, [conversations, filter, dir]);

  const counts = useMemo(() => {
    let inbound = 0;
    let outbound = 0;
    for (const c of conversations) {
      if (c.direction === "inbound") inbound++;
      else if (c.direction === "outbound") outbound++;
    }
    return { inbound, outbound, total: conversations.length };
  }, [conversations]);

  return (
    <Section
      title={`Talking to (${counts.total} peers · ${counts.outbound} out · ${counts.inbound} in)`}
    >
      {conversations.length === 0 ? (
        <Empty>
          No active peer conversations reported. Requires probe v2026.4.25.6+ with the
          conversations collector enabled.
        </Empty>
      ) : (
        <div className="space-y-2">
          <div className="flex flex-wrap items-center gap-2">
            <input
              value={filter}
              onChange={(e) => setFilter(e.target.value)}
              placeholder="Filter by host, IP, port, or process…"
              className="w-72 rounded-md border border-ink-800 bg-ink-950 px-3 py-1.5 text-xs text-slate-200 placeholder:text-slate-600 focus:border-sonar-500 focus:outline-none"
            />
            <div className="flex overflow-hidden rounded-md border border-ink-800 text-[11px]">
              {(["all", "outbound", "inbound"] as const).map((d) => (
                <button
                  key={d}
                  type="button"
                  onClick={() => setDir(d)}
                  className={
                    "px-2.5 py-1 transition " +
                    (dir === d
                      ? "bg-sonar-700/40 text-sonar-200"
                      : "bg-ink-900 text-slate-400 hover:bg-ink-800")
                  }
                >
                  {d}
                </button>
              ))}
            </div>
            <span className="text-[11px] text-slate-500">
              showing {filtered.length} of {conversations.length}
            </span>
          </div>
          <div className="max-h-[28rem] overflow-auto">
            <Table head={["Dir", "Peer", "Port", "Proto", "Process", "State", "Conns"]}>
              {filtered.map((c, i) => (
                <tr key={i} className="border-t border-ink-800">
                  <td className="px-3 py-1.5">
                    <DirectionPill dir={c.direction} />
                  </td>
                  <td className="px-3 py-1.5">
                    <div className="flex flex-col">
                      <span className="text-xs text-slate-200">
                        {c.remoteHost || c.remoteIp}
                      </span>
                      {c.remoteHost && (
                        <span className="font-mono text-[10px] text-slate-500">
                          {c.remoteIp}
                        </span>
                      )}
                    </div>
                  </td>
                  <td className="px-3 py-1.5 text-xs tabular-nums">
                    {c.remotePort}
                    {c.direction === "inbound" && c.localPort != null && (
                      <span className="ml-1 text-[10px] text-slate-500">
                        ← :{c.localPort}
                      </span>
                    )}
                  </td>
                  <td className="px-3 py-1.5 font-mono text-xs text-slate-400">{c.proto}</td>
                  <td className="px-3 py-1.5 text-xs">
                    {c.processName || "—"}
                    {c.pid ? (
                      <span className="ml-1 text-[10px] text-slate-500">pid {c.pid}</span>
                    ) : null}
                  </td>
                  <td className="px-3 py-1.5 text-[11px] text-slate-500">
                    {c.state || "—"}
                  </td>
                  <td className="px-3 py-1.5 text-right text-xs tabular-nums text-slate-300">
                    {c.count}
                  </td>
                </tr>
              ))}
            </Table>
          </div>
        </div>
      )}
    </Section>
  );
}

function DirectionPill({ dir }: { dir: SnapshotConversation["direction"] }) {
  const cls =
    dir === "outbound"
      ? "bg-sonar-900/60 text-sonar-200"
      : dir === "inbound"
        ? "bg-emerald-900/60 text-emerald-200"
        : "bg-slate-800 text-slate-300";
  const label = dir === "outbound" ? "out" : dir === "inbound" ? "in" : dir;
  return (
    <span
      className={`inline-block rounded px-1.5 py-0.5 text-[10px] uppercase tracking-wide ${cls}`}
    >
      {label}
    </span>
  );
}

// ---- Sessions / services / failed units -----------------------------------

function SessionsTable({ sessions }: { sessions: SnapshotSession[] }) {
  return (
    <Section title="Logged-in users">
      {sessions.length === 0 ? (
        <Empty>No interactive sessions.</Empty>
      ) : (
        <Table head={["User", "Tty", "Host", "Since"]}>
          {sessions.map((s, i) => (
            <tr key={i} className="border-t border-ink-800">
              <td className="px-3 py-1.5 text-xs">{s.user}</td>
              <td className="px-3 py-1.5 font-mono text-xs text-slate-400">{s.tty || "—"}</td>
              <td className="px-3 py-1.5 text-xs text-slate-400">{s.host || "—"}</td>
              <td className="px-3 py-1.5 text-xs text-slate-500">
                {s.started ? formatRelative(s.started) : "—"}
              </td>
            </tr>
          ))}
        </Table>
      )}
    </Section>
  );
}

function ServicesTable({ services }: { services: SnapshotService[] }) {
  return (
    <Section title="Stopped automatic services">
      <Table head={["Service", "Display", "Start", "Status"]}>
        {services.map((s) => (
          <tr key={s.name} className="border-t border-ink-800">
            <td className="px-3 py-1.5 font-mono text-xs">{s.name}</td>
            <td className="px-3 py-1.5 text-xs text-slate-300">{s.displayName || "—"}</td>
            <td className="px-3 py-1.5 text-xs text-slate-400">{s.startType || "—"}</td>
            <td className="px-3 py-1.5 text-xs text-amber-300">{s.status || "—"}</td>
          </tr>
        ))}
      </Table>
    </Section>
  );
}

function FailedUnitsTable({ units }: { units: string[] }) {
  return (
    <Section title="Failed systemd units">
      <ul className="space-y-1">
        {units.map((u) => (
          <li key={u} className="rounded bg-ink-800/40 px-2 py-1 font-mono text-xs text-red-300">
            {u}
          </li>
        ))}
      </ul>
    </Section>
  );
}

// ---- Host meta ------------------------------------------------------------

function HostMeta({ snap }: { snap: NonNullable<AgentDetail["lastMetrics"]> }) {
  const items: Array<[string, string]> = [
    ["OS", `${snap.host.platform} ${snap.host.platformVersion}`],
    ["Family", snap.host.platformFamily || "—"],
    ["Kernel", `${snap.host.kernelVersion} (${snap.host.kernelArch})`],
    ["Boot", snap.host.bootTime ? new Date(snap.host.bootTime).toLocaleString() : "—"],
    ["Procs", String(snap.host.procs ?? 0)],
    ...(snap.host.virtualization
      ? ([["Virt", snap.host.virtualization]] as Array<[string, string]>)
      : []),
    [
      "Load",
      snap.loadAvg
        ? `${snap.loadAvg.load1} / ${snap.loadAvg.load5} / ${snap.loadAvg.load15}`
        : "—",
    ],
    ["Captured", formatRelative(snap.capturedAt)],
  ];
  return (
    <Section title="Host">
      <dl className="grid grid-cols-2 gap-x-4 gap-y-2 text-xs md:grid-cols-4">
        {items.map(([k, v]) => (
          <div key={k}>
            <dt className="text-slate-500">{k}</dt>
            <dd className="font-mono text-slate-200">{v}</dd>
          </div>
        ))}
      </dl>
    </Section>
  );
}

// ---- Shared bits ----------------------------------------------------------

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="rounded-xl border border-ink-800 bg-ink-900 p-4">
      <h3 className="mb-2 text-xs font-semibold uppercase tracking-wide text-slate-400">
        {title}
      </h3>
      {children}
    </div>
  );
}

function Table({ head, children }: { head: string[]; children: React.ReactNode }) {
  return (
    <table className="w-full text-left text-sm">
      <thead className="text-xs uppercase tracking-wide text-slate-500">
        <tr>
          {head.map((h) => (
            <th key={h} className="px-3 py-1.5">
              {h}
            </th>
          ))}
        </tr>
      </thead>
      <tbody>{children}</tbody>
    </table>
  );
}

function Empty({ children }: { children: React.ReactNode }) {
  return <div className="text-xs text-slate-500">{children}</div>;
}

function UsageBar({ pct }: { pct: number }) {
  const clamped = Math.min(100, Math.max(0, pct));
  return (
    <div className="flex items-center gap-2">
      <div className="h-1.5 w-24 overflow-hidden rounded bg-ink-800">
        <div
          className={"h-full " + pctBarColor(clamped)}
          style={{ width: `${clamped}%` }}
        />
      </div>
      <div className="w-10 text-right text-[10px] tabular-nums text-slate-400">
        {clamped.toFixed(0)}%
      </div>
    </div>
  );
}

function truncate(s: string, n: number) {
  if (!s) return "";
  return s.length > n ? s.slice(0, n - 1) + "…" : s;
}

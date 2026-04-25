// ApplianceDetail — the system tab for a single network appliance.
//
// Renders from one /appliances/{id} fetch + one /appliances/{id}/metrics
// fetch; per-port time-series is fetched lazily when the operator
// expands a row. Same shape and idioms as AgentDetail so the
// dashboard feels consistent across "the box on the rack" and "the
// switch above it".

import { useQuery } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { api } from "../api/client";
import type {
  ApplianceDetail,
  ApplianceEntity,
  ApplianceIfaceSeries,
  ApplianceInterface,
  ApplianceLLDP,
  ApplianceMetricSeries,
  Site,
} from "../api/types";
import Sparkline from "../components/Sparkline";
import {
  formatBytes,
  formatDuration,
  formatPct,
  formatRelative,
  pctBarColor,
} from "../lib/format";

export default function ApplianceDetailPage() {
  const { id = "" } = useParams<{ id: string }>();

  const appliance = useQuery({
    queryKey: ["appliance", id],
    queryFn: () => api.get<ApplianceDetail>(`/appliances/${id}`),
    refetchInterval: 30_000,
    enabled: !!id,
  });
  const metrics = useQuery({
    queryKey: ["appliance-metrics", id, "24h"],
    queryFn: () => api.get<ApplianceMetricSeries>(`/appliances/${id}/metrics?range=24h`),
    refetchInterval: 60_000,
    enabled: !!id,
  });
  const sites = useQuery({ queryKey: ["sites"], queryFn: () => api.get<Site[]>("/sites") });

  const snap = appliance.data?.lastSnapshot ?? null;

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

  if (appliance.isLoading) {
    return <div className="text-sm text-slate-400">Loading appliance…</div>;
  }
  if (appliance.isError || !appliance.data) {
    return (
      <div className="space-y-3">
        <Link to="/appliances" className="text-sm text-sonar-400 hover:underline">
          ← Back to appliances
        </Link>
        <div className="rounded-md border border-red-800/60 bg-red-950/40 p-4 text-sm text-red-200">
          Could not load appliance:{" "}
          {(appliance.error as Error)?.message ?? "unknown error"}
        </div>
      </div>
    );
  }

  const a = appliance.data;
  const siteName = sites.data?.find((s) => s.id === a.siteId)?.name ?? a.siteId.slice(0, 8);
  const memPct =
    a.memUsedBytes != null && a.memTotalBytes && a.memTotalBytes > 0
      ? (Number(a.memUsedBytes) / Number(a.memTotalBytes)) * 100
      : null;
  // "online" for a network device = we got a poll reply within
  // 3 × pollInterval. Anything older typically means SNMP is failing
  // or the device is unreachable.
  const polledRecently =
    a.lastPolledAt &&
    Date.now() - new Date(a.lastPolledAt).getTime() <
      Math.max(3 * a.pollIntervalSeconds, 180) * 1000;

  return (
    <div className="space-y-6">
      <div className="flex items-end justify-between gap-4">
        <div>
          <Link to="/appliances" className="text-xs text-sonar-400 hover:underline">
            ← All appliances
          </Link>
          <h2 className="mt-1 text-2xl font-semibold tracking-tight">
            {a.name}
            {a.sysName && a.sysName !== a.name && (
              <span className="ml-2 text-sm font-normal text-slate-400">
                ({a.sysName})
              </span>
            )}
          </h2>
          <p className="text-sm text-slate-400">
            {siteName} · {a.vendor}
            {a.model && <> · {a.model}</>} · SNMP {a.snmpVersion} ·{" "}
            <span className="font-mono">{a.mgmtIp}</span>
          </p>
        </div>
        <div className="text-right text-xs">
          <div>
            <span
              className={
                polledRecently
                  ? "rounded bg-emerald-900/40 px-2 py-0.5 text-emerald-300"
                  : "rounded bg-slate-800 px-2 py-0.5 text-slate-400"
              }
            >
              {polledRecently ? "polling" : "stale"}
            </span>
          </div>
          <div className="mt-1 text-slate-500">
            polled {formatRelative(a.lastPolledAt)}
          </div>
          <div className="text-slate-600">interval {a.pollIntervalSeconds}s</div>
        </div>
      </div>

      {a.lastError && (
        <div className="rounded-xl border border-red-800/60 bg-red-950/40 p-3 text-sm text-red-200">
          <strong className="font-semibold">Last poll failed.</strong>{" "}
          <code className="rounded bg-red-950/60 px-1 py-0.5 font-mono text-xs">
            {a.lastError}
          </code>
        </div>
      )}

      {snap == null ? (
        <div className="rounded-xl border border-ink-800 bg-ink-900 p-6 text-sm text-slate-400">
          No SNMP snapshot yet. The poller picks up new appliances within ~30
          seconds and polls each on its configured interval. If nothing
          appears within a couple of cycles, double-check the community
          string / v3 user and that the device's SNMP ACL allows the poller
          host's IP.
        </div>
      ) : (
        <>
          <StatCards
            cpuPct={a.cpuPct ?? snap.chassis.cpuPct ?? null}
            memPct={memPct}
            uptime={a.uptimeSeconds ?? snap.system.uptimeSeconds}
            physUp={
              a.physUpCount ??
              snap.interfaces.filter((i) => i.kind === "physical" && i.operUp).length
            }
            physTotal={
              a.physTotalCount ??
              snap.interfaces.filter((i) => i.kind === "physical").length
            }
            logicalCount={
              snap.interfaces.length -
              (a.physTotalCount ??
                snap.interfaces.filter((i) => i.kind === "physical").length)
            }
            uplinkCount={
              a.uplinkCount ?? snap.interfaces.filter((i) => i.isUplink).length
            }
            memTotal={Number(a.memTotalBytes ?? snap.chassis.memTotalBytes ?? 0)}
          />

          <Charts cpu={cpuSeries} mem={memSeries} loading={metrics.isLoading} />

          <InterfacesTable applianceId={a.id} interfaces={snap.interfaces ?? []} />

          {snap.entities && snap.entities.length > 0 && (
            <EntitiesTable entities={snap.entities} />
          )}

          {snap.lldp && snap.lldp.length > 0 && <LLDPTable neighbors={snap.lldp} />}

          <SystemMeta detail={a} />

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
  cpuPct: number | null;
  memPct: number | null;
  uptime: number;
  physUp: number;
  physTotal: number;
  logicalCount: number;
  uplinkCount: number;
  memTotal: number;
}

function StatCards(p: StatCardsProps) {
  // Physical ports are the only count that should drive the "X / Y up" UX —
  // an access switch's ifTable is dominated by SVIs, port-channels, and
  // loopbacks, so the raw number lies about how many cables you can plug in.
  const portsPct = p.physTotal > 0 ? (p.physUp / p.physTotal) * 100 : null;
  const sub =
    p.physTotal === 0
      ? "no physical ports"
      : p.uplinkCount > 0
        ? `${p.uplinkCount} uplink${p.uplinkCount === 1 ? "" : "s"} · ${p.logicalCount} logical`
        : `${p.logicalCount} logical interfaces`;
  return (
    <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
      <Stat
        label="CPU"
        value={formatPct(p.cpuPct)}
        bar={p.cpuPct ?? 0}
        sub={p.cpuPct == null ? "no chassis MIB" : "5s avg"}
      />
      <Stat
        label="Memory"
        value={formatPct(p.memPct)}
        bar={p.memPct ?? 0}
        sub={p.memTotal ? `${formatBytes(p.memTotal)} total` : "—"}
      />
      <Stat
        label="Physical ports"
        value={p.physTotal === 0 ? "—" : `${p.physUp} / ${p.physTotal}`}
        bar={portsPct ?? 0}
        sub={sub}
      />
      <Stat
        label="Uptime"
        value={formatDuration(p.uptime)}
        sub="since last reboot"
      />
    </div>
  );
}

interface StatProps {
  label: string;
  value: string;
  bar?: number;
  sub: string;
}

function Stat({ label, value, bar, sub }: StatProps) {
  return (
    <div className="rounded-xl border border-ink-800 bg-ink-900 p-4">
      <div className="text-xs uppercase tracking-wide text-slate-500">{label}</div>
      <div className="mt-1 text-2xl font-semibold tracking-tight text-slate-100">
        {value}
      </div>
      {bar != null && (
        <div className="mt-2 h-1.5 w-full overflow-hidden rounded-full bg-ink-800">
          <div
            className={`h-full ${pctBarColor(bar)}`}
            style={{ width: `${Math.min(100, Math.max(0, bar))}%` }}
          />
        </div>
      )}
      <div className="mt-2 text-xs text-slate-500">{sub}</div>
    </div>
  );
}

// ---- Sparkline charts ----------------------------------------------------

function Charts({
  cpu,
  mem,
  loading,
}: {
  cpu: number[];
  mem: number[];
  loading: boolean;
}) {
  return (
    <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
      <ChartCard title="CPU (24h)" data={cpu} suffix="%" loading={loading} />
      <ChartCard title="Memory (24h)" data={mem} suffix="%" loading={loading} />
    </div>
  );
}
function ChartCard({
  title,
  data,
  suffix,
  loading,
}: {
  title: string;
  data: number[];
  suffix: string;
  loading: boolean;
}) {
  const last = data.length > 0 ? data[data.length - 1] : null;
  return (
    <div className="rounded-xl border border-ink-800 bg-ink-900 p-4">
      <div className="flex items-baseline justify-between">
        <div className="text-sm text-slate-300">{title}</div>
        <div className="text-xs text-slate-500">
          {loading
            ? "loading…"
            : data.length === 0
              ? "no samples"
              : `now ${last?.toFixed(1)}${suffix}`}
        </div>
      </div>
      <Sparkline values={data} height={40} min={0} max={100} />
    </div>
  );
}

// ---- Interfaces table (the headline view for a switch) ------------------

type KindFilter = "physical" | "uplinks" | "all" | "logical";

function InterfacesTable({
  applianceId,
  interfaces,
}: {
  applianceId: string;
  interfaces: ApplianceInterface[];
}) {
  const [filter, setFilter] = useState("");
  const [hideDown, setHideDown] = useState(false);
  // Default to "physical" because that's what an operator means when they
  // say "ports". Uplinks are always pinned to the top regardless of this
  // filter (so they're visible even when the operator narrows to physical).
  const [kindFilter, setKindFilter] = useState<KindFilter>("physical");
  const [sortBy, setSortBy] = useState<"index" | "name" | "in" | "out">("index");
  const [expanded, setExpanded] = useState<number | null>(null);

  const counts = useMemo(() => {
    let phys = 0;
    let logical = 0;
    let uplinks = 0;
    for (const ifc of interfaces) {
      if (ifc.kind === "physical") phys++;
      else logical++;
      if (ifc.isUplink) uplinks++;
    }
    return { phys, logical, uplinks, total: interfaces.length };
  }, [interfaces]);

  const rows = useMemo(() => {
    let r = interfaces.filter((ifc) => {
      if (hideDown && !ifc.operUp) return false;
      switch (kindFilter) {
        case "physical":
          // Show physical ports, but never hide an uplink — port-channels
          // and 10G ports are usually classified non-physical and they're
          // exactly what an operator looking at "ports" wants to see.
          if (ifc.kind !== "physical" && !ifc.isUplink) return false;
          break;
        case "uplinks":
          if (!ifc.isUplink) return false;
          break;
        case "logical":
          if (ifc.kind === "physical") return false;
          break;
        case "all":
          break;
      }
      if (!filter) return true;
      const f = filter.toLowerCase();
      return (
        ifc.name?.toLowerCase().includes(f) ||
        ifc.descr?.toLowerCase().includes(f) ||
        (ifc.alias ?? "").toLowerCase().includes(f)
      );
    });
    r = [...r];
    switch (sortBy) {
      case "name":
        r.sort((a, b) => a.name.localeCompare(b.name, undefined, { numeric: true }));
        break;
      case "in":
        r.sort((a, b) => Number(b.inBps ?? 0) - Number(a.inBps ?? 0));
        break;
      case "out":
        r.sort((a, b) => Number(b.outBps ?? 0) - Number(a.outBps ?? 0));
        break;
      default:
        r.sort((a, b) => a.ifIndex - b.ifIndex);
    }
    // Uplinks always pin to the top so the most operationally important
    // ports stay above the fold no matter how the user sorts/filters.
    r.sort((a, b) => Number(b.isUplink ?? false) - Number(a.isUplink ?? false));
    return r;
  }, [interfaces, filter, hideDown, kindFilter, sortBy]);

  return (
    <div className="rounded-xl border border-ink-800 bg-ink-900">
      <div className="flex flex-wrap items-center gap-3 border-b border-ink-800 p-3">
        <div className="text-sm font-semibold">
          Interfaces{" "}
          <span className="font-normal text-slate-500">
            ({counts.phys} physical · {counts.logical} logical · {counts.uplinks} uplinks)
          </span>
        </div>
        <input
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          placeholder="filter by name / descr / alias…"
          className="ml-auto w-64 rounded-md border border-ink-700 bg-ink-950 px-2 py-1 text-xs"
        />
        <select
          value={kindFilter}
          onChange={(e) => setKindFilter(e.target.value as KindFilter)}
          className="rounded-md border border-ink-700 bg-ink-950 px-2 py-1 text-xs"
          title="Limit to physical ports (the default), uplinks only, all, or only logical (SVIs/loopbacks/etc.)"
        >
          <option value="physical">show: physical + uplinks</option>
          <option value="uplinks">show: uplinks only</option>
          <option value="logical">show: logical only</option>
          <option value="all">show: all ({counts.total})</option>
        </select>
        <label className="flex items-center gap-1 text-xs text-slate-400">
          <input
            type="checkbox"
            checked={hideDown}
            onChange={(e) => setHideDown(e.target.checked)}
          />
          hide down
        </label>
        <select
          value={sortBy}
          onChange={(e) => setSortBy(e.target.value as typeof sortBy)}
          className="rounded-md border border-ink-700 bg-ink-950 px-2 py-1 text-xs"
        >
          <option value="index">sort: index</option>
          <option value="name">sort: name</option>
          <option value="in">sort: in bps</option>
          <option value="out">sort: out bps</option>
        </select>
      </div>
      <div className="overflow-x-auto">
        <table className="w-full text-left text-sm">
          <thead className="bg-ink-800/40 text-xs uppercase tracking-wide text-slate-400">
            <tr>
              <th className="px-3 py-2">#</th>
              <th className="px-3 py-2">Name</th>
              <th className="px-3 py-2">Kind</th>
              <th className="px-3 py-2">Description / alias</th>
              <th className="px-3 py-2">Status</th>
              <th className="px-3 py-2 text-right">Speed</th>
              <th className="px-3 py-2 text-right">Last change</th>
              <th className="px-3 py-2 text-right">In</th>
              <th className="px-3 py-2 text-right">Out</th>
              <th className="px-3 py-2 text-right">Errors</th>
              <th className="px-3 py-2 text-right">Discards</th>
              <th className="px-3 py-2"></th>
            </tr>
          </thead>
          <tbody>
            {rows.length === 0 && (
              <tr>
                <td colSpan={12} className="px-3 py-6 text-center text-slate-500">
                  No interfaces match.
                </td>
              </tr>
            )}
            {rows.map((ifc) => (
              <Row
                key={ifc.ifIndex}
                applianceId={applianceId}
                ifc={ifc}
                expanded={expanded === ifc.ifIndex}
                onToggle={() =>
                  setExpanded(expanded === ifc.ifIndex ? null : ifc.ifIndex)
                }
              />
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function Row({
  applianceId,
  ifc,
  expanded,
  onToggle,
}: {
  applianceId: string;
  ifc: ApplianceInterface;
  expanded: boolean;
  onToggle: () => void;
}) {
  const errors = (ifc.inErrors ?? 0) + (ifc.outErrors ?? 0);
  const discards = (ifc.inDiscards ?? 0) + (ifc.outDiscards ?? 0);
  // Uplinks get a distinct row tint + a left-edge accent bar so they
  // stand out even in a long table.
  const rowClass = ifc.isUplink
    ? "border-t border-ink-800 bg-amber-950/10 hover:bg-amber-950/20"
    : "border-t border-ink-800 hover:bg-ink-800/30";
  return (
    <>
      <tr className={rowClass}>
        <td className="px-3 py-2 text-slate-500">
          {ifc.isUplink && (
            <span
              className="mr-1 inline-block h-3 w-1 rounded-sm bg-amber-400 align-middle"
              title="Uplink"
            />
          )}
          {ifc.ifIndex}
        </td>
        <td className="px-3 py-2 font-mono text-slate-200">
          {ifc.name}
          {ifc.isUplink && (
            <span
              className="ml-1.5 rounded bg-amber-500/20 px-1 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-amber-200"
              title="Heuristic: high speed, alias contains uplink/trunk, port-channel, or LLDP neighbor present"
            >
              uplink
            </span>
          )}
        </td>
        <td className="px-3 py-2">
          <KindBadge kind={ifc.kind} />
        </td>
        <td className="px-3 py-2 text-slate-400">
          <div>{ifc.descr || "—"}</div>
          {ifc.alias && (
            <div className="text-xs text-slate-500">"{ifc.alias}"</div>
          )}
        </td>
        <td className="px-3 py-2">
          <StatusBadge admin={ifc.adminUp} oper={ifc.operUp} />
        </td>
        <td className="px-3 py-2 text-right text-slate-400">
          {ifc.speedBps ? formatBitRate(ifc.speedBps) : "—"}
        </td>
        <td
          className="px-3 py-2 text-right text-slate-400"
          title={
            ifc.lastChangeSeconds != null
              ? `${ifc.operUp ? "Up" : "Down"} for ${formatDuration(ifc.lastChangeSeconds)} (since last ifLastChange)`
              : "Device did not report ifLastChange for this port"
          }
        >
          {ifc.lastChangeSeconds == null ? (
            <span className="text-slate-600">—</span>
          ) : (
            <span className={ifc.operUp ? "text-emerald-300/80" : "text-red-300/80"}>
              {formatDuration(ifc.lastChangeSeconds)}
            </span>
          )}
        </td>
        <td className="px-3 py-2 text-right text-emerald-300">
          {ifc.inBps == null ? "—" : formatBitRate(Number(ifc.inBps))}
        </td>
        <td className="px-3 py-2 text-right text-sonar-300">
          {ifc.outBps == null ? "—" : formatBitRate(Number(ifc.outBps))}
        </td>
        <td className={`px-3 py-2 text-right ${errors > 0 ? "text-amber-300" : "text-slate-500"}`}>
          {errors}
        </td>
        <td className={`px-3 py-2 text-right ${discards > 0 ? "text-amber-300" : "text-slate-500"}`}>
          {discards}
        </td>
        <td className="px-3 py-2 text-right">
          <button
            type="button"
            onClick={onToggle}
            className="rounded border border-ink-700 px-2 py-0.5 text-xs text-slate-300 hover:bg-ink-800"
          >
            {expanded ? "hide" : "graph"}
          </button>
        </td>
      </tr>
      {expanded && (
        <tr className="bg-ink-950/50">
          <td colSpan={12} className="px-3 py-3">
            <IfaceSparkline applianceId={applianceId} ifIndex={ifc.ifIndex} />
          </td>
        </tr>
      )}
    </>
  );
}

function KindBadge({ kind }: { kind?: ApplianceInterface["kind"] }) {
  const k = kind ?? "other";
  const styles: Record<string, string> = {
    physical: "bg-slate-800 text-slate-300",
    vlan: "bg-indigo-900/40 text-indigo-200",
    loopback: "bg-slate-800 text-slate-400",
    tunnel: "bg-violet-900/40 text-violet-200",
    lag: "bg-amber-900/40 text-amber-200",
    mgmt: "bg-emerald-900/40 text-emerald-200",
    stack: "bg-cyan-900/40 text-cyan-200",
    other: "bg-slate-800 text-slate-500",
  };
  const cls = styles[k] ?? styles.other;
  return (
    <span className={`rounded px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-wide ${cls}`}>
      {k}
    </span>
  );
}

function IfaceSparkline({
  applianceId,
  ifIndex,
}: {
  applianceId: string;
  ifIndex: number;
}) {
  const q = useQuery({
    queryKey: ["appliance-iface", applianceId, ifIndex, "24h"],
    queryFn: () =>
      api.get<ApplianceIfaceSeries>(
        `/appliances/${applianceId}/interfaces/${ifIndex}/metrics?range=24h`,
      ),
    refetchInterval: 60_000,
  });
  if (q.isLoading) return <div className="text-xs text-slate-500">Loading…</div>;
  if (q.isError) {
    return (
      <div className="text-xs text-red-300">
        Failed to load history: {(q.error as Error).message}
      </div>
    );
  }
  const samples = q.data?.samples ?? [];
  const inSeries = samples.map((s) => Number(s.inBps ?? 0));
  const outSeries = samples.map((s) => Number(s.outBps ?? 0));
  if (samples.length === 0) {
    return (
      <div className="text-xs text-slate-500">
        No samples yet — sparklines populate after the second poll cycle.
      </div>
    );
  }
  return (
    <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
      <div>
        <div className="mb-1 text-xs text-emerald-300">in bps (24h)</div>
        <Sparkline
          values={inSeries}
          height={32}
          min={0}
          strokeClass="stroke-emerald-400"
          fillClass="fill-emerald-500/15"
        />
      </div>
      <div>
        <div className="mb-1 text-xs text-sonar-300">out bps (24h)</div>
        <Sparkline values={outSeries} height={32} min={0} />
      </div>
    </div>
  );
}

function StatusBadge({ admin, oper }: { admin: boolean; oper: boolean }) {
  if (oper) {
    return (
      <span className="rounded bg-emerald-900/40 px-1.5 py-0.5 text-[11px] text-emerald-300">
        up
      </span>
    );
  }
  if (!admin) {
    return (
      <span className="rounded bg-slate-800 px-1.5 py-0.5 text-[11px] text-slate-400">
        admin-down
      </span>
    );
  }
  return (
    <span className="rounded bg-red-900/40 px-1.5 py-0.5 text-[11px] text-red-300">
      down
    </span>
  );
}

// ---- Entities (chassis hardware inventory) ------------------------------

function EntitiesTable({ entities }: { entities: ApplianceEntity[] }) {
  return (
    <div className="rounded-xl border border-ink-800 bg-ink-900">
      <div className="border-b border-ink-800 p-3 text-sm font-semibold">
        Hardware inventory{" "}
        <span className="font-normal text-slate-500">
          ({entities.length} entities)
        </span>
      </div>
      <div className="overflow-x-auto">
        <table className="w-full text-left text-sm">
          <thead className="bg-ink-800/40 text-xs uppercase tracking-wide text-slate-400">
            <tr>
              <th className="px-3 py-2">#</th>
              <th className="px-3 py-2">Class</th>
              <th className="px-3 py-2">Description</th>
              <th className="px-3 py-2">Model</th>
              <th className="px-3 py-2">Serial</th>
              <th className="px-3 py-2">HW</th>
              <th className="px-3 py-2">FW</th>
              <th className="px-3 py-2">SW</th>
            </tr>
          </thead>
          <tbody>
            {entities.map((e) => (
              <tr key={e.index} className="border-t border-ink-800">
                <td className="px-3 py-2 text-slate-500">{e.index}</td>
                <td className="px-3 py-2 text-slate-400">{entityClass(e.class)}</td>
                <td className="px-3 py-2">{e.description}</td>
                <td className="px-3 py-2 font-mono text-slate-300">{e.modelName || "—"}</td>
                <td className="px-3 py-2 font-mono text-slate-400">{e.serial || "—"}</td>
                <td className="px-3 py-2 text-slate-500">{e.hardwareRev || "—"}</td>
                <td className="px-3 py-2 text-slate-500">{e.firmwareRev || "—"}</td>
                <td className="px-3 py-2 text-slate-500">{e.softwareRev || "—"}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function entityClass(c: number): string {
  switch (c) {
    case 3:
      return "chassis";
    case 4:
      return "backplane";
    case 5:
      return "container";
    case 6:
      return "powerSupply";
    case 7:
      return "fan";
    case 8:
      return "sensor";
    case 9:
      return "module";
    case 10:
      return "port";
    default:
      return `class ${c}`;
  }
}

// ---- LLDP neighbors ------------------------------------------------------

function LLDPTable({ neighbors }: { neighbors: ApplianceLLDP[] }) {
  return (
    <div className="rounded-xl border border-ink-800 bg-ink-900">
      <div className="border-b border-ink-800 p-3 text-sm font-semibold">
        LLDP neighbors{" "}
        <span className="font-normal text-slate-500">
          ({neighbors.length})
        </span>
      </div>
      <div className="overflow-x-auto">
        <table className="w-full text-left text-sm">
          <thead className="bg-ink-800/40 text-xs uppercase tracking-wide text-slate-400">
            <tr>
              <th className="px-3 py-2">Local if</th>
              <th className="px-3 py-2">Remote system</th>
              <th className="px-3 py-2">Remote port</th>
              <th className="px-3 py-2">Chassis ID</th>
            </tr>
          </thead>
          <tbody>
            {neighbors.map((n, i) => (
              <tr key={`${n.localIfIndex}-${i}`} className="border-t border-ink-800">
                <td className="px-3 py-2 text-slate-500">{n.localIfIndex}</td>
                <td className="px-3 py-2">
                  <div className="text-slate-200">{n.remoteSysName || "—"}</div>
                  {n.remoteSysDescr && (
                    <div className="text-xs text-slate-500">{n.remoteSysDescr}</div>
                  )}
                </td>
                <td className="px-3 py-2 text-slate-300">
                  {n.remotePortDescr || n.remotePortId || "—"}
                </td>
                <td className="px-3 py-2 font-mono text-xs text-slate-500">
                  {n.remoteChassisId || "—"}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

// ---- System metadata footer ---------------------------------------------

function SystemMeta({ detail }: { detail: ApplianceDetail }) {
  const snap = detail.lastSnapshot;
  if (!snap) return null;
  return (
    <div className="rounded-xl border border-ink-800 bg-ink-900 p-4 text-xs text-slate-400">
      <div className="mb-2 text-sm font-semibold text-slate-200">System</div>
      <div className="grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-3">
        <Meta label="sysName" value={snap.system.name} />
        <Meta label="sysDescr" value={snap.system.description} />
        <Meta label="sysObjectID" value={snap.system.objectId || "—"} />
        <Meta label="sysContact" value={snap.system.contact || "—"} />
        <Meta label="sysLocation" value={snap.system.location || "—"} />
        <Meta label="captured" value={`${snap.collectMs} ms`} />
      </div>
    </div>
  );
}

function Meta({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <div className="text-[10px] uppercase tracking-wide text-slate-500">
        {label}
      </div>
      <div className="break-all font-mono text-slate-300">{value}</div>
    </div>
  );
}

// ---- helpers -------------------------------------------------------------

// formatBitRate renders bps as "1.2 Gbps", "340 Mbps", "12 Kbps".
// Nothing in the codebase needed this before, so it lives here rather
// than format.ts; bytes-on-disk vs bits-on-the-wire are easy to confuse
// and we want them visibly separate.
function formatBitRate(bps: number): string {
  if (!bps || bps < 0) return "0 bps";
  if (bps < 1_000) return `${bps} bps`;
  if (bps < 1_000_000) return `${(bps / 1_000).toFixed(1)} Kbps`;
  if (bps < 1_000_000_000) return `${(bps / 1_000_000).toFixed(1)} Mbps`;
  if (bps < 1_000_000_000_000) return `${(bps / 1_000_000_000).toFixed(2)} Gbps`;
  return `${(bps / 1_000_000_000_000).toFixed(2)} Tbps`;
}

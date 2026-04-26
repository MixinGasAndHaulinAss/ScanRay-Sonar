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

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { ApiError, api } from "../api/client";
import type {
  AgentDetail,
  HardwareDisk,
  HardwareGPU,
  HardwareMemoryModule,
  HardwareNIC,
  MetricSeries,
  Site,
  SnapshotConversation,
  SnapshotDisk,
  SnapshotHardware,
  SnapshotListener,
  SnapshotNIC,
  SnapshotProcess,
  SnapshotSession,
  SnapshotService,
} from "../api/types";
import AgentNetworkGraphSection from "../components/AgentNetworkGraph";
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
  const qc = useQueryClient();

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
  // Pull all agents for tag autocomplete: small payload, already
  // cached by the Agents list page so this is usually a no-op.
  const allAgents = useQuery({
    queryKey: ["agents"],
    queryFn: () => api.get<AgentDetail[]>("/agents"),
  });

  const updateTags = useMutation({
    mutationFn: (tags: string[]) =>
      api.patch<AgentDetail>(`/agents/${id}`, { tags }),
    onSuccess: (updated) => {
      qc.setQueryData(["agent", id], (prev: AgentDetail | undefined) =>
        prev ? { ...prev, tags: updated.tags } : prev,
      );
      qc.invalidateQueries({ queryKey: ["agents"] });
    },
  });

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
      <div className="flex flex-wrap items-end justify-between gap-4">
        <div className="min-w-0 flex-1 space-y-2">
          <Link to="/agents" className="text-xs text-sonar-400 hover:underline">
            ← All agents
          </Link>
          <h2 className="mt-1 text-2xl font-semibold tracking-tight">{a.hostname}</h2>
          <p className="text-sm text-slate-400">
            {siteName} · {a.os} {a.osVersion} · agent {a.agentVersion || "?"}
            {a.primaryIp && <> · {a.primaryIp}</>}
            {a.publicIp && (
              <>
                {" · "}
                <span className="text-slate-300">public {a.publicIp}</span>
                {(a.geoCity || a.geoCountryName) && (
                  <span className="text-slate-500">
                    {" "}
                    ({[a.geoCity, a.geoSubdivision, a.geoCountryIso].filter(Boolean).join(", ")})
                  </span>
                )}
                {a.geoOrg && (
                  <span className="text-slate-500"> via AS{a.geoAsn} {a.geoOrg}</span>
                )}
              </>
            )}
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

          <AgentNetworkGraphSection agentId={id} />

          <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
            <SessionsTable sessions={snap.loggedInUsers ?? []} />
            {snap.stoppedAutoServices && snap.stoppedAutoServices.length > 0 && (
              <ServicesTable services={snap.stoppedAutoServices} />
            )}
            {snap.failedUnits && snap.failedUnits.length > 0 && (
              <FailedUnitsTable units={snap.failedUnits} />
            )}
          </div>

          {snap.hardware && <HardwareSection hw={snap.hardware} />}

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

// ---- Tag editor ----------------------------------------------------------
//
// Inline tag editor for an agent. Tags are short, lowercase identifiers
// — "prod", "edge", "ec2", "k8s-node" — that operators use to filter
// the Agents list. The editor:
//   * Accepts comma- or Enter-separated input (with autocomplete from
//     other hosts' existing tags so spellings stay consistent).
//   * Removes a tag with click on its × or Backspace on an empty input.
//   * Sends a PATCH with the next list immediately (optimistic UX is
//     handled by react-query setQueryData in the parent).
//
// Tags are normalized server-side (lower, trimmed, deduped, capped at
// 32 chars and 32 total) — we just mirror that so the UI doesn't drift
// from what's persisted.

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
  const [focused, setFocused] = useState(false);

  // The set of suggestions narrows as the operator types and excludes
  // tags already on this host so we don't duplicate-suggest.
  const filteredSuggestions = useMemo(() => {
    const q = draft.trim().toLowerCase();
    const own = new Set(tags);
    return suggestions
      .filter((s) => !own.has(s) && (q === "" || s.includes(q)))
      .slice(0, 8);
  }, [draft, suggestions, tags]);

  const commitDraft = () => {
    const t = normalizeTag(draft);
    setDraft("");
    if (!t) return;
    if (tags.includes(t)) return;
    if (tags.length >= MAX_TAG_COUNT) return;
    onChange([...tags, t]);
  };

  const removeTag = (t: string) => onChange(tags.filter((x) => x !== t));

  return (
    <div className="space-y-1">
      <div className="flex flex-wrap items-center gap-1.5">
        <span className="text-[10px] uppercase tracking-wide text-slate-500">Tags</span>
        {tags.length === 0 && (
          <span className="text-[11px] italic text-slate-600">none</span>
        )}
        {tags.map((t) => (
          <span
            key={t}
            className="group inline-flex items-center gap-1 rounded-full border border-sonar-700/60 bg-sonar-900/30 px-2 py-0.5 text-[11px] text-sonar-100"
          >
            {t}
            <button
              type="button"
              onClick={() => removeTag(t)}
              className="text-sonar-300/60 hover:text-red-300"
              title={`Remove ${t}`}
              aria-label={`Remove ${t}`}
            >
              ×
            </button>
          </span>
        ))}
        <div className="relative">
          <input
            value={draft}
            onChange={(e) => {
              const v = e.target.value;
              if (v.endsWith(",")) {
                setDraft(v.slice(0, -1));
                setTimeout(commitDraft, 0);
                return;
              }
              setDraft(v);
            }}
            onKeyDown={(e) => {
              if (e.key === "Enter") {
                e.preventDefault();
                commitDraft();
              } else if (e.key === "Backspace" && draft === "" && tags.length > 0) {
                onChange(tags.slice(0, -1));
              } else if (e.key === "Escape") {
                setDraft("");
                (e.target as HTMLInputElement).blur();
              }
            }}
            onFocus={() => setFocused(true)}
            onBlur={() => {
              setTimeout(() => setFocused(false), 120);
              commitDraft();
            }}
            placeholder={tags.length === 0 ? "add tag…" : "+"}
            disabled={tags.length >= MAX_TAG_COUNT}
            className="h-6 w-28 rounded-full border border-ink-700 bg-ink-950 px-2 text-[11px] text-slate-100 placeholder:text-slate-600 focus:border-sonar-500 focus:outline-none"
          />
          {focused && filteredSuggestions.length > 0 && (
            <div className="absolute z-20 mt-1 max-h-52 w-44 overflow-auto rounded-md border border-ink-700 bg-ink-950 py-1 text-[11px] shadow-lg">
              {filteredSuggestions.map((s) => (
                <button
                  key={s}
                  type="button"
                  onMouseDown={(e) => {
                    e.preventDefault();
                    setDraft("");
                    if (!tags.includes(s)) onChange([...tags, s]);
                  }}
                  className="block w-full px-2 py-1 text-left text-slate-300 hover:bg-ink-800 hover:text-sonar-100"
                >
                  {s}
                </button>
              ))}
            </div>
          )}
        </div>
        {saving && <span className="text-[10px] text-slate-500">saving…</span>}
      </div>
      {error && <div className="text-[11px] text-red-300">{error}</div>}
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
//
// Process rows are click-to-expand. We keep a compact summary line in the
// table (so a couple-dozen procs fit on screen) and tuck the noisy bits
// — the full cmdline, every available field, and a copy button — into a
// details drawer that opens beneath the row. This fixes two earlier UX
// bugs at once:
//   * Long cmdlines were overflowing the column on narrow viewports.
//   * The `title=` tooltip was the only way to read them, which is
//     undiscoverable and unselectable.

function ProcessTable({
  title,
  rows,
  sortBy,
}: {
  title: string;
  rows: SnapshotProcess[];
  sortBy: "cpu" | "mem";
}) {
  const [expanded, setExpanded] = useState<Set<string>>(() => new Set());
  const toggle = (key: string) =>
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });

  return (
    <Section title={title}>
      {rows.length === 0 ? (
        <Empty>No processes to show.</Empty>
      ) : (
        // table-fixed + per-column widths makes the Name column actually
        // honour `truncate` instead of widening the table to fit the
        // cmdline. Without it, long cmdlines push the right-hand metric
        // column off-screen on narrow viewports.
        //
        // The two right-hand "stat" columns adapt to the table's
        // sortBy: when sorted by CPU we lead with CPU% and back it
        // with RSS; when sorted by mem we lead with RSS and back
        // with CPU%. The rest of the rich stats (disk/net Bps, open
        // conns) live in the per-row drawer to keep the visible
        // table compact on narrow viewports.
        <div className="overflow-hidden">
          <table className="w-full table-fixed text-left text-sm">
            <colgroup>
              <col className="w-6" />
              <col className="w-14" />
              <col />
              <col className="w-20" />
              <col className="w-20" />
              <col className="w-20" />
            </colgroup>
            <thead className="text-xs uppercase tracking-wide text-slate-500">
              <tr>
                <th className="px-1 py-1.5" aria-label="Expand" />
                <th className="px-3 py-1.5">PID</th>
                <th className="px-3 py-1.5">Name</th>
                <th className="px-3 py-1.5 text-right">
                  {sortBy === "cpu" ? "CPU%" : "RSS"}
                </th>
                <th className="px-3 py-1.5 text-right">
                  {sortBy === "cpu" ? "RSS" : "CPU%"}
                </th>
                <th className="px-3 py-1.5 text-right" title="Disk read+write per second">
                  Disk/s
                </th>
              </tr>
            </thead>
            <tbody>
              {rows.map((r) => {
                const key = `${r.pid}-${r.name}`;
                const open = expanded.has(key);
                return (
                  <ProcessRow
                    key={key}
                    row={r}
                    open={open}
                    onToggle={() => toggle(key)}
                    sortBy={sortBy}
                  />
                );
              })}
            </tbody>
          </table>
        </div>
      )}
    </Section>
  );
}

function ProcessRow({
  row,
  open,
  onToggle,
  sortBy,
}: {
  row: SnapshotProcess;
  open: boolean;
  onToggle: () => void;
  sortBy: "cpu" | "mem";
}) {
  const primary =
    sortBy === "cpu" ? formatPct(row.cpuPct) : formatBytes(row.rssBytes);
  const secondary =
    sortBy === "cpu" ? formatBytes(row.rssBytes) : formatPct(row.cpuPct);
  const diskBps = (row.diskReadBps ?? 0) + (row.diskWriteBps ?? 0);
  return (
    <>
      <tr
        className={
          "cursor-pointer border-t border-ink-800 transition hover:bg-ink-800/40 " +
          (open ? "bg-ink-800/30" : "")
        }
        onClick={onToggle}
        aria-expanded={open}
      >
        <td className="px-1 py-1.5 align-top">
          <Chevron open={open} />
        </td>
        <td className="px-3 py-1.5 align-top font-mono text-xs text-slate-500">
          {row.pid}
        </td>
        <td className="px-3 py-1.5 align-top text-xs">
          <div className="truncate font-medium">{row.name}</div>
          {row.user && (
            <div className="truncate text-[10px] text-slate-500">{row.user}</div>
          )}
        </td>
        <td className="px-3 py-1.5 align-top text-right text-xs tabular-nums">
          {primary}
        </td>
        <td className="px-3 py-1.5 align-top text-right text-xs tabular-nums text-slate-400">
          {secondary}
        </td>
        <td className="px-3 py-1.5 align-top text-right text-xs tabular-nums text-slate-400">
          {diskBps > 0 ? formatBytes(diskBps) + "/s" : "—"}
        </td>
      </tr>
      {open && (
        <tr className="border-t border-ink-800 bg-ink-950/40">
          <td className="px-1 py-3" />
          <td className="px-3 py-3" colSpan={5}>
            <ProcessDetails row={row} />
          </td>
        </tr>
      )}
    </>
  );
}

function ProcessDetails({ row }: { row: SnapshotProcess }) {
  const cmd = row.cmdline?.trim();
  // The drawer is the only place rich per-process counters surface,
  // so we lay them out in a 6-up grid with plain text values. We
  // fall back to "—" for the network rates because the current
  // probe doesn't yet emit per-process net Bps (gopsutil doesn't
  // expose it portably) — wiring those in is a separate change and
  // the UI is already prepared for the field's eventual arrival.
  const fields: Array<[string, string]> = [
    ["PID", String(row.pid)],
    ["Name", row.name],
    ["User", row.user || "—"],
    ["CPU", formatPct(row.cpuPct)],
    ["Memory", row.memPct != null ? `${formatPct(row.memPct)} (${formatBytes(row.rssBytes)})` : formatBytes(row.rssBytes)],
    ["Open conns", row.openConns != null ? String(row.openConns) : "—"],
    ["Disk read /s", row.diskReadBps ? formatBytes(row.diskReadBps) + "/s" : "—"],
    ["Disk write /s", row.diskWriteBps ? formatBytes(row.diskWriteBps) + "/s" : "—"],
    ["Net sent /s", row.netSentBps ? formatBytes(row.netSentBps) + "/s" : "—"],
    ["Net recv /s", row.netRecvBps ? formatBytes(row.netRecvBps) + "/s" : "—"],
  ];
  return (
    <div className="space-y-3 text-xs">
      <dl className="grid grid-cols-2 gap-x-4 gap-y-1.5 sm:grid-cols-3 lg:grid-cols-5">
        {fields.map(([k, v]) => (
          <Field key={k} label={k} value={v} mono={k === "PID" || k === "Name"} />
        ))}
      </dl>
      <div>
        <div className="mb-1 flex items-center justify-between">
          <span className="text-[10px] uppercase tracking-wide text-slate-500">
            Command line
          </span>
          {cmd && (
            <button
              type="button"
              onClick={(e) => {
                e.stopPropagation();
                navigator.clipboard?.writeText(cmd).catch(() => {});
              }}
              className="rounded border border-ink-800 bg-ink-900 px-2 py-0.5 text-[10px] text-slate-400 transition hover:border-sonar-700 hover:text-sonar-200"
            >
              Copy
            </button>
          )}
        </div>
        <pre className="overflow-x-auto whitespace-pre-wrap break-all rounded-md border border-ink-800 bg-ink-950 px-3 py-2 font-mono text-[11px] leading-snug text-slate-300">
          {cmd || "(no cmdline reported)"}
        </pre>
      </div>
    </div>
  );
}

function Field({
  label,
  value,
  mono,
}: {
  label: string;
  value: string;
  mono?: boolean;
}) {
  return (
    <div className="min-w-0">
      <dt className="text-[10px] uppercase tracking-wide text-slate-500">{label}</dt>
      <dd
        className={
          "truncate text-slate-200 " + (mono ? "font-mono text-[11px]" : "text-xs")
        }
        title={value}
      >
        {value}
      </dd>
    </div>
  );
}

function Chevron({ open }: { open: boolean }) {
  return (
    <svg
      width="12"
      height="12"
      viewBox="0 0 12 12"
      className={
        "transition-transform text-slate-500 " + (open ? "rotate-90 text-sonar-300" : "")
      }
      aria-hidden="true"
    >
      <path
        d="M4 2 L8 6 L4 10"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.5"
        strokeLinecap="round"
        strokeLinejoin="round"
      />
    </svg>
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

// ---- Hardware -------------------------------------------------------------
//
// "Hardware" is one section but four logical sub-tables: system (the
// box itself: vendor, BIOS, mobo, chassis), memory modules, storage,
// and network adapters/GPUs. Probe collects this once per 6h since
// it doesn't change between snapshots, so the UI doesn't need to
// re-render anything when it's missing — we just hide the section.

function HardwareSection({ hw }: { hw: SnapshotHardware }) {
  const empty =
    !hw.system &&
    !hw.cpu &&
    (!hw.memoryModules || hw.memoryModules.length === 0) &&
    (!hw.storage || hw.storage.length === 0) &&
    (!hw.networkAdapters || hw.networkAdapters.length === 0) &&
    (!hw.gpus || hw.gpus.length === 0);
  if (empty) return null;

  return (
    <div className="space-y-3 rounded-xl border border-ink-800 bg-ink-900 p-4">
      <div className="flex items-baseline justify-between">
        <h3 className="text-xs font-semibold uppercase tracking-wide text-slate-400">
          Hardware
        </h3>
        <span className="text-[10px] text-slate-600">
          collected once per probe lifetime
        </span>
      </div>

      {(hw.system || hw.cpu) && <HardwareSystemBlock hw={hw} />}

      <div className="grid grid-cols-1 gap-3 lg:grid-cols-2">
        {hw.memoryModules && hw.memoryModules.length > 0 && (
          <HardwareMemTable mods={hw.memoryModules} />
        )}
        {hw.storage && hw.storage.length > 0 && (
          <HardwareDiskTable disks={hw.storage} />
        )}
        {hw.networkAdapters && hw.networkAdapters.length > 0 && (
          <HardwareNICTable nics={hw.networkAdapters} />
        )}
        {hw.gpus && hw.gpus.length > 0 && <HardwareGPUTable gpus={hw.gpus} />}
      </div>

      {hw.collectionWarnings && hw.collectionWarnings.length > 0 && (
        <div className="rounded border border-amber-800/40 bg-amber-950/20 p-2 text-[11px] text-amber-200">
          <div className="mb-0.5 font-semibold">Hardware collector warnings</div>
          <ul className="list-inside list-disc space-y-0.5">
            {hw.collectionWarnings.map((w) => (
              <li key={w}>{w}</li>
            ))}
          </ul>
        </div>
      )}
    </div>
  );
}

function HardwareSystemBlock({ hw }: { hw: SnapshotHardware }) {
  const sys = hw.system ?? {};
  const cpu = hw.cpu ?? {};
  // Lay out the most identity-sensitive fields first so an operator
  // can confirm "yes this is the box I think it is" at a glance.
  const items: Array<[string, string | undefined]> = [
    ["Vendor", sys.manufacturer],
    ["Product", sys.productName],
    ["Serial", sys.serialNumber],
    ["Chassis", joinNonEmpty([sys.chassisType, sys.chassisAssetTag], " · ")],
    ["BIOS", joinNonEmpty([sys.biosVendor, sys.biosVersion, sys.biosDate], " · ")],
    ["Board", joinNonEmpty([sys.boardManufacturer, sys.boardProduct], " · ")],
    ["Board serial", sys.boardSerial],
    [
      "CPU",
      joinNonEmpty(
        [
          cpu.model,
          cpu.cores ? `${cpu.cores} cores` : undefined,
          cpu.threads ? `${cpu.threads} threads` : undefined,
          cpu.mhzNominal ? `${cpu.mhzNominal} MHz` : undefined,
        ],
        " · ",
      ),
    ],
  ].filter(([, v]) => v && v !== "");
  if (items.length === 0) return null;
  return (
    <dl className="grid grid-cols-1 gap-x-4 gap-y-1 text-xs sm:grid-cols-2 lg:grid-cols-3">
      {items.map(([k, v]) => (
        <div key={k} className="flex gap-2">
          <dt className="w-20 shrink-0 text-[10px] uppercase tracking-wide text-slate-500">
            {k}
          </dt>
          <dd className="min-w-0 truncate font-mono text-slate-200" title={v}>
            {v}
          </dd>
        </div>
      ))}
    </dl>
  );
}

function HardwareMemTable({ mods }: { mods: HardwareMemoryModule[] }) {
  // Operators want the at-a-glance answer "how much RAM, what
  // speed?" before they care about per-DIMM specifics. Provide a
  // total in the section header.
  const total = mods.reduce((s, m) => s + (m.sizeBytes ?? 0), 0);
  const speeds = Array.from(
    new Set(mods.map((m) => m.speedMhz).filter((n): n is number => !!n)),
  );
  return (
    <Section
      title={`Memory · ${formatBytes(total)}${speeds.length > 0 ? ` · ${speeds.join("/")} MHz` : ""}`}
    >
      <Table head={["Slot", "Size", "Type", "Mfg", "Part #", "Serial"]}>
        {mods.map((m, i) => (
          <tr key={i} className="border-t border-ink-800 align-top">
            <td className="px-3 py-1 font-mono text-[11px] text-slate-300">
              {m.slot || "—"}
            </td>
            <td className="px-3 py-1 text-[11px] tabular-nums">
              {m.sizeBytes ? formatBytes(m.sizeBytes) : "—"}
              {m.speedMhz ? <div className="text-[9px] text-slate-500">{m.speedMhz} MHz</div> : null}
            </td>
            <td className="px-3 py-1 text-[11px] text-slate-400">
              {joinNonEmpty([m.type, m.formFactor], " ") || "—"}
            </td>
            <td className="px-3 py-1 text-[11px] text-slate-400">
              {m.manufacturer || "—"}
            </td>
            <td className="px-3 py-1 font-mono text-[11px] text-slate-300">
              {m.partNumber || "—"}
            </td>
            <td className="px-3 py-1 font-mono text-[10px] text-slate-500">
              {m.serialNumber || "—"}
            </td>
          </tr>
        ))}
      </Table>
    </Section>
  );
}

function HardwareDiskTable({ disks }: { disks: HardwareDisk[] }) {
  const total = disks.reduce((s, d) => s + (d.sizeBytes ?? 0), 0);
  return (
    <Section title={`Storage · ${formatBytes(total)} raw`}>
      <Table head={["Device", "Model", "Bus", "Size", "Type", "Serial"]}>
        {disks.map((d, i) => (
          <tr key={i} className="border-t border-ink-800 align-top">
            <td className="px-3 py-1 font-mono text-[11px] text-slate-300">
              {d.device || "—"}
            </td>
            <td className="px-3 py-1 text-[11px]">
              <div>{d.model || "—"}</div>
              {d.vendor && (
                <div className="text-[9px] text-slate-500">{d.vendor}</div>
              )}
            </td>
            <td className="px-3 py-1 text-[11px] uppercase text-slate-400">
              {d.busType || "—"}
            </td>
            <td className="px-3 py-1 text-[11px] tabular-nums">
              {d.sizeBytes ? formatBytes(d.sizeBytes) : "—"}
            </td>
            <td className="px-3 py-1 text-[11px] text-slate-400">
              {d.rotational == null ? "—" : d.rotational ? "HDD" : "SSD"}
            </td>
            <td className="px-3 py-1 font-mono text-[10px] text-slate-500">
              {d.serial || "—"}
            </td>
          </tr>
        ))}
      </Table>
    </Section>
  );
}

function HardwareNICTable({ nics }: { nics: HardwareNIC[] }) {
  return (
    <Section title="Network adapters (hardware)">
      <Table head={["Name", "Vendor / Product", "Driver", "Speed", "MAC"]}>
        {nics.map((n, i) => (
          <tr key={i} className="border-t border-ink-800 align-top">
            <td className="px-3 py-1 font-mono text-[11px] text-slate-300">
              {n.name || "—"}
            </td>
            <td className="px-3 py-1 text-[11px]">
              <div>{n.product || "—"}</div>
              {n.vendor && (
                <div className="text-[9px] text-slate-500">{n.vendor}</div>
              )}
            </td>
            <td className="px-3 py-1 font-mono text-[11px] text-slate-400">
              {n.driver || "—"}
            </td>
            <td className="px-3 py-1 text-[11px] tabular-nums text-slate-400">
              {n.speedMbps ? `${n.speedMbps} Mbps` : "—"}
            </td>
            <td className="px-3 py-1 font-mono text-[10px] text-slate-500">
              {n.mac || "—"}
            </td>
          </tr>
        ))}
      </Table>
    </Section>
  );
}

function HardwareGPUTable({ gpus }: { gpus: HardwareGPU[] }) {
  return (
    <Section title="GPUs">
      <Table head={["Vendor", "Product", "Driver"]}>
        {gpus.map((g, i) => (
          <tr key={i} className="border-t border-ink-800 align-top">
            <td className="px-3 py-1 text-[11px] text-slate-400">{g.vendor || "—"}</td>
            <td className="px-3 py-1 text-[11px]">{g.product || "—"}</td>
            <td className="px-3 py-1 font-mono text-[11px] text-slate-400">
              {g.driver || "—"}
            </td>
          </tr>
        ))}
      </Table>
    </Section>
  );
}

function joinNonEmpty(parts: Array<string | number | undefined | null>, sep: string): string {
  return parts.filter((p) => p != null && p !== "").join(sep);
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

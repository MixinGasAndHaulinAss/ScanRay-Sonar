// Dashboard — the operator's "is everything OK?" landing page.
//
// Layout philosophy: every tile must answer one operational question
// at a glance. Counts go on the left as headline numbers; lists of
// things-needing-attention go on the right. We deliberately avoid
// nested charts here — anything time-series belongs on the per-device
// detail pages where it has real estate.
//
// The data sources (sites, agents, appliances, topology) are already
// cached by other pages, so opening Dashboard is virtually free for an
// operator who's been clicking around.

import { useMemo } from "react";
import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api } from "../api/client";
import type {
  Agent,
  Appliance,
  Site,
  Topology as TopologyT,
} from "../api/types";
import { formatBytes, formatRelative } from "../lib/format";

// AGENT_ONLINE_MS: how recently we must have heard from an agent before
// we count it as online. Probes send a snapshot every 60s, so 5min
// is "missed several heartbeats" — comfortably past a single dropped
// frame but not so long that an actual outage hides for an hour.
const AGENT_ONLINE_MS = 5 * 60_000;

export default function Dashboard() {
  const sites = useQuery({ queryKey: ["sites"], queryFn: () => api.get<Site[]>("/sites") });
  const agents = useQuery({
    queryKey: ["agents"],
    queryFn: () => api.get<Agent[]>("/agents"),
    refetchInterval: 30_000,
  });
  const appliances = useQuery({
    queryKey: ["appliances"],
    queryFn: () => api.get<Appliance[]>("/appliances"),
    refetchInterval: 30_000,
  });
  const topology = useQuery({
    queryKey: ["topology", false],
    queryFn: () => api.get<TopologyT>("/topology"),
    refetchInterval: 60_000,
  });

  // Roll the agent rows into an "online" count + a list of agents that
  // are stale or report a pending reboot. Memoized because the source
  // arrays come from React Query (new ref each fetch) and we don't want
  // to rebuild the lists on every parent rerender.
  const agentRollup = useMemo(() => {
    const list = agents.data ?? [];
    const now = Date.now();
    let online = 0;
    let pendingReboot = 0;
    const stale: Agent[] = [];
    const recent: Agent[] = [];
    for (const a of list) {
      const seen = a.lastSeenAt ? new Date(a.lastSeenAt).getTime() : 0;
      const isOnline = seen > 0 && now - seen < AGENT_ONLINE_MS;
      if (isOnline) online++;
      else if (a.isActive) stale.push(a);
      if (a.pendingReboot) pendingReboot++;
      recent.push(a);
    }
    recent.sort((a, b) => {
      const ta = a.lastSeenAt ? new Date(a.lastSeenAt).getTime() : 0;
      const tb = b.lastSeenAt ? new Date(b.lastSeenAt).getTime() : 0;
      return tb - ta;
    });
    return {
      total: list.length,
      online,
      pendingReboot,
      stale: stale.slice(0, 5),
      recent: recent.slice(0, 6),
    };
  }, [agents.data]);

  // Same idea for appliances: total, polling, recently-errored, and
  // the top utilization lines. Polling = had a successful poll within
  // 3 × pollInterval, the same threshold the detail page uses for the
  // "polling" pill.
  const applianceRollup = useMemo(() => {
    const list = appliances.data ?? [];
    const now = Date.now();
    let polling = 0;
    let errored = 0;
    let physTotal = 0;
    let physUp = 0;
    let uplinks = 0;
    const recentErrors: Appliance[] = [];
    for (const a of list) {
      const seen = a.lastPolledAt ? new Date(a.lastPolledAt).getTime() : 0;
      const window = Math.max(3 * a.pollIntervalSeconds, 180) * 1000;
      const polledRecently = seen > 0 && now - seen < window;
      if (polledRecently && !a.lastError) polling++;
      if (a.lastError) {
        errored++;
        recentErrors.push(a);
      }
      physTotal += a.physTotalCount ?? 0;
      physUp += a.physUpCount ?? 0;
      uplinks += a.uplinkCount ?? 0;
    }
    return {
      total: list.length,
      polling,
      errored,
      physTotal,
      physUp,
      uplinks,
      recentErrors: recentErrors.slice(0, 5),
    };
  }, [appliances.data]);

  const portsPct =
    applianceRollup.physTotal > 0
      ? (applianceRollup.physUp / applianceRollup.physTotal) * 100
      : null;

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-2xl font-semibold tracking-tight">Overview</h2>
        <p className="mt-1 text-sm text-slate-400">
          A unified view of your fleet — agents on hosts, SNMP-managed
          appliances, and the topology between them.
        </p>
      </div>

      <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
        <Tile
          to="/sites"
          label="Sites"
          value={sites.data?.length ?? "—"}
          accent="indigo"
        />
        <Tile
          to="/agents"
          label="Agents online"
          value={`${agentRollup.online} / ${agentRollup.total}`}
          subtitle={
            agentRollup.pendingReboot > 0
              ? `${agentRollup.pendingReboot} pending reboot`
              : "in the last 5 minutes"
          }
          accent={
            agentRollup.total === 0
              ? "slate"
              : agentRollup.online === agentRollup.total
                ? "emerald"
                : agentRollup.online === 0
                  ? "red"
                  : "amber"
          }
        />
        <Tile
          to="/appliances"
          label="Appliances polling"
          value={`${applianceRollup.polling} / ${applianceRollup.total}`}
          subtitle={
            applianceRollup.errored > 0
              ? `${applianceRollup.errored} errored`
              : applianceRollup.total === 0
                ? "none yet"
                : "all healthy"
          }
          accent={
            applianceRollup.total === 0
              ? "slate"
              : applianceRollup.errored > 0
                ? "amber"
                : applianceRollup.polling === applianceRollup.total
                  ? "emerald"
                  : "amber"
          }
        />
        <Tile
          to="/appliances"
          label="Physical ports up"
          value={
            applianceRollup.physTotal > 0
              ? `${applianceRollup.physUp} / ${applianceRollup.physTotal}`
              : "—"
          }
          subtitle={
            portsPct == null
              ? "no SNMP polls yet"
              : `${portsPct.toFixed(0)}% utilized · ${applianceRollup.uplinks} uplinks`
          }
          accent="sonar"
        />
      </div>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
        <RecentAgents agents={agentRollup.recent} loading={agents.isLoading} />
        <Attention
          appliances={applianceRollup.recentErrors}
          staleAgents={agentRollup.stale}
        />
        <TopologySummary
          topology={topology.data}
          loading={topology.isLoading}
        />
      </div>

      {sites.data?.length === 0 && (
        <div className="rounded-xl border border-sonar-900/60 bg-sonar-950/30 p-5 text-sm text-sonar-200">
          <div className="font-semibold">Welcome to ScanRay Sonar.</div>
          <p className="mt-1 text-sonar-300/80">
            Get started by creating a{" "}
            <Link to="/sites" className="underline hover:text-sonar-200">
              site
            </Link>
            , then enroll{" "}
            <Link to="/agents" className="underline hover:text-sonar-200">
              agents
            </Link>{" "}
            or add{" "}
            <Link to="/appliances" className="underline hover:text-sonar-200">
              appliances
            </Link>
            .
          </p>
        </div>
      )}
    </div>
  );
}

// ---- Tile ---------------------------------------------------------------

const ACCENT_CLASSES: Record<string, string> = {
  emerald: "border-emerald-700/40 bg-emerald-900/10",
  amber: "border-amber-700/40 bg-amber-900/10",
  red: "border-red-700/40 bg-red-900/10",
  sonar: "border-sonar-700/40 bg-sonar-950/30",
  indigo: "border-indigo-800/40 bg-indigo-950/30",
  slate: "border-ink-800 bg-ink-900",
};

function Tile({
  to,
  label,
  value,
  subtitle,
  accent = "slate",
}: {
  to: string;
  label: string;
  value: string | number;
  subtitle?: string;
  accent?: keyof typeof ACCENT_CLASSES;
}) {
  return (
    <Link
      to={to}
      className={`group rounded-xl border p-4 shadow-sm transition-colors hover:border-sonar-700/60 ${ACCENT_CLASSES[accent]}`}
    >
      <div className="text-xs uppercase tracking-wide text-slate-400">
        {label}
      </div>
      <div className="mt-1 text-2xl font-semibold tracking-tight text-white">
        {value}
      </div>
      {subtitle && (
        <div className="mt-1 text-xs text-slate-400 group-hover:text-slate-300">
          {subtitle}
        </div>
      )}
    </Link>
  );
}

// ---- Recent agents ------------------------------------------------------

function RecentAgents({
  agents,
  loading,
}: {
  agents: Agent[];
  loading: boolean;
}) {
  return (
    <Card title="Recent agents" link={{ to: "/agents", label: "All agents →" }}>
      {loading && <Empty text="Loading…" />}
      {!loading && agents.length === 0 && (
        <Empty text="No agents enrolled yet." />
      )}
      <ul className="divide-y divide-ink-800">
        {agents.map((a) => {
          const seen = a.lastSeenAt ? new Date(a.lastSeenAt).getTime() : 0;
          const online = seen > 0 && Date.now() - seen < AGENT_ONLINE_MS;
          return (
            <li key={a.id}>
              <Link
                to={`/agents/${a.id}`}
                className="flex items-center justify-between gap-3 px-3 py-2 text-sm hover:bg-ink-800/40"
              >
                <div className="flex min-w-0 items-center gap-2">
                  <span
                    className={`inline-block h-2 w-2 flex-none rounded-full ${
                      online ? "bg-emerald-400" : "bg-slate-600"
                    }`}
                    title={online ? "online" : "stale"}
                  />
                  <span className="truncate font-medium text-slate-100">
                    {a.hostname}
                  </span>
                  <span className="truncate text-xs text-slate-500">
                    {a.os}
                    {a.pendingReboot && (
                      <span className="ml-1 rounded bg-amber-900/40 px-1 py-0.5 text-[10px] uppercase tracking-wide text-amber-200">
                        reboot
                      </span>
                    )}
                  </span>
                </div>
                <div className="flex items-center gap-3 text-xs text-slate-500">
                  {a.cpuPct != null && (
                    <span className="tabular-nums">{a.cpuPct.toFixed(0)}% cpu</span>
                  )}
                  {a.memUsedBytes != null && (
                    <span className="tabular-nums">
                      {formatBytes(a.memUsedBytes)}
                    </span>
                  )}
                  <span className="tabular-nums">{formatRelative(a.lastSeenAt)}</span>
                </div>
              </Link>
            </li>
          );
        })}
      </ul>
    </Card>
  );
}

// ---- Attention list -----------------------------------------------------

function Attention({
  appliances,
  staleAgents,
}: {
  appliances: Appliance[];
  staleAgents: Agent[];
}) {
  const empty = appliances.length === 0 && staleAgents.length === 0;
  return (
    <Card title="Needs attention">
      {empty && (
        <Empty text="Nothing on fire. Last poll succeeded everywhere." />
      )}
      {appliances.length > 0 && (
        <div>
          <div className="px-3 pb-1 pt-2 text-[10px] uppercase tracking-wider text-slate-500">
            Appliance errors
          </div>
          <ul className="divide-y divide-ink-800">
            {appliances.map((a) => (
              <li key={a.id}>
                <Link
                  to={`/appliances/${a.id}`}
                  className="block px-3 py-2 text-sm hover:bg-ink-800/40"
                >
                  <div className="flex items-center justify-between gap-2">
                    <span className="font-medium text-red-200">{a.name}</span>
                    <span className="text-xs text-slate-500">
                      {formatRelative(a.lastPolledAt)}
                    </span>
                  </div>
                  <div className="mt-0.5 truncate text-xs text-red-300/80">
                    {a.lastError}
                  </div>
                </Link>
              </li>
            ))}
          </ul>
        </div>
      )}
      {staleAgents.length > 0 && (
        <div>
          <div className="px-3 pb-1 pt-2 text-[10px] uppercase tracking-wider text-slate-500">
            Stale agents (no recent heartbeat)
          </div>
          <ul className="divide-y divide-ink-800">
            {staleAgents.map((a) => (
              <li key={a.id}>
                <Link
                  to={`/agents/${a.id}`}
                  className="flex items-center justify-between gap-3 px-3 py-2 text-sm hover:bg-ink-800/40"
                >
                  <span className="truncate text-slate-200">{a.hostname}</span>
                  <span className="text-xs text-slate-500">
                    last seen {formatRelative(a.lastSeenAt)}
                  </span>
                </Link>
              </li>
            ))}
          </ul>
        </div>
      )}
    </Card>
  );
}

// ---- Topology summary ---------------------------------------------------

function TopologySummary({
  topology,
  loading,
}: {
  topology: TopologyT | undefined;
  loading: boolean;
}) {
  const counts = useMemo(() => {
    if (!topology) return null;
    let managed = 0;
    let foreign = 0;
    let edges = topology.edges.length;
    for (const n of topology.nodes) {
      if (n.kind === "appliance") managed++;
      else foreign++;
    }
    return { managed, foreign, edges };
  }, [topology]);

  return (
    <Card
      title="Topology"
      link={{ to: "/topology", label: "Open map →" }}
    >
      {loading && <Empty text="Loading…" />}
      {!loading && (!topology || topology.nodes.length === 0) && (
        <Empty text="No appliances polled yet — topology populates after the first poll cycle." />
      )}
      {topology && counts && counts.managed > 0 && (
        <div className="space-y-3 px-3 py-3 text-sm">
          <Stat
            label="Managed appliances"
            value={counts.managed}
            sub={`${counts.foreign} foreign neighbors`}
          />
          <Stat
            label="Discovered links"
            value={counts.edges}
            sub="LLDP + CDP, deduped"
          />
          <div className="text-xs text-slate-500">
            Generated {formatRelative(topology.generatedAt)} ·{" "}
            <Link to="/topology" className="text-sonar-400 hover:underline">
              view map
            </Link>
          </div>
        </div>
      )}
    </Card>
  );
}

function Stat({
  label,
  value,
  sub,
}: {
  label: string;
  value: number | string;
  sub: string;
}) {
  return (
    <div>
      <div className="flex items-baseline justify-between gap-3">
        <span className="text-xs uppercase tracking-wide text-slate-500">
          {label}
        </span>
        <span className="text-lg font-semibold tabular-nums text-slate-100">
          {value}
        </span>
      </div>
      <div className="text-xs text-slate-500">{sub}</div>
    </div>
  );
}

// ---- Card primitive ------------------------------------------------------

function Card({
  title,
  link,
  children,
}: {
  title: string;
  link?: { to: string; label: string };
  children: React.ReactNode;
}) {
  return (
    <div className="overflow-hidden rounded-xl border border-ink-800 bg-ink-900 shadow-sm">
      <div className="flex items-center justify-between border-b border-ink-800 px-3 py-2">
        <div className="text-sm font-semibold text-slate-200">{title}</div>
        {link && (
          <Link to={link.to} className="text-xs text-sonar-400 hover:underline">
            {link.label}
          </Link>
        )}
      </div>
      <div>{children}</div>
    </div>
  );
}

function Empty({ text }: { text: string }) {
  return <div className="px-3 py-6 text-center text-xs text-slate-500">{text}</div>;
}

// Overview — the default landing view of the Agents page. Pulls a
// little from each of the deeper dashboards so an operator can see
// "is everything fine?" at a glance without paging through the
// dropdown.

import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { api } from "../../api/client";
import type {
  Agent,
  OverviewDevicesPerformanceResponse,
  OverviewUserExperienceResponse,
} from "../../api/types";
import { formatRelative } from "../../lib/format";
import ApplicationsPerformance from "./ApplicationsPerformance";
import { Card, EmptyHint, ErrorHint, KPITile } from "./common";
import DevicesAverages from "./DevicesAverages";
import DevicesPerformance from "./DevicesPerformance";
import NetworkLatency from "./NetworkLatency";
import NetworkPerformance from "./NetworkPerformance";
import UserExperience from "./UserExperience";

export type OverviewPanel =
  | "home"
  | "user-experience"
  | "applications"
  | "network-latency"
  | "network-performance"
  | "devices-performance"
  | "devices-averages";

const PANELS: { id: OverviewPanel; label: string }[] = [
  { id: "home", label: "Summary" },
  { id: "user-experience", label: "User experience" },
  { id: "applications", label: "Applications" },
  { id: "network-latency", label: "Latency" },
  { id: "network-performance", label: "Network" },
  { id: "devices-performance", label: "Device performance" },
  { id: "devices-averages", label: "Averages" },
];

interface OverviewProps {
  panel: OverviewPanel;
  onPanel: (p: OverviewPanel) => void;
}

export default function Overview({ panel, onPanel }: OverviewProps) {
  return (
    <div className="space-y-4">
      <div className="flex flex-wrap gap-1">
        {PANELS.map((p) => (
          <button
            key={p.id}
            type="button"
            onClick={() => onPanel(p.id)}
            className={
              "rounded-md px-2.5 py-1 text-xs font-medium transition " +
              (panel === p.id
                ? "bg-sonar-600/30 text-sonar-200 ring-1 ring-sonar-500/40"
                : "text-slate-400 hover:bg-ink-800 hover:text-slate-200")
            }
          >
            {p.label}
          </button>
        ))}
      </div>
      {panel === "home" && <OverviewHome />}
      {panel === "user-experience" && <UserExperience />}
      {panel === "applications" && <ApplicationsPerformance />}
      {panel === "network-latency" && <NetworkLatency />}
      {panel === "network-performance" && <NetworkPerformance />}
      {panel === "devices-performance" && <DevicesPerformance />}
      {panel === "devices-averages" && <DevicesAverages />}
    </div>
  );
}

function OverviewHome() {
  const agents = useQuery({
    queryKey: ["agents"],
    queryFn: () => api.get<Agent[]>("/agents"),
    refetchInterval: 30_000,
  });
  const perf = useQuery({
    queryKey: ["overview", "devices-performance"],
    queryFn: () =>
      api.get<OverviewDevicesPerformanceResponse>("/agents/overview/devices-performance"),
    refetchInterval: 60_000,
  });
  const ux = useQuery({
    queryKey: ["overview", "user-experience"],
    queryFn: () =>
      api.get<OverviewUserExperienceResponse>("/agents/overview/user-experience"),
    refetchInterval: 60_000,
  });

  if (agents.isLoading) return <EmptyHint>Loading overview…</EmptyHint>;
  if (agents.isError) return <ErrorHint>Failed to load agents.</ErrorHint>;

  const list = agents.data ?? [];
  const total = list.length;
  const online = list.filter(
    (a) => a.lastSeenAt && Date.now() - new Date(a.lastSeenAt).getTime() < 5 * 60_000,
  ).length;
  const pendingReboot = list.filter((a) => a.pendingReboot).length;
  const lowDisk = list.filter((a) => {
    if (!a.rootDiskUsedBytes || !a.rootDiskTotalBytes) return false;
    const pct = (Number(a.rootDiskUsedBytes) / Number(a.rootDiskTotalBytes)) * 100;
    return pct >= 95;
  }).length;
  const recentlySeen = [...list]
    .filter((a) => a.lastSeenAt)
    .sort(
      (a, b) =>
        new Date(b.lastSeenAt!).getTime() - new Date(a.lastSeenAt!).getTime(),
    )
    .slice(0, 8);

  return (
    <div className="space-y-4">
      <div className="grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-5">
        <KPITile label="Total devices" value={total} />
        <KPITile
          label="Online (5m)"
          value={online}
          tone={online === total ? "good" : online === 0 ? "bad" : "warn"}
        />
        <KPITile
          label="Pending reboot"
          value={pendingReboot}
          tone={pendingReboot > 0 ? "warn" : "neutral"}
        />
        <KPITile
          label="Low free disk"
          value={lowDisk}
          tone={lowDisk > 0 ? "bad" : "neutral"}
        />
        <KPITile
          label="Avg device score"
          value={ux.data?.averageScore != null ? ux.data.averageScore.toFixed(1) : "—"}
          tone={
            ux.data?.averageScore == null
              ? "neutral"
              : ux.data.averageScore >= 8
                ? "good"
                : ux.data.averageScore >= 5
                  ? "warn"
                  : "bad"
          }
        />
      </div>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <Card title="Recently active hosts">
          {recentlySeen.length === 0 ? (
            <EmptyHint>No agents have phoned home yet.</EmptyHint>
          ) : (
            <ul className="divide-y divide-ink-800/60 text-sm">
              {recentlySeen.map((a) => (
                <li key={a.id} className="flex items-baseline justify-between py-1.5">
                  <Link
                    to={`/agents/${a.id}`}
                    className="truncate text-sonar-300 hover:underline"
                  >
                    {a.hostname}
                  </Link>
                  <span className="ml-3 shrink-0 text-xs tabular-nums text-slate-500">
                    {formatRelative(a.lastSeenAt)}
                  </span>
                </li>
              ))}
            </ul>
          )}
        </Card>
        <Card title="Managed devices by OS">
          {!perf.data ? (
            <EmptyHint>Loading device breakdown…</EmptyHint>
          ) : (
            <ul className="space-y-1.5">
              {Object.entries(perf.data.managedDevicesByOS).map(([os, count]) => (
                <li key={os} className="flex items-baseline justify-between text-sm">
                  <span className="capitalize text-slate-200">{os || "unknown"}</span>
                  <span className="tabular-nums text-slate-400">{count}</span>
                </li>
              ))}
              {Object.keys(perf.data.managedDevicesByOS).length === 0 && (
                <div className="text-xs text-slate-500">No agents enrolled.</div>
              )}
            </ul>
          )}
        </Card>
      </div>
    </div>
  );
}

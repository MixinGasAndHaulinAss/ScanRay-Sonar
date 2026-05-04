// NetworkPerformance — "Network · Performance" overview.
//
// Layout from the screenshot:
//   * WiFi vs Wired traffic split (donut → we render a 2-bar inline
//     comparison + raw byte totals to avoid pulling a chart lib).
//   * Hourly Sent / Received MB across the fleet.
//   * Four KPI tiles for high-latency hosts and average latency by
//     adapter type, plus the average WiFi signal.
//   * Latency-by-adapter trend (wifi vs wired, 24h).
//   * Top / bottom 5 ISPs by latency.

import { useQuery } from "@tanstack/react-query";
import { useMemo } from "react";
import { api } from "../../api/client";
import type { OverviewNetworkPerformanceResponse } from "../../api/types";
import LineChart from "../../components/LineChart";
import { formatBytes } from "../../lib/format";
import { Card, EmptyHint, ErrorHint, KPITile } from "./common";

export default function NetworkPerformance() {
  const q = useQuery({
    queryKey: ["overview", "network-performance"],
    queryFn: () =>
      api.get<OverviewNetworkPerformanceResponse>("/agents/overview/network-performance"),
    refetchInterval: 60_000,
  });

  // NOTE: All hooks (useMemo) MUST be called before any early return,
  // otherwise React error #300 fires when data toggles between
  // loading and loaded states. Defaults below let the hooks run
  // safely even before q.data is available.
  const hourlyMB = q.data?.hourlyMB ?? [];
  const latencyByAdapter = q.data?.latencyByAdapter ?? [];

  const hourlyTimes = useMemo(() => hourlyMB.map((h) => h.hour), [hourlyMB]);
  const hourlyIn = useMemo(() => hourlyMB.map((h) => h.inMB), [hourlyMB]);
  const hourlyOut = useMemo(() => hourlyMB.map((h) => h.outMB), [hourlyMB]);

  const lbaTimes = useMemo(() => latencyByAdapter.map((p) => p.hour), [latencyByAdapter]);
  const lbaWifi = useMemo(() => latencyByAdapter.map((p) => p.wifi ?? 0), [latencyByAdapter]);
  const lbaWired = useMemo(() => latencyByAdapter.map((p) => p.wired ?? 0), [latencyByAdapter]);

  if (q.isLoading) return <EmptyHint>Loading performance…</EmptyHint>;
  if (q.isError || !q.data) return <ErrorHint>Failed to load Network Performance.</ErrorHint>;

  const {
    adapterSplit,
    highLatencyDevices,
    avgWiredLatencyMs,
    avgWiFiLatencyMs,
    avgWiFiSignalPct,
    topISPsByLatency,
    bottomISPsByLatency,
  } = q.data;

  const totalBytes = adapterSplit.wifiBytes24h + adapterSplit.wiredBytes24h;
  const wifiPct = totalBytes > 0 ? (adapterSplit.wifiBytes24h / totalBytes) * 100 : 0;
  const wiredPct = totalBytes > 0 ? (adapterSplit.wiredBytes24h / totalBytes) * 100 : 0;

  return (
    <div className="space-y-4">
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
        <Card
          title="WiFi vs Wired traffic"
          subtitle={`${adapterSplit.deviceCount} devices · 24h`}
        >
          {totalBytes === 0 ? (
            <EmptyHint>No bytes recorded in the last 24h.</EmptyHint>
          ) : (
            <div className="space-y-3">
              <div>
                <div className="flex items-baseline justify-between text-xs">
                  <span className="text-slate-300">WiFi</span>
                  <span className="tabular-nums text-slate-400">
                    {wifiPct.toFixed(1)}% · {formatBytes(adapterSplit.wifiBytes24h)}
                  </span>
                </div>
                <div className="mt-1 h-2 w-full overflow-hidden rounded bg-ink-800">
                  <div className="h-full bg-sky-500" style={{ width: `${wifiPct}%` }} />
                </div>
              </div>
              <div>
                <div className="flex items-baseline justify-between text-xs">
                  <span className="text-slate-300">Wired</span>
                  <span className="tabular-nums text-slate-400">
                    {wiredPct.toFixed(1)}% · {formatBytes(adapterSplit.wiredBytes24h)}
                  </span>
                </div>
                <div className="mt-1 h-2 w-full overflow-hidden rounded bg-ink-800">
                  <div className="h-full bg-emerald-500" style={{ width: `${wiredPct}%` }} />
                </div>
              </div>
            </div>
          )}
        </Card>

        <Card title="Hourly traffic" subtitle="MB · last 24h">
          {hourlyMB.length === 0 ? (
            <EmptyHint>No hourly aggregations yet.</EmptyHint>
          ) : (
            <LineChart
              times={hourlyTimes}
              series={[
                { label: "In", values: hourlyIn, color: "stroke-emerald-400 text-emerald-400" },
                { label: "Out", values: hourlyOut, color: "stroke-amber-400 text-amber-400" },
              ]}
              yMin={0}
              yUnit=" MB"
              height={140}
              ariaLabel="Hourly bytes in/out"
            />
          )}
        </Card>

        <Card title="Key insights" subtitle="across the fleet">
          <div className="grid grid-cols-2 gap-2">
            <KPITile
              label="High-latency devices"
              value={highLatencyDevices}
              tone={highLatencyDevices > 0 ? "warn" : "good"}
            />
            <KPITile
              label="Avg WiFi signal"
              value={`${avgWiFiSignalPct.toFixed(0)}%`}
              tone={
                avgWiFiSignalPct >= 70
                  ? "good"
                  : avgWiFiSignalPct >= 40
                    ? "warn"
                    : "bad"
              }
            />
            <KPITile
              label="Avg wired RTT"
              value={`${avgWiredLatencyMs.toFixed(1)} ms`}
              tone={avgWiredLatencyMs <= 30 ? "good" : avgWiredLatencyMs <= 80 ? "warn" : "bad"}
            />
            <KPITile
              label="Avg WiFi RTT"
              value={`${avgWiFiLatencyMs.toFixed(1)} ms`}
              tone={avgWiFiLatencyMs <= 30 ? "good" : avgWiFiLatencyMs <= 80 ? "warn" : "bad"}
            />
          </div>
        </Card>
      </div>

      <Card title="Latency by adapter" subtitle="ms · last 24h">
        {latencyByAdapter.length === 0 ? (
          <EmptyHint>No latency-by-adapter samples yet.</EmptyHint>
        ) : (
          <LineChart
            times={lbaTimes}
            series={[
              { label: "WiFi", values: lbaWifi, color: "stroke-sky-400 text-sky-400" },
              { label: "Wired", values: lbaWired, color: "stroke-emerald-400 text-emerald-400" },
            ]}
            yMin={0}
            yUnit=" ms"
            height={180}
            ariaLabel="Latency by adapter type"
          />
        )}
      </Card>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <Card title="Top ISPs by latency" subtitle="lowest avg ms">
          <ISPLatencyList rows={topISPsByLatency} />
        </Card>
        <Card title="Bottom ISPs by latency" subtitle="highest avg ms">
          <ISPLatencyList rows={bottomISPsByLatency} />
        </Card>
      </div>
    </div>
  );
}

function ISPLatencyList({
  rows,
}: {
  rows: { isp: string; count: number; avgMs: number }[];
}) {
  if (!rows || rows.length === 0) {
    return <EmptyHint>No ISP latency samples yet.</EmptyHint>;
  }
  return (
    <ul className="divide-y divide-ink-800/60 text-sm">
      {rows.map((r) => (
        <li key={r.isp} className="flex items-baseline justify-between py-1.5">
          <span className="truncate text-slate-200" title={r.isp}>
            {r.isp}
          </span>
          <span className="ml-3 shrink-0 tabular-nums text-slate-300">
            {r.avgMs.toFixed(1)} ms · {r.count}
          </span>
        </li>
      ))}
    </ul>
  );
}


// NetworkLatency — "Network · Latency" overview.
//
// Five panels mirroring the screenshot:
//   * Latency by device (top-N hosts by avg ms over the window).
//   * Latency grouped by ISP (geo_org).
//   * Top ISPs by host count.
//   * WiFi signal gauge — aggregate average across the fleet.
//   * Longest traceroute hops — placeholder until traceroute lands
//     (a follow-up of Phase 1.5 in the plan).

import { useQuery } from "@tanstack/react-query";
import { api } from "../../api/client";
import type { OverviewNetworkLatencyResponse } from "../../api/types";
import { Card, EmptyHint, ErrorHint, TopList } from "./common";

export default function NetworkLatency() {
  const q = useQuery({
    queryKey: ["overview", "network-latency"],
    queryFn: () => api.get<OverviewNetworkLatencyResponse>("/agents/overview/network-latency"),
    refetchInterval: 60_000,
  });

  if (q.isLoading) return <EmptyHint>Loading latency dashboard…</EmptyHint>;
  if (q.isError || !q.data) return <ErrorHint>Failed to load Network Latency.</ErrorHint>;

  const { latencyByDevice, latencyByISP, topISPs, wifiSignalAvgPct, longestTracerouteHops } = q.data;

  return (
    <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
      <Card title="Latency by device" subtitle="avg ms · top 10">
        <TopList rows={latencyByDevice} unit=" ms" emptyHint="No latency samples yet." />
      </Card>

      <Card title="Latency by ISP" subtitle="avg ms">
        {latencyByISP.length === 0 ? (
          <EmptyHint>No ISP-classified hosts. The API needs the GeoIP ASN database.</EmptyHint>
        ) : (
          <ul className="divide-y divide-ink-800/60 text-sm">
            {latencyByISP.map((r) => (
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
        )}
      </Card>

      <Card title="Top ISPs" subtitle="host count">
        {topISPs.length === 0 ? (
          <EmptyHint>No ISP data.</EmptyHint>
        ) : (
          <ul className="divide-y divide-ink-800/60 text-sm">
            {topISPs.map((r) => (
              <li key={r.isp} className="flex items-baseline justify-between py-1.5">
                <span className="truncate text-slate-200" title={r.isp}>
                  {r.isp}
                </span>
                <span className="ml-3 shrink-0 tabular-nums text-slate-400">{r.count}</span>
              </li>
            ))}
          </ul>
        )}
      </Card>

      <Card title="Average WiFi signal" subtitle="fleet-wide">
        <WiFiGauge pct={wifiSignalAvgPct} />
      </Card>

      <Card title="Longest traceroute hops" subtitle="coming soon">
        {longestTracerouteHops.length === 0 ? (
          <EmptyHint>
            Traceroute is deferred to a Phase&nbsp;1.5 follow-up. The probe currently only
            measures end-to-end ICMP RTT.
          </EmptyHint>
        ) : (
          <ul className="divide-y divide-ink-800/60 text-sm">
            {longestTracerouteHops.map((h) => (
              <li key={h.hostname} className="flex items-baseline justify-between py-1.5">
                <span className="truncate text-slate-200">{h.hostname}</span>
                <span className="ml-3 shrink-0 tabular-nums text-slate-300">{h.hops}</span>
              </li>
            ))}
          </ul>
        )}
      </Card>
    </div>
  );
}

function WiFiGauge({ pct }: { pct: number }) {
  if (pct == null || Number.isNaN(pct)) {
    return <EmptyHint>No WiFi-connected hosts have reported signal yet.</EmptyHint>;
  }
  const clamped = Math.max(0, Math.min(100, pct));
  const tone =
    clamped >= 70 ? "bg-emerald-500" : clamped >= 40 ? "bg-amber-500" : "bg-red-500";
  return (
    <div className="space-y-2">
      <div className="text-3xl font-semibold tabular-nums text-slate-100">
        {clamped.toFixed(0)}
        <span className="ml-1 text-sm text-slate-500">%</span>
      </div>
      <div className="h-2 w-full overflow-hidden rounded bg-ink-800">
        <div className={"h-full " + tone} style={{ width: `${clamped}%` }} />
      </div>
      <div className="text-[10px] text-slate-500">
        Mean of <code>health.wifiSignalPct</code> across reporting WiFi hosts.
      </div>
    </div>
  );
}

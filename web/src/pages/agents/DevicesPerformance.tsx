// DevicesPerformance — "Devices · Performance" overview view.
//
// Layout (left to right, top to bottom in the screenshots):
//   * Managed-devices-by-OS donut (we render as a small bar list).
//   * 24h device-score trend line.
//   * 12 KPI tiles for the things operators care about most.
//   * Top-5 / Bottom-5 models by composite score.
//   * Geographic map (currently rendered as a simple list with
//     coordinates — react-simple-maps is already a dep; we wire
//     to it in a follow-up to keep this commit small).

import { useQuery } from "@tanstack/react-query";
import { useMemo } from "react";
import { api } from "../../api/client";
import type { OverviewDevicesPerformanceResponse } from "../../api/types";
import LineChart from "../../components/LineChart";
import OverviewMiniMap from "../../components/OverviewMiniMap";
import { Card, EmptyHint, ErrorHint, KPITile } from "./common";

export default function DevicesPerformance() {
  const q = useQuery({
    queryKey: ["overview", "devices-performance"],
    queryFn: () =>
      api.get<OverviewDevicesPerformanceResponse>("/agents/overview/devices-performance"),
    refetchInterval: 60_000,
  });

  // NOTE: All hooks (useMemo) MUST be called before any early return,
  // otherwise React error #300 fires when data toggles between
  // loading and loaded states.
  const scoreTrendData = q.data?.scoreTrend ?? [];
  const trendTimes = useMemo(() => scoreTrendData.map((t) => t.hour), [scoreTrendData]);
  const trendScores = useMemo(() => scoreTrendData.map((t) => t.score), [scoreTrendData]);

  if (q.isLoading) return <EmptyHint>Loading performance dashboard…</EmptyHint>;
  if (q.isError || !q.data) return <ErrorHint>Failed to load Devices Performance.</ErrorHint>;
  const { managedDevicesByOS, keyDeviceInsights, top5Models, bottom5Models, map, scoreTrend } = q.data;

  const totalDevices = Object.values(managedDevicesByOS).reduce((s, n) => s + n, 0);

  const insights: { label: string; key: string; tone?: "good" | "warn" | "bad" }[] = [
    { label: "BSODs (24h)",          key: "bsodCount",            tone: "bad" },
    { label: "Missing patches",      key: "missingPatchCount",    tone: "warn" },
    { label: "User reboots (24h)",   key: "userRebootCount",      tone: "warn" },
    { label: "App crashes (24h)",    key: "appCrashCount",        tone: "warn" },
    { label: "High-load CPU",        key: "highloadCpuIncidents", tone: "warn" },
    { label: "Low battery health",   key: "lowBatteryHealth",     tone: "warn" },
    { label: "Win 30+ day uptime",   key: "winUptime30d",         tone: "warn" },
    { label: "Low RAM (>90%)",       key: "lowRAM",               tone: "warn" },
    { label: "Low free disk (<5%)",  key: "lowFreeDisk",          tone: "warn" },
    { label: "Low device score",     key: "lowDeviceScore",       tone: "bad" },
  ];

  return (
    <div className="space-y-4">
      <div className="grid grid-cols-1 gap-4 lg:grid-cols-3">
        <Card title="Managed devices" subtitle={`${totalDevices} total`}>
          {totalDevices === 0 ? (
            <div className="text-xs text-slate-500">No agents enrolled.</div>
          ) : (
            <ul className="space-y-1.5">
              {Object.entries(managedDevicesByOS).map(([os, count]) => {
                const pct = (count / Math.max(1, totalDevices)) * 100;
                return (
                  <li key={os} className="space-y-1">
                    <div className="flex items-baseline justify-between text-xs">
                      <span className="capitalize text-slate-200">{os || "unknown"}</span>
                      <span className="tabular-nums text-slate-400">{count}</span>
                    </div>
                    <div className="h-1.5 w-full overflow-hidden rounded bg-ink-800">
                      <div
                        className="h-full bg-sonar-500"
                        style={{ width: `${pct}%` }}
                      />
                    </div>
                  </li>
                );
              })}
            </ul>
          )}
        </Card>

        <Card title="Average device score (24h)" subtitle="0–10">
          {scoreTrend.length === 0 ? (
            <EmptyHint>No telemetry yet.</EmptyHint>
          ) : (
            <LineChart
              times={trendTimes}
              series={[{ label: "Score", values: trendScores, color: "stroke-emerald-400 text-emerald-400" }]}
              yMin={0}
              yMax={10}
              height={160}
              ariaLabel="Device score trend"
            />
          )}
        </Card>

        <Card title="Geo map" subtitle={`${map.length} located`}>
          {map.length === 0 ? (
            <div className="text-xs text-slate-500">
              No GeoIP-located hosts. The probe needs <code>publicIp</code>{" "}
              and the API needs the MaxMind databases mounted under
              <code> /var/lib/sonar-geoip</code>.
            </div>
          ) : (
            <OverviewMiniMap hosts={map} height={220} />
          )}
        </Card>
      </div>

      <Card title="Key device insights">
        <div className="grid grid-cols-2 gap-2 sm:grid-cols-3 lg:grid-cols-5">
          {insights.map((i) => {
            const v = keyDeviceInsights[i.key] ?? 0;
            return (
              <KPITile
                key={i.key}
                label={i.label}
                value={v}
                tone={v === 0 ? "neutral" : i.tone}
              />
            );
          })}
        </div>
      </Card>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <Card title="Top 5 models by score">
          {top5Models.length === 0 ? (
            <EmptyHint>No model data — probe HardwareCollector hasn't run yet.</EmptyHint>
          ) : (
            <table className="w-full text-left text-sm">
              <thead className="text-xs uppercase tracking-wide text-slate-500">
                <tr>
                  <th className="px-2 py-1">Model</th>
                  <th className="px-2 py-1 text-right">Count</th>
                  <th className="px-2 py-1 text-right">Score</th>
                </tr>
              </thead>
              <tbody>
                {top5Models.map((m) => (
                  <tr key={m.model} className="border-t border-ink-800">
                    <td className="px-2 py-1 text-slate-200">{m.model}</td>
                    <td className="px-2 py-1 text-right tabular-nums text-slate-400">{m.count}</td>
                    <td className="px-2 py-1 text-right tabular-nums text-emerald-300">{m.score.toFixed(1)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </Card>
        <Card title="Bottom 5 models by score">
          {bottom5Models.length === 0 ? (
            <EmptyHint>No model data.</EmptyHint>
          ) : (
            <table className="w-full text-left text-sm">
              <thead className="text-xs uppercase tracking-wide text-slate-500">
                <tr>
                  <th className="px-2 py-1">Model</th>
                  <th className="px-2 py-1 text-right">Count</th>
                  <th className="px-2 py-1 text-right">Score</th>
                </tr>
              </thead>
              <tbody>
                {bottom5Models.map((m) => (
                  <tr key={m.model} className="border-t border-ink-800">
                    <td className="px-2 py-1 text-slate-200">{m.model}</td>
                    <td className="px-2 py-1 text-right tabular-nums text-slate-400">{m.count}</td>
                    <td className="px-2 py-1 text-right tabular-nums text-amber-300">{m.score.toFixed(1)}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}
        </Card>
      </div>
    </div>
  );
}

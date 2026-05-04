// ApplicationsPerformance — "Applications · Performance" overview.
//
// Today the probe only reports an aggregate `appCrashCount24h` (from
// the Windows Application event log). Per-app breakdowns are deferred
// until WerFault report parsing lands. This view therefore shows:
//   * A summary KPI of total app crashes in the window.
//   * Top hosts by app-crash count (built from the same field).
//   * A "coverage" hint explaining what's still scaffolded.

import { useQuery } from "@tanstack/react-query";
import { api } from "../../api/client";
import type { OverviewApplicationsPerformanceResponse } from "../../api/types";
import { Card, EmptyHint, ErrorHint, KPITile, TopList } from "./common";

export default function ApplicationsPerformance() {
  const q = useQuery({
    queryKey: ["overview", "applications-performance"],
    queryFn: () =>
      api.get<OverviewApplicationsPerformanceResponse>(
        "/agents/overview/applications-performance",
      ),
    refetchInterval: 60_000,
  });

  if (q.isLoading) return <EmptyHint>Loading applications dashboard…</EmptyHint>;
  if (q.isError || !q.data) return <ErrorHint>Failed to load Applications Performance.</ErrorHint>;
  const { coverage, summary, mostCrashes } = q.data;

  return (
    <div className="space-y-4">
      <div className="grid grid-cols-1 gap-3 md:grid-cols-3">
        <KPITile
          label="App crashes (24h)"
          value={summary.totalCrashes24h}
          tone={summary.totalCrashes24h > 0 ? "warn" : "good"}
        />
        <KPITile label="Reporting devices" value={summary.deviceCount} />
        <KPITile
          label="Per-app breakdown"
          value={coverage.perAppBreakdown ? "available" : "not yet"}
          tone={coverage.perAppBreakdown ? "good" : "neutral"}
        />
      </div>

      <Card title="Most crashes (24h)" subtitle="top 10 hosts">
        <TopList rows={mostCrashes} emptyHint="No crashes reported." />
      </Card>

      <Card title="Coverage" subtitle="what this dashboard shows today">
        <ul className="space-y-1.5 text-xs text-slate-400">
          <li>
            <strong className="text-slate-200">App crashes:</strong>{" "}
            {coverage.appCrashes
              ? "collected on Windows from the Application event log (Source = Application Error)."
              : "not yet collected — probe needs the v2.x rollout."}
          </li>
          <li>
            <strong className="text-slate-200">Per-app breakdown:</strong>{" "}
            {coverage.perAppBreakdown
              ? "available."
              : "deferred — WerFault report parsing is not implemented in this release."}
          </li>
          <li>
            <strong className="text-slate-200">App launches:</strong>{" "}
            {coverage.appLaunches ? "available." : "deferred — requires ETW StartProcess events."}
          </li>
        </ul>
      </Card>
    </div>
  );
}

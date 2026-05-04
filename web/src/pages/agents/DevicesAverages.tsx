// DevicesAverages — the "Devices · Averages" overview view.
//
// Six top-5 cards on the left plus six 24-hour trend lines on the
// right. The data comes from /agents/overview/devices-averages.
// All the heavy lifting (top-N sorts, hourly aggregations) happens
// server-side; the view just renders shapes.

import { useQuery } from "@tanstack/react-query";
import { useMemo } from "react";
import { api } from "../../api/client";
import type { OverviewDevicesAveragesResponse, OverviewTrendPoint } from "../../api/types";
import LineChart from "../../components/LineChart";
import { Card, EmptyHint, ErrorHint, TopList } from "./common";

export default function DevicesAverages() {
  const q = useQuery({
    queryKey: ["overview", "devices-averages"],
    queryFn: () =>
      api.get<OverviewDevicesAveragesResponse>("/agents/overview/devices-averages"),
    refetchInterval: 60_000,
  });

  if (q.isLoading) return <EmptyHint>Loading averages…</EmptyHint>;
  if (q.isError || !q.data) return <ErrorHint>Failed to load Devices Averages.</ErrorHint>;
  const { top, trends } = q.data;

  return (
    <div className="space-y-4">
      <div className="grid grid-cols-1 gap-3 md:grid-cols-2 lg:grid-cols-3">
        <Card title="Worst battery health" subtitle="Top 5">
          <TopList rows={top.worstBatteryHealth} unit="%" emptyHint="No battery hosts." />
        </Card>
        <Card title="Most BSODs (24h)" subtitle="Top 5">
          <TopList rows={top.mostBSODs} emptyHint="No BSODs reported." />
        </Card>
        <Card title="Most app crashes (24h)" subtitle="Top 5">
          <TopList rows={top.mostAppCrashes} emptyHint="No crashes reported." />
        </Card>
        <Card title="Least free disk %" subtitle="Top 5">
          <TopList rows={top.leastFreeDiskPct} unit="%" emptyHint="No data." />
        </Card>
        <Card title="Most missing patches" subtitle="Top 5">
          <TopList rows={top.mostMissingPatches} emptyHint="Probes haven't reported any yet." />
        </Card>
        <Card title="Most event-log errors (24h)" subtitle="Top 5">
          <TopList rows={top.mostEventLogErrors} emptyHint="No errors reported." />
        </Card>
      </div>

      <div className="grid grid-cols-1 gap-3 md:grid-cols-2 lg:grid-cols-3">
        <TrendCard title="Average CPU %"           unit="%"     data={trends.cpuPct} />
        <TrendCard title="CPU Queue Length"        unit=""      data={trends.cpuQueueLength} />
        <TrendCard title="Average Memory %"        unit="%"     data={trends.memPct} />
        <TrendCard title="Disk Queue Length"       unit=""      data={trends.diskQueueLength} />
        <TrendCard title="Network MB/s"            unit=" MB/s" data={trends.networkMBps} />
        <TrendCard title="Hourly Network MB"       unit=" MB"   data={trends.networkHourly} />
      </div>
    </div>
  );
}

function TrendCard({
  title,
  unit,
  data,
}: {
  title: string;
  unit: string;
  data: OverviewTrendPoint[];
}) {
  const times = useMemo(() => data.map((d) => d.hour), [data]);
  const values = useMemo(() => data.map((d) => d.value), [data]);
  return (
    <Card title={title} subtitle="last 24h">
      <LineChart
        times={times}
        series={[{ label: title, values, color: "stroke-sky-400 text-sky-400" }]}
        yMin={0}
        yUnit={unit}
        height={140}
        ariaLabel={title}
      />
    </Card>
  );
}

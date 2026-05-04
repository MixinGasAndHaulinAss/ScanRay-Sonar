// UserExperience — "User Experience" overview.
//
// Layout (top to bottom):
//   * KPI strip — average device score, devices scored, low-score
//     count.
//   * Score distribution histogram.
//   * Worst-5 / Best-5 hosts by composite score.
//   * Eight ranked top-N cards covering the questions operators most
//     often ask:
//       - most reboots (24h)
//       - average user input delay (placeholder)
//       - average logon time
//       - blue-screen counts (24h)
//       - longest app launch times (placeholder)
//       - highest CPU load
//       - longest logon times
//       - longest uptime in days
//   * A small inline notice for the placeholder cards explaining the
//     probe-side collector is not yet implemented (so operators don't
//     mistake "no rows" for a regression).

import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { api } from "../../api/client";
import type { OverviewTopRow, OverviewUserExperienceResponse } from "../../api/types";
import { Card, EmptyHint, ErrorHint, KPITile } from "./common";

const HISTOGRAM_BANDS = [
  "0–1", "1–2", "2–3", "3–4", "4–5", "5–6", "6–7", "7–8", "8–9", "9–10",
];

export default function UserExperience() {
  const q = useQuery({
    queryKey: ["overview", "user-experience"],
    queryFn: () => api.get<OverviewUserExperienceResponse>("/agents/overview/user-experience"),
    refetchInterval: 60_000,
  });

  if (q.isLoading) return <EmptyHint>Loading user-experience dashboard…</EmptyHint>;
  if (q.isError || !q.data) return <ErrorHint>Failed to load User Experience.</ErrorHint>;

  const { averageScore, histogram, worst, best, deviceCount, top } = q.data;
  const histMax = Math.max(1, ...histogram);

  return (
    <div className="space-y-4">
      <div className="grid grid-cols-1 gap-3 md:grid-cols-3">
        <KPITile
          label="Average score"
          value={averageScore.toFixed(1)}
          tone={
            averageScore >= 8 ? "good" : averageScore >= 5 ? "warn" : "bad"
          }
        />
        <KPITile label="Devices scored" value={deviceCount} />
        <KPITile
          label="Below 5.0"
          value={histogram.slice(0, 5).reduce((s, n) => s + n, 0)}
          tone="bad"
        />
      </div>

      <Card title="Score distribution" subtitle="0 (worst) — 10 (best)">
        {deviceCount === 0 ? (
          <EmptyHint>No agents have enough telemetry to score yet.</EmptyHint>
        ) : (
          <div className="grid grid-cols-10 gap-1 items-end h-32">
            {histogram.map((n, i) => {
              const h = (n / histMax) * 100;
              const tone =
                i >= 8 ? "bg-emerald-500"
                : i >= 5 ? "bg-sky-500"
                : i >= 3 ? "bg-amber-500"
                : "bg-red-500";
              return (
                <div key={i} className="flex flex-col items-center gap-1">
                  <div className="flex-1 w-full flex items-end">
                    <div
                      className={"w-full rounded-t " + tone}
                      style={{ height: `${h}%`, minHeight: n > 0 ? 2 : 0 }}
                      title={`${HISTOGRAM_BANDS[i]}: ${n}`}
                    />
                  </div>
                  <div className="text-[9px] tabular-nums text-slate-500">
                    {HISTOGRAM_BANDS[i]}
                  </div>
                </div>
              );
            })}
          </div>
        )}
      </Card>

      <div className="grid grid-cols-1 gap-4 lg:grid-cols-2">
        <Card title="Worst 5 hosts" subtitle="lowest score">
          {worst.length === 0 ? (
            <EmptyHint>No data.</EmptyHint>
          ) : (
            <ul className="divide-y divide-ink-800/60 text-sm">
              {worst.slice(0, 5).map((h) => (
                <li key={h.id} className="flex items-baseline justify-between py-1.5">
                  <Link to={`/agents/${h.id}`} className="truncate text-sonar-300 hover:underline">
                    {h.hostname}
                  </Link>
                  <span className="ml-3 shrink-0 tabular-nums text-red-300">
                    {h.score.toFixed(1)}
                  </span>
                </li>
              ))}
            </ul>
          )}
        </Card>
        <Card title="Best 5 hosts" subtitle="highest score">
          {best.length === 0 ? (
            <EmptyHint>No data.</EmptyHint>
          ) : (
            <ul className="divide-y divide-ink-800/60 text-sm">
              {best.slice(0, 5).map((h) => (
                <li key={h.id} className="flex items-baseline justify-between py-1.5">
                  <Link to={`/agents/${h.id}`} className="truncate text-sonar-300 hover:underline">
                    {h.hostname}
                  </Link>
                  <span className="ml-3 shrink-0 tabular-nums text-emerald-300">
                    {h.score.toFixed(1)}
                  </span>
                </li>
              ))}
            </ul>
          )}
        </Card>
      </div>

      <div className="grid grid-cols-1 gap-4 md:grid-cols-2 xl:grid-cols-4">
        <RankCard
          title="Most reboots"
          subtitle="last 24h"
          rows={top.mostReboots}
          unit=""
          tone="amber"
        />
        <RankCard
          title="Avg user input delay"
          subtitle="ms · not yet collected"
          rows={top.avgInputDelay}
          unit=" ms"
          tone="slate"
          placeholder={
            <>
              The probe does not yet sample the
              <code className="mx-1 rounded bg-ink-950 px-1 py-0.5 text-[10px]">
                Microsoft-Windows-DesktopWindowManager
              </code>
              input-latency counter. Coming in a follow-up.
            </>
          }
        />
        <RankCard
          title="Avg logon time"
          subtitle="ms · last 7 days"
          rows={top.longestLogonAvg}
          unit=" ms"
          tone="amber"
        />
        <RankCard
          title="Blue-screen count"
          subtitle="last 24h"
          rows={top.mostBSODs}
          unit=""
          tone="red"
        />
        <RankCard
          title="Longest app launch"
          subtitle="ms · not yet collected"
          rows={top.longestAppLaunch}
          unit=" ms"
          tone="slate"
          placeholder={
            <>
              Per-app launch latency requires an ETW collector. Reserved
              for a follow-up; the field is plumbed end-to-end so the card
              starts populating as soon as the probe ships data.
            </>
          }
        />
        <RankCard
          title="Highest CPU load"
          subtitle="current snapshot"
          rows={top.highestCPU}
          unit="%"
          tone="amber"
        />
        <RankCard
          title="Longest logon time"
          subtitle="max ms · last 7 days"
          rows={top.longestLogonMax}
          unit=" ms"
          tone="red"
        />
        <RankCard
          title="Longest uptime"
          subtitle="days since last reboot"
          rows={top.longestUptimeDays}
          unit=" d"
          tone="sky"
        />
      </div>
    </div>
  );
}

type Tone = "amber" | "red" | "sky" | "slate";

const TONE_CLASSES: Record<Tone, string> = {
  amber: "text-amber-300",
  red: "text-red-300",
  sky: "text-sky-300",
  slate: "text-slate-300",
};

function RankCard({
  title,
  subtitle,
  rows,
  unit,
  tone,
  placeholder,
}: {
  title: string;
  subtitle?: string;
  rows: OverviewTopRow[];
  unit: string;
  tone: Tone;
  placeholder?: React.ReactNode;
}) {
  return (
    <Card title={title} subtitle={subtitle}>
      {rows.length === 0 ? (
        <EmptyHint>{placeholder ?? "No data yet."}</EmptyHint>
      ) : (
        <ul className="divide-y divide-ink-800/60 text-sm">
          {rows.slice(0, 5).map((r) => (
            <li key={r.id} className="flex items-baseline justify-between py-1.5">
              <Link
                to={`/agents/${r.id}`}
                className="truncate text-sonar-300 hover:underline"
                title={r.hostname}
              >
                {r.hostname}
              </Link>
              <span className={"ml-3 shrink-0 tabular-nums " + TONE_CLASSES[tone]}>
                {formatValue(r.value, unit)}
              </span>
            </li>
          ))}
        </ul>
      )}
    </Card>
  );
}

function formatValue(v: number | null | undefined, unit: string): string {
  if (v == null) return "—";
  if (unit === "%") return v.toFixed(1) + "%";
  if (unit === " d") return v.toFixed(1) + " d";
  if (unit === " ms") return Math.round(v).toLocaleString() + " ms";
  if (!unit) return Math.round(v).toString();
  return v.toFixed(1) + unit;
}

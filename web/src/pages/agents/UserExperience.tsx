// UserExperience — "User Experience" overview.
//
// Renders the composite device-score distribution and the worst /
// best 5 hosts. The score formula lives server-side in
// internal/api/score.go; we just present the values here.

import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import { api } from "../../api/client";
import type { OverviewUserExperienceResponse } from "../../api/types";
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

  const { averageScore, histogram, worst, best, deviceCount } = q.data;
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
              {worst.map((h) => (
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
              {best.map((h) => (
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
    </div>
  );
}

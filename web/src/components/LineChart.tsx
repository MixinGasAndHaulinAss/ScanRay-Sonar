// LineChart — multi-series SVG chart for the new network + latency
// graphs and the overview trend lines. Designed to feel like
// Sparkline's bigger sibling: pure inline SVG, no chart library, no
// dependencies. ~150 lines is enough for everything the dashboards
// need (axes, time labels, legend, tooltip).
//
// Why not recharts / uplot / vis-x:
//   * Bundle size — we already keep the web bundle under 500 KB
//     gzipped and want to stay there.
//   * The dashboards need exactly two chart shapes (line and bar) and
//     ~150 LoC covers both.
//   * No transitive dependency churn (recharts pulls in d3 packages).
//
// Tooltip behaviour: hovering anywhere over the chart finds the
// nearest x-value and shows a small box with each series' y-value.
// We do NOT track mouse-leave to keep the tooltip persistent — it
// disappears as soon as the cursor leaves the SVG.

import { useMemo, useState } from "react";

export interface LineSeries {
  /** Display name shown in the legend + tooltip. */
  label: string;
  /** Tailwind class for the line stroke + legend swatch (e.g. "stroke-sky-400 text-sky-400"). */
  color?: string;
  /** Y values, same length as `times` on the parent component. */
  values: (number | null | undefined)[];
}

interface LineChartProps {
  /** Time values, one per index, used to label the X axis. */
  times: (Date | string)[];
  series: LineSeries[];
  width?: number;
  height?: number;
  /** Force a Y minimum; default is data min (or 0 if all values >= 0). */
  yMin?: number;
  /** Force a Y maximum; default is data max + a small headroom. */
  yMax?: number;
  /** Y-axis units shown after each tick label (e.g. "ms", "%", "MB"). */
  yUnit?: string;
  /** Number of Y axis gridlines (default 4). */
  yTicks?: number;
  className?: string;
  ariaLabel?: string;
}

const DEFAULT_COLORS = [
  "stroke-sky-400 text-sky-400",
  "stroke-amber-400 text-amber-400",
  "stroke-emerald-400 text-emerald-400",
  "stroke-rose-400 text-rose-400",
  "stroke-violet-400 text-violet-400",
];

export default function LineChart({
  times,
  series,
  width = 720,
  height = 240,
  yMin,
  yMax,
  yUnit = "",
  yTicks = 4,
  className,
  ariaLabel,
}: LineChartProps) {
  const [hoverIdx, setHoverIdx] = useState<number | null>(null);

  const padL = 44;
  const padR = 16;
  const padT = 12;
  const padB = 28;

  const inner = useMemo(() => {
    const w = Math.max(1, width - padL - padR);
    const h = Math.max(1, height - padT - padB);
    return { w, h };
  }, [width, height]);

  // Domain
  const { yLo, yHi } = useMemo(() => {
    let lo = Infinity;
    let hi = -Infinity;
    for (const s of series) {
      for (const v of s.values) {
        if (v == null) continue;
        if (v < lo) lo = v;
        if (v > hi) hi = v;
      }
    }
    if (!isFinite(lo)) lo = 0;
    if (!isFinite(hi)) hi = 1;
    if (lo === hi) hi = lo + 1;
    return {
      yLo: yMin ?? Math.min(0, lo),
      yHi: yMax ?? hi * 1.08,
    };
  }, [series, yMin, yMax]);

  if (!times.length || series.every((s) => s.values.every((v) => v == null))) {
    return (
      <div
        className={
          "grid place-items-center text-xs text-slate-600 " + (className ?? "")
        }
        style={{ width, height }}
      >
        no data
      </div>
    );
  }

  const xMax = Math.max(times.length - 1, 1);
  const x = (i: number) => padL + (i / xMax) * inner.w;
  const y = (v: number) =>
    padT + inner.h - ((v - yLo) / (yHi - yLo)) * inner.h;

  const ticks: number[] = [];
  for (let i = 0; i <= yTicks; i++) {
    ticks.push(yLo + ((yHi - yLo) * i) / yTicks);
  }

  // X-axis labels: show start, midpoint, end (and one or two more if
  // the chart is wide enough). Trade simplicity for readability.
  const xLabelIdxs = useMemo(() => {
    const idxs: number[] = [];
    const targetCount = Math.min(5, times.length);
    if (targetCount <= 1) return [0];
    for (let i = 0; i < targetCount; i++) {
      idxs.push(Math.round((i * (times.length - 1)) / (targetCount - 1)));
    }
    return idxs;
  }, [times.length]);

  const fmtTime = (d: Date | string) => {
    const dt = typeof d === "string" ? new Date(d) : d;
    if (Number.isNaN(dt.getTime())) return String(d);
    const hh = dt.getHours().toString().padStart(2, "0");
    const mm = dt.getMinutes().toString().padStart(2, "0");
    return `${hh}:${mm}`;
  };

  const linePaths = series.map((s, sIdx) => {
    const pts: string[] = [];
    s.values.forEach((v, i) => {
      if (v == null) {
        // Break the path so gaps render as gaps.
        if (pts.length && !pts[pts.length - 1].endsWith("M")) {
          pts.push("");
        }
        return;
      }
      const px = x(i).toFixed(1);
      const py = y(v).toFixed(1);
      const cmd = pts.length === 0 || pts[pts.length - 1] === "" ? "M" : "L";
      pts.push(`${cmd} ${px} ${py}`);
    });
    return {
      d: pts.filter(Boolean).join(" "),
      color: s.color ?? DEFAULT_COLORS[sIdx % DEFAULT_COLORS.length],
      label: s.label,
    };
  });

  const handleMove = (e: React.MouseEvent<SVGSVGElement>) => {
    const rect = e.currentTarget.getBoundingClientRect();
    const px = ((e.clientX - rect.left) / rect.width) * width;
    if (px < padL || px > padL + inner.w) {
      setHoverIdx(null);
      return;
    }
    const idx = Math.round(((px - padL) / inner.w) * xMax);
    setHoverIdx(Math.max(0, Math.min(times.length - 1, idx)));
  };

  return (
    <div className={"flex flex-col " + (className ?? "")}>
      <svg
        width={width}
        height={height}
        viewBox={`0 0 ${width} ${height}`}
        role="img"
        aria-label={ariaLabel}
        onMouseMove={handleMove}
        onMouseLeave={() => setHoverIdx(null)}
        className="select-none"
      >
        {/* Gridlines + Y labels */}
        {ticks.map((t, i) => (
          <g key={i}>
            <line
              x1={padL}
              x2={padL + inner.w}
              y1={y(t)}
              y2={y(t)}
              className="stroke-slate-800/60"
              strokeWidth="1"
              strokeDasharray="2 4"
            />
            <text
              x={padL - 6}
              y={y(t)}
              className="fill-slate-500 text-[10px]"
              textAnchor="end"
              dominantBaseline="middle"
            >
              {formatTick(t, yUnit)}
            </text>
          </g>
        ))}

        {/* X axis labels */}
        {xLabelIdxs.map((i) => (
          <text
            key={i}
            x={x(i)}
            y={height - padB + 14}
            className="fill-slate-500 text-[10px]"
            textAnchor="middle"
          >
            {fmtTime(times[i])}
          </text>
        ))}

        {/* Series */}
        {linePaths.map((p, i) => (
          <path
            key={i}
            d={p.d}
            className={p.color}
            strokeWidth="1.75"
            fill="none"
          />
        ))}

        {/* Hover crosshair + dots */}
        {hoverIdx !== null && (
          <>
            <line
              x1={x(hoverIdx)}
              x2={x(hoverIdx)}
              y1={padT}
              y2={padT + inner.h}
              className="stroke-slate-600/60"
              strokeWidth="1"
            />
            {series.map((s, sIdx) => {
              const v = s.values[hoverIdx];
              if (v == null) return null;
              return (
                <circle
                  key={sIdx}
                  cx={x(hoverIdx)}
                  cy={y(v)}
                  r="3"
                  className={
                    s.color ?? DEFAULT_COLORS[sIdx % DEFAULT_COLORS.length]
                  }
                  strokeWidth="1.5"
                  fill="currentColor"
                />
              );
            })}
          </>
        )}
      </svg>

      {/* Legend + tooltip */}
      <div className="flex flex-wrap items-center gap-x-4 gap-y-1 px-2 pt-1 text-[11px] text-slate-400">
        {series.map((s, i) => (
          <span
            key={i}
            className={
              "inline-flex items-center gap-1.5 " +
              (s.color ?? DEFAULT_COLORS[i % DEFAULT_COLORS.length])
            }
          >
            <span
              className="inline-block h-[2px] w-3 rounded-full"
              style={{ background: "currentColor" }}
            />
            <span className="text-slate-400">{s.label}</span>
            {hoverIdx !== null && (
              <span className="text-slate-200">
                {formatTick(s.values[hoverIdx] ?? 0, yUnit)}
              </span>
            )}
          </span>
        ))}
        {hoverIdx !== null && (
          <span className="ml-auto text-slate-500">
            {fmtTime(times[hoverIdx])}
          </span>
        )}
      </div>
    </div>
  );
}

function formatTick(v: number, unit: string): string {
  if (Math.abs(v) >= 1_000_000) return (v / 1_000_000).toFixed(1) + "M" + unit;
  if (Math.abs(v) >= 1_000) return (v / 1_000).toFixed(1) + "k" + unit;
  if (Math.abs(v) >= 100) return v.toFixed(0) + unit;
  if (Math.abs(v) >= 10) return v.toFixed(1) + unit;
  return v.toFixed(2) + unit;
}

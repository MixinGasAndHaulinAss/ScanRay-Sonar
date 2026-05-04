// common.tsx — shared building blocks for the seven Agents-overview
// sub-pages. Lifted to one place so DevicesAverages, NetworkLatency,
// etc. all use the same Card / KPITile / TopList / EmptyHint
// components and the views feel consistent.

import type React from "react";

export function Card({
  title,
  subtitle,
  children,
  className,
}: {
  title?: React.ReactNode;
  subtitle?: React.ReactNode;
  children?: React.ReactNode;
  className?: string;
}) {
  return (
    <div className={"rounded-xl border border-ink-800 bg-ink-900 p-4 " + (className ?? "")}>
      {title && (
        <div className="mb-2 flex items-baseline justify-between">
          <h4 className="text-xs font-semibold uppercase tracking-wide text-slate-400">
            {title}
          </h4>
          {subtitle && <span className="text-[10px] text-slate-500">{subtitle}</span>}
        </div>
      )}
      {children}
    </div>
  );
}

export function KPITile({
  label,
  value,
  tone = "neutral",
}: {
  label: string;
  value: number | string;
  tone?: "neutral" | "good" | "warn" | "bad";
}) {
  const toneCls =
    tone === "good"
      ? "text-emerald-300"
      : tone === "warn"
        ? "text-amber-300"
        : tone === "bad"
          ? "text-red-300"
          : "text-slate-100";
  return (
    <div className="rounded-lg border border-ink-800 bg-ink-950/40 p-3">
      <div className="text-[10px] uppercase tracking-wide text-slate-500">{label}</div>
      <div className={"mt-1 text-2xl font-semibold tabular-nums " + toneCls}>{value}</div>
    </div>
  );
}

export function TopList({
  rows,
  unit = "",
  emptyHint = "No data",
}: {
  rows: { id?: string; hostname?: string; isp?: string; model?: string; value?: number; count?: number; avgMs?: number; score?: number }[];
  unit?: string;
  emptyHint?: string;
}) {
  if (!rows || rows.length === 0) {
    return <div className="text-xs text-slate-500">{emptyHint}</div>;
  }
  return (
    <ul className="divide-y divide-ink-800/60 text-sm">
      {rows.map((r, i) => {
        const label = r.hostname ?? r.isp ?? r.model ?? r.id ?? "—";
        const value =
          r.value != null ? r.value : r.avgMs != null ? r.avgMs : r.score != null ? r.score : r.count;
        return (
          <li key={i} className="flex items-baseline justify-between py-1.5">
            <span className="truncate text-slate-200" title={label}>
              {label}
            </span>
            <span className="ml-3 shrink-0 tabular-nums text-slate-300">
              {value != null ? `${value}${unit}` : "—"}
            </span>
          </li>
        );
      })}
    </ul>
  );
}

export function EmptyHint({ children }: { children: React.ReactNode }) {
  return (
    <div className="rounded-md border border-dashed border-ink-700 bg-ink-950/40 p-3 text-xs text-slate-500">
      {children}
    </div>
  );
}

export function ErrorHint({ children }: { children: React.ReactNode }) {
  return (
    <div className="rounded-md border border-red-800/60 bg-red-950/40 p-3 text-xs text-red-200">
      {children}
    </div>
  );
}

// Tiny formatting helpers shared across the dashboard. Intentionally
// dependency-free — these are everywhere and we'd rather not couple
// "show me 12.4 GB" to dayjs/numeral/etc.

const KB = 1024;
const MB = KB * 1024;
const GB = MB * 1024;
const TB = GB * 1024;

export function formatBytes(n: number | null | undefined, digits = 1): string {
  if (n == null || isNaN(n)) return "—";
  if (n < KB) return `${n} B`;
  if (n < MB) return `${(n / KB).toFixed(digits)} KB`;
  if (n < GB) return `${(n / MB).toFixed(digits)} MB`;
  if (n < TB) return `${(n / GB).toFixed(digits)} GB`;
  return `${(n / TB).toFixed(digits)} TB`;
}

export function formatPct(n: number | null | undefined, digits = 1): string {
  if (n == null || isNaN(n)) return "—";
  return `${n.toFixed(digits)}%`;
}

// formatDuration turns a seconds count into "12d 4h", "3h 10m",
// "5m 20s", etc. — a single non-noisy summary suited to a stat card.
export function formatDuration(seconds: number | null | undefined): string {
  if (seconds == null || isNaN(seconds) || seconds < 0) return "—";
  const d = Math.floor(seconds / 86400);
  const h = Math.floor((seconds % 86400) / 3600);
  const m = Math.floor((seconds % 3600) / 60);
  const s = Math.floor(seconds % 60);
  if (d > 0) return `${d}d ${h}h`;
  if (h > 0) return `${h}h ${m}m`;
  if (m > 0) return `${m}m ${s}s`;
  return `${s}s`;
}

// formatRelative renders a timestamp as "37s ago", "4m ago", etc.
// Past-only: future timestamps fall back to a locale string.
export function formatRelative(iso: string | null | undefined): string {
  if (!iso) return "never";
  const t = new Date(iso).getTime();
  if (isNaN(t)) return "—";
  const diff = (Date.now() - t) / 1000;
  if (diff < 0) return new Date(iso).toLocaleString();
  if (diff < 60) return `${Math.floor(diff)}s ago`;
  if (diff < 3600) return `${Math.floor(diff / 60)}m ago`;
  if (diff < 86400) return `${Math.floor(diff / 3600)}h ago`;
  return `${Math.floor(diff / 86400)}d ago`;
}

// pctBarColor picks a Tailwind class for a horizontal usage bar.
// Green under 60%, amber under 85%, red beyond — same palette the
// stat cards use, so the eye picks up "something's wrong" without
// needing to read numbers.
export function pctBarColor(pct: number): string {
  if (pct >= 85) return "bg-red-500";
  if (pct >= 60) return "bg-amber-500";
  return "bg-emerald-500";
}

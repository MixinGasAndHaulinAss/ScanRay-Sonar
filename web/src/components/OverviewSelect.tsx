// OverviewSelect — the dropdown switcher at the top of the Agents
// page that picks one of seven views (matches the screenshots in the
// product spec). Selection is persisted in localStorage so an
// operator who lives in "Network - Latency" doesn't have to re-pick
// it every reload.
//
// We keep the option list in this file (instead of routing each view
// to its own URL) because:
//   * The seven views all read from the same /agents endpoints — the
//     dashboards are shaped, not data-shaped — so URL-per-view
//     would just churn the user's history list.
//   * Persisting selection in localStorage matches the existing
//     `sonar.agents.tagFilter` pattern used in pages/Agents.tsx, so
//     the page feels consistent.

export type OverviewView =
  | "overview"
  | "devices"
  | "devices-averages"
  | "devices-performance"
  | "network-latency"
  | "network-performance"
  | "applications-performance"
  | "user-experience"
  | "enrollment";

export const OVERVIEW_VIEWS: { id: OverviewView; label: string; group: string }[] = [
  { id: "overview",                 label: "Overview",                  group: "Summary" },
  { id: "devices",                  label: "Devices",                   group: "Devices" },
  { id: "devices-averages",         label: "Devices · Averages",        group: "Devices" },
  { id: "devices-performance",      label: "Devices · Performance",     group: "Devices" },
  { id: "network-latency",          label: "Network · Latency",         group: "Network" },
  { id: "network-performance",      label: "Network · Performance",     group: "Network" },
  { id: "applications-performance", label: "Applications · Performance", group: "Apps" },
  { id: "user-experience",          label: "User Experience",           group: "Apps" },
  { id: "enrollment",               label: "Enrollment tokens",         group: "Admin" },
];

export const OVERVIEW_KEY = "sonar.agents.overview";

interface Props {
  value: OverviewView;
  onChange: (next: OverviewView) => void;
  className?: string;
}

export default function OverviewSelect({ value, onChange, className }: Props) {
  // Group the options under <optgroup>s so the dropdown reads as
  // "Devices: ..." / "Network: ..." instead of one long flat list.
  const groups = Array.from(new Set(OVERVIEW_VIEWS.map((v) => v.group)));
  return (
    <label className={"flex items-center gap-2 " + (className ?? "")}>
      <span className="text-xs uppercase tracking-wide text-slate-500">View</span>
      <select
        value={value}
        onChange={(e) => onChange(e.target.value as OverviewView)}
        className="h-8 rounded-md border border-ink-700 bg-ink-950 px-2 text-xs text-slate-100 focus:border-sonar-500 focus:outline-none"
      >
        {groups.map((g) => (
          <optgroup key={g} label={g}>
            {OVERVIEW_VIEWS.filter((v) => v.group === g).map((v) => (
              <option key={v.id} value={v.id}>
                {v.label}
              </option>
            ))}
          </optgroup>
        ))}
      </select>
    </label>
  );
}

export function loadOverviewView(): OverviewView {
  try {
    const v = localStorage.getItem(OVERVIEW_KEY) as OverviewView | null;
    if (v && OVERVIEW_VIEWS.some((x) => x.id === v)) return v;
  } catch {
    /* localStorage may be disabled */
  }
  return "overview";
}

export function saveOverviewView(v: OverviewView) {
  try {
    localStorage.setItem(OVERVIEW_KEY, v);
  } catch {
    /* localStorage may be disabled */
  }
}

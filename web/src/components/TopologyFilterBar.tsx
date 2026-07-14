// TopologyFilterBar — shared phones + tag controls for global Topology
// and the site network map. AND/OR mode matters for Meraki role tags
// (firewall vs switch) where AND would empty the graph.

import TagFilter from "./TagFilter";

const ROLE_CHIP_ORDER = ["firewall", "switch", "wap", "meraki", "sensor", "camera"];

export type TagMatchMode = "and" | "or";

interface Props {
  availableTags: string[];
  selectedTags: string[];
  onTagsChange: (next: string[]) => void;
  matchMode: TagMatchMode;
  onMatchModeChange: (mode: TagMatchMode) => void;
  includePhones: boolean;
  onIncludePhonesChange: (v: boolean) => void;
  onRefresh?: () => void;
  refreshing?: boolean;
}

export default function TopologyFilterBar({
  availableTags,
  selectedTags,
  onTagsChange,
  matchMode,
  onMatchModeChange,
  includePhones,
  onIncludePhonesChange,
  onRefresh,
  refreshing,
}: Props) {
  const roleChips = ROLE_CHIP_ORDER.filter((t) => availableTags.includes(t));

  const toggleTag = (t: string) => {
    onTagsChange(
      selectedTags.includes(t)
        ? selectedTags.filter((x) => x !== t)
        : [...selectedTags, t],
    );
  };

  return (
    <div className="flex flex-col items-stretch gap-2 sm:items-end">
      {roleChips.length > 0 && (
        <div className="flex flex-wrap items-center justify-end gap-1.5">
          <span className="text-[10px] uppercase tracking-wide text-slate-500">Roles</span>
          {roleChips.map((t) => {
            const on = selectedTags.includes(t);
            return (
              <button
                key={t}
                type="button"
                onClick={() => toggleTag(t)}
                className={
                  "rounded-full border px-2.5 py-0.5 text-[11px] transition " +
                  (on
                    ? "border-sonar-500 bg-sonar-700/40 text-sonar-100"
                    : "border-ink-700 bg-ink-900 text-slate-400 hover:border-ink-600 hover:text-slate-200")
                }
              >
                {t}
              </button>
            );
          })}
        </div>
      )}
      <div className="flex flex-wrap items-center justify-end gap-2">
        <label
          className="inline-flex cursor-pointer select-none items-center gap-2 rounded-full border border-ink-700 bg-ink-900 px-3 py-1.5 text-xs text-slate-300 hover:border-ink-600"
          title="By default IP phones are hidden so the inter-switch backbone stays readable."
        >
          <input
            type="checkbox"
            className="accent-sonar-500"
            checked={includePhones}
            onChange={(e) => onIncludePhonesChange(e.target.checked)}
          />
          Show IP phones
        </label>
        <div
          className="inline-flex overflow-hidden rounded-full border border-ink-700 text-[11px]"
          title="AND requires every selected tag; OR keeps nodes with any selected tag"
        >
          <button
            type="button"
            onClick={() => onMatchModeChange("and")}
            className={
              "px-2.5 py-1.5 " +
              (matchMode === "and"
                ? "bg-ink-700 text-slate-100"
                : "bg-ink-900 text-slate-400 hover:bg-ink-800")
            }
          >
            AND
          </button>
          <button
            type="button"
            onClick={() => onMatchModeChange("or")}
            className={
              "px-2.5 py-1.5 " +
              (matchMode === "or"
                ? "bg-ink-700 text-slate-100"
                : "bg-ink-900 text-slate-400 hover:bg-ink-800")
            }
          >
            OR
          </button>
        </div>
        <TagFilter
          availableTags={availableTags}
          selected={selectedTags}
          onChange={onTagsChange}
          mode={matchMode}
        />
        {selectedTags.length > 0 && (
          <button
            type="button"
            onClick={() => onTagsChange([])}
            className="rounded-full border border-ink-700 bg-ink-900 px-3 py-1.5 text-xs text-slate-400 hover:border-ink-600 hover:text-slate-200"
          >
            Clear ({selectedTags.length})
          </button>
        )}
        {onRefresh && (
          <button
            type="button"
            onClick={onRefresh}
            disabled={refreshing}
            className="rounded-full border border-ink-700 bg-ink-900 px-3 py-1.5 text-xs text-slate-200 hover:border-ink-600 hover:bg-ink-800 disabled:opacity-50"
          >
            {refreshing ? "Refreshing…" : "Refresh"}
          </button>
        )}
      </div>
    </div>
  );
}

/** Keep appliances matching tags; pull connected foreign/cloud nodes via edges. */
export function filterTopologyByTags<
  T extends {
    nodes: { id: string; kind: string; tags?: string[] }[];
    edges: { from: string; to: string }[];
  },
>(data: T, tagFilter: string[], mode: TagMatchMode): T {
  if (tagFilter.length === 0) return data;
  const keepIds = new Set<string>();
  for (const n of data.nodes) {
    if (n.kind !== "appliance") continue;
    const tags = new Set(n.tags ?? []);
    const match =
      mode === "or"
        ? tagFilter.some((t) => tags.has(t))
        : tagFilter.every((t) => tags.has(t));
    if (match) keepIds.add(n.id);
  }
  // Expand once so Internet / VPN peers attached to kept appliances remain.
  for (const e of data.edges) {
    if (keepIds.has(e.from) || keepIds.has(e.to)) {
      keepIds.add(e.from);
      keepIds.add(e.to);
    }
  }
  return {
    ...data,
    nodes: data.nodes.filter((n) => keepIds.has(n.id)),
    edges: data.edges.filter((e) => keepIds.has(e.from) && keepIds.has(e.to)),
  };
}

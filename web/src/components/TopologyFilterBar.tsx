// TopologyFilterBar — shared tag + link controls for global Topology
// and the site network map. AND/OR mode matters for Meraki role tags
// (firewall vs switch) where AND would empty the graph.
//
// IP phones arrive from the API tagged "phone". They are hidden unless
// the phone role chip is selected (same click on/off pattern as switch).

import TagFilter from "./TagFilter";

const ROLE_CHIP_ORDER = [
  "firewall",
  "switch",
  "wap",
  "meraki",
  "sensor",
  "camera",
  "phone",
];

export type TagMatchMode = "and" | "or";

export interface LinkVisibility {
  wan: boolean;
  autoVpn: boolean;
  thirdPartyVpn: boolean;
}

interface Props {
  availableTags: string[];
  selectedTags: string[];
  onTagsChange: (next: string[]) => void;
  matchMode: TagMatchMode;
  onMatchModeChange: (mode: TagMatchMode) => void;
  links: LinkVisibility;
  onLinksChange: (next: LinkVisibility) => void;
  onRefresh?: () => void;
  refreshing?: boolean;
}

export default function TopologyFilterBar({
  availableTags,
  selectedTags,
  onTagsChange,
  matchMode,
  onMatchModeChange,
  links,
  onLinksChange,
  onRefresh,
  refreshing,
}: Props) {
  // Always offer the phone chip when phones exist in the payload.
  const roleChips = ROLE_CHIP_ORDER.filter((t) => availableTags.includes(t));

  const toggleTag = (t: string) => {
    onTagsChange(
      selectedTags.includes(t)
        ? selectedTags.filter((x) => x !== t)
        : [...selectedTags, t],
    );
  };

  const linkToggle = (
    key: keyof LinkVisibility,
    label: string,
    title: string,
  ) => (
    <label
      className="inline-flex cursor-pointer select-none items-center gap-1.5 rounded-full border border-ink-700 bg-ink-900 px-2.5 py-1 text-[11px] text-slate-300 hover:border-ink-600"
      title={title}
    >
      <input
        type="checkbox"
        className="accent-sonar-500"
        checked={links[key]}
        onChange={(e) => onLinksChange({ ...links, [key]: e.target.checked })}
      />
      {label}
    </label>
  );

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
                title={
                  t === "phone"
                    ? "Show IP phones (hidden by default)"
                    : undefined
                }
              >
                {t}
              </button>
            );
          })}
        </div>
      )}
      <div className="flex flex-wrap items-center justify-end gap-1.5">
        <span className="text-[10px] uppercase tracking-wide text-slate-500">Links</span>
        {linkToggle("wan", "WAN", "Meraki WAN uplinks and SNMP tunnel stubs to Internet")}
        {linkToggle("autoVpn", "Auto VPN", "Meraki site-to-site Auto VPN between networks")}
        {linkToggle(
          "thirdPartyVpn",
          "3rd-party VPN",
          "Third-party IPsec peers — off by default (very noisy)",
        )}
      </div>
      <div className="flex flex-wrap items-center justify-end gap-2">
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

function hasTag(tags: string[] | undefined, tag: string): boolean {
  return (tags ?? []).includes(tag);
}

/**
 * Filter topology by role/tags.
 * - "phone" is opt-in: phone-tagged nodes stay hidden unless selected.
 * - Other tags filter appliances (AND/OR); connected foreign/cloud nodes
 *   are pulled in via edges.
 */
export function filterTopologyByTags<
  T extends {
    nodes: { id: string; kind: string; tags?: string[] }[];
    edges: { from: string; to: string }[];
  },
>(data: T, tagFilter: string[], mode: TagMatchMode): T {
  const wantPhones = tagFilter.includes("phone");
  const roleTags = tagFilter.filter((t) => t !== "phone");

  let nodes = data.nodes;
  let edges = data.edges;

  if (roleTags.length > 0) {
    const keepIds = new Set<string>();
    for (const n of nodes) {
      if (n.kind !== "appliance") continue;
      const tags = new Set(n.tags ?? []);
      const match =
        mode === "or"
          ? roleTags.some((t) => tags.has(t))
          : roleTags.every((t) => tags.has(t));
      if (match) keepIds.add(n.id);
    }
    for (const e of edges) {
      if (keepIds.has(e.from) || keepIds.has(e.to)) {
        keepIds.add(e.from);
        keepIds.add(e.to);
      }
    }
    nodes = nodes.filter((n) => keepIds.has(n.id));
    edges = edges.filter((e) => keepIds.has(e.from) && keepIds.has(e.to));
  }

  if (!wantPhones) {
    const phoneIds = new Set(
      nodes.filter((n) => hasTag(n.tags, "phone")).map((n) => n.id),
    );
    if (phoneIds.size > 0) {
      nodes = nodes.filter((n) => !phoneIds.has(n.id));
      edges = edges.filter((e) => !phoneIds.has(e.from) && !phoneIds.has(e.to));
    }
  }

  return { ...data, nodes, edges };
}

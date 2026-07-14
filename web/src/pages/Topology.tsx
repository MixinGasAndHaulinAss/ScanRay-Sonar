// Topology — switch fabric + Meraki WAN/VPN view.
// Rendering: React Flow (@xyflow) + ELK layered layout (see TopologyFlow).

import { useEffect, useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api } from "../api/client";
import type { Topology, TopologyEdge } from "../api/types";
import TopologyFlow, { isVPNEdge, isWANEdge } from "../components/TopologyFlow";
import TopologyFilterBar, {
  filterTopologyByTags,
  type LinkVisibility,
  type TagMatchMode,
} from "../components/TopologyFilterBar";

const DEFAULT_LINKS: LinkVisibility = {
  wan: true,
  autoVpn: true,
  thirdPartyVpn: false,
};

const TAG_FILTER_KEY = "sonar.topology.tags";
const TAG_MODE_KEY = "sonar.topology.tagMode";
const LINKS_KEY = "sonar.topology.links";

function linkMedium(e: TopologyEdge): string {
  const m = (e.linkKind as { medium?: unknown } | undefined)?.medium;
  return typeof m === "string" ? m.toLowerCase() : "";
}

function isAutoVPN(e: TopologyEdge): boolean {
  return e.protocol === "meraki-autovpn";
}

function isThirdPartyVPN(e: TopologyEdge): boolean {
  return e.protocol === "third-party-vpn" || e.protocol === "ipsec";
}

function edgeAllowed(e: TopologyEdge, links: LinkVisibility): boolean {
  if (isWANEdge(e)) return links.wan;
  if (isAutoVPN(e)) return links.autoVpn;
  if (isThirdPartyVPN(e)) return links.thirdPartyVpn;
  if (isVPNEdge(e) || linkMedium(e) === "vpn") return links.autoVpn;
  return true;
}

function loadTags(): string[] {
  try {
    const raw = localStorage.getItem(TAG_FILTER_KEY);
    if (!raw) return [];
    const parsed = JSON.parse(raw);
    return Array.isArray(parsed) ? parsed.filter((t) => typeof t === "string") : [];
  } catch {
    return [];
  }
}

function loadTagMode(): TagMatchMode {
  return localStorage.getItem(TAG_MODE_KEY) === "or" ? "or" : "and";
}

function loadLinks(): LinkVisibility {
  try {
    const raw = localStorage.getItem(LINKS_KEY);
    if (!raw) return DEFAULT_LINKS;
    const parsed = JSON.parse(raw) as Partial<LinkVisibility>;
    return {
      wan: parsed.wan ?? true,
      autoVpn: parsed.autoVpn ?? true,
      thirdPartyVpn: parsed.thirdPartyVpn ?? false,
    };
  } catch {
    return DEFAULT_LINKS;
  }
}

function applyLinkFilter(data: Topology, links: LinkVisibility): Topology {
  const edges = data.edges.filter((e) => edgeAllowed(e, links));
  const used = new Set<string>();
  for (const e of edges) {
    used.add(e.from);
    used.add(e.to);
  }
  const nodes = data.nodes.filter((n) => {
    if (n.kind === "appliance" || n.kind === "cloud") return true;
    return used.has(n.id);
  });
  const cloudId = nodes.find((n) => n.kind === "cloud")?.id;
  const withoutCloud = links.wan
    ? nodes
    : nodes.filter((n) => n.kind !== "cloud");
  const withoutCloudEdges = links.wan
    ? edges
    : edges.filter((e) => e.from !== cloudId && e.to !== cloudId);
  return { ...data, nodes: withoutCloud, edges: withoutCloudEdges };
}

export default function Topology() {
  const [tagFilter, setTagFilter] = useState<string[]>(loadTags);
  useEffect(() => {
    localStorage.setItem(TAG_FILTER_KEY, JSON.stringify(tagFilter));
  }, [tagFilter]);

  const [tagMode, setTagMode] = useState<TagMatchMode>(loadTagMode);
  useEffect(() => {
    localStorage.setItem(TAG_MODE_KEY, tagMode);
  }, [tagMode]);

  const [links, setLinks] = useState<LinkVisibility>(loadLinks);
  useEffect(() => {
    localStorage.setItem(LINKS_KEY, JSON.stringify(links));
  }, [links]);

  const { data, isLoading, error, refetch, isFetching } = useQuery({
    queryKey: ["topology"],
    queryFn: () => api.get<Topology>("/topology"),
    refetchInterval: 30_000,
  });

  const allTags = useMemo(() => {
    const set = new Set<string>();
    for (const n of data?.nodes ?? []) {
      for (const t of n.tags ?? []) set.add(t);
    }
    return Array.from(set).sort();
  }, [data]);

  const filtered = useMemo<Topology | undefined>(() => {
    if (!data) return data;
    const tagged = filterTopologyByTags(data, tagFilter, tagMode);
    return applyLinkFilter(tagged, links);
  }, [data, tagFilter, tagMode, links]);

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h2 className="text-2xl font-semibold tracking-tight">Topology</h2>
          <p className="mt-0.5 text-xs text-slate-500">
            React Flow + ELK layered layout. Toggle roles (including phone) and link
            layers. Third-party VPN is off by default.
          </p>
        </div>
        <TopologyFilterBar
          availableTags={allTags}
          selectedTags={tagFilter}
          onTagsChange={setTagFilter}
          matchMode={tagMode}
          onMatchModeChange={setTagMode}
          links={links}
          onLinksChange={setLinks}
          onRefresh={() => refetch()}
          refreshing={isFetching}
        />
      </div>

      {isLoading && <div className="text-sm text-slate-500">Loading topology…</div>}
      {error && (
        <div className="rounded-md border border-red-900/60 bg-red-950/30 px-3 py-2 text-sm text-red-300">
          Failed to load topology.
        </div>
      )}

      {filtered && <TopologyGraph data={filtered} />}
      {filtered && <TopologyLinkLegend />}
    </div>
  );
}

/** Shared by Topology page and SiteNetworkMap. */
export function TopologyGraph({ data }: { data: Topology }) {
  return <TopologyFlow data={data} />;
}

export function TopologyLinkLegend() {
  return (
    <div className="flex flex-wrap gap-3 rounded-md border border-ink-800 bg-ink-900/60 p-3 text-xs text-slate-300">
      <span className="font-semibold text-slate-200">Link kinds:</span>
      <Swatch color="bg-slate-400" label="L2 LLDP/CDP (layout)" />
      <Swatch color="bg-amber-400" label="WAN → Internet" />
      <Swatch color="bg-purple-400" label="Auto VPN" />
      <Swatch color="bg-fuchsia-500" label="3rd-party VPN (opt-in)" />
    </div>
  );
}

function Swatch({ color, label }: { color: string; label: string }) {
  return (
    <span className="inline-flex items-center gap-1.5">
      <span className={`inline-block h-2.5 w-5 rounded ${color}`} />
      <span>{label}</span>
    </span>
  );
}

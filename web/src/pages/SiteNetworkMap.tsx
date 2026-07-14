import { useEffect, useMemo, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api } from "../api/client";
import type { Topology, TopologyEdge, TopologyNode } from "../api/types";
import TopologyFilterBar, {
  filterTopologyByTags,
  type LinkVisibility,
  type TagMatchMode,
} from "../components/TopologyFilterBar";
import { TopologyGraph, TopologyLinkLegend } from "./Topology";

const TAG_FILTER_KEY = "sonar.topology.site.tags";
const TAG_MODE_KEY = "sonar.topology.site.tagMode";
const PHONES_KEY = "sonar.topology.site.includePhones";
const LINKS_KEY = "sonar.topology.site.links";

const DEFAULT_LINKS: LinkVisibility = {
  wan: true,
  autoVpn: true,
  thirdPartyVpn: false,
};

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

function linkMedium(e: TopologyEdge): string {
  const m = (e.linkKind as { medium?: unknown } | undefined)?.medium;
  return typeof m === "string" ? m.toLowerCase() : "";
}

function edgeAllowed(e: TopologyEdge, links: LinkVisibility): boolean {
  const medium = linkMedium(e);
  if (medium === "wan" || e.protocol === "uplink") return links.wan;
  if (e.protocol === "meraki-autovpn") return links.autoVpn;
  if (e.protocol === "third-party-vpn" || e.protocol === "ipsec") return links.thirdPartyVpn;
  if (medium === "vpn") return links.autoVpn;
  return true;
}

function applyLinkFilter(data: Topology, links: LinkVisibility): Topology {
  const edges = data.edges.filter((e) => edgeAllowed(e, links));
  const used = new Set<string>();
  for (const e of edges) {
    used.add(e.from);
    used.add(e.to);
  }
  const cloudId = data.nodes.find((n) => n.kind === "cloud")?.id;
  let nodes = data.nodes.filter((n: TopologyNode) => {
    if (n.kind === "appliance") return true;
    if (n.kind === "cloud") return links.wan;
    return used.has(n.id);
  });
  if (!links.wan && cloudId) {
    nodes = nodes.filter((n) => n.id !== cloudId);
  }
  return { ...data, nodes, edges };
}

export default function SiteNetworkMap() {
  const { siteId } = useParams<{ siteId: string }>();

  const [includePhones, setIncludePhones] = useState(
    () => localStorage.getItem(PHONES_KEY) === "1",
  );
  useEffect(() => {
    localStorage.setItem(PHONES_KEY, includePhones ? "1" : "0");
  }, [includePhones]);

  const [tagFilter, setTagFilter] = useState<string[]>(loadTags);
  useEffect(() => {
    localStorage.setItem(TAG_FILTER_KEY, JSON.stringify(tagFilter));
  }, [tagFilter]);

  const [tagMode, setTagMode] = useState<TagMatchMode>(() =>
    localStorage.getItem(TAG_MODE_KEY) === "or" ? "or" : "and",
  );
  useEffect(() => {
    localStorage.setItem(TAG_MODE_KEY, tagMode);
  }, [tagMode]);

  const [links, setLinks] = useState<LinkVisibility>(loadLinks);
  useEffect(() => {
    localStorage.setItem(LINKS_KEY, JSON.stringify(links));
  }, [links]);

  const { data, isLoading, error, refetch, isFetching } = useQuery({
    queryKey: ["site-network-map", siteId, includePhones],
    queryFn: () =>
      api.get<Topology>(
        `/sites/${siteId}/network-map${includePhones ? "?includePhones=1" : ""}`,
      ),
    enabled: !!siteId,
    refetchInterval: 30_000,
  });

  const allTags = useMemo(() => {
    const set = new Set<string>();
    for (const n of data?.nodes ?? []) {
      for (const t of n.tags ?? []) set.add(t);
    }
    return Array.from(set).sort();
  }, [data]);

  const filtered = useMemo(() => {
    if (!data) return data;
    return applyLinkFilter(filterTopologyByTags(data, tagFilter, tagMode), links);
  }, [data, tagFilter, tagMode, links]);

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h2 className="text-2xl font-semibold tracking-tight">Site network map</h2>
          <p className="mt-0.5 text-xs text-slate-500">
            Topology limited to this site. Third-party VPN is off by default — turn it on
            under Links if you need those peers.
          </p>
        </div>
        <div className="flex flex-wrap items-end gap-2">
          <TopologyFilterBar
            availableTags={allTags}
            selectedTags={tagFilter}
            onTagsChange={setTagFilter}
            matchMode={tagMode}
            onMatchModeChange={setTagMode}
            includePhones={includePhones}
            onIncludePhonesChange={setIncludePhones}
            links={links}
            onLinksChange={setLinks}
            onRefresh={() => refetch()}
            refreshing={isFetching}
          />
          <Link
            to="/sites"
            className="rounded-full border border-ink-700 bg-ink-900 px-3 py-1.5 text-xs text-slate-300 hover:bg-ink-800"
          >
            ← Sites
          </Link>
        </div>
      </div>

      {isLoading && <p className="text-sm text-slate-500">Loading map…</p>}
      {error && (
        <div className="rounded-md border border-red-900/60 bg-red-950/30 px-3 py-2 text-sm text-red-300">
          Could not load network map for this site.
        </div>
      )}
      {filtered && <TopologyGraph data={filtered} />}
      {filtered && <TopologyLinkLegend />}
    </div>
  );
}

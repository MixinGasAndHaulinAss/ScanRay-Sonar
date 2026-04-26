// Topology — switch fabric view. Two tabs:
//
//   * Graph — managed appliances + foreign LLDP/CDP neighbors as a
//     draggable force-directed graph (history-shared with the
//     per-host network graph on the agent detail page; see
//     ../components/ForceGraph.tsx).
//
//   * Map — the same nodes plotted on a world map by their MgmtIP's
//     MaxMind-derived (lat, lon). Private / non-routable IPs don't
//     get a fix and are stacked into a "private network" widget so
//     the operator still has a path to drill into them.
//
// A shared tag filter (TagFilter dropdown) narrows visible nodes on
// both tabs at once. Edges are pruned to those whose endpoints are
// both visible — otherwise an arrow would dangle into empty space.

import { useEffect, useMemo, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import {
  ComposableMap,
  Geographies,
  Geography,
  Marker,
  ZoomableGroup,
} from "react-simple-maps";
import { feature } from "topojson-client";
import type {
  Topology as TopoJsonTopology,
  GeometryCollection,
} from "topojson-specification";
import worldTopo from "../assets/world-110m.json";
import { api } from "../api/client";
import type { Topology, TopologyEdge, TopologyNode } from "../api/types";
import ForceGraph, {
  type ForceEdgeInput,
  type ForceNodeInput,
  type SimNode,
} from "../components/ForceGraph";
import TagFilter from "../components/TagFilter";

const STATUS_FILL: Record<TopologyNode["status"], string> = {
  up: "#0ea5e9",
  degraded: "#f59e0b",
  down: "#ef4444",
  unknown: "#475569",
};
const STATUS_RING: Record<TopologyNode["status"], string> = {
  up: "#38bdf8",
  degraded: "#fbbf24",
  down: "#f87171",
  unknown: "#64748b",
};

function nodeRadius(n: TopologyNode): number {
  if (n.kind === "foreign") return 16;
  return 22 + Math.min((n.uplinkCount ?? 0) * 1.5, 8);
}

interface TopoNode extends ForceNodeInput {
  ref: TopologyNode;
}

interface TopoEdge extends ForceEdgeInput {
  ref: TopologyEdge;
}

const TAB_KEY = "sonar.topology.tab";
const TAG_FILTER_KEY = "sonar.topology.tags";

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

export default function TopologyPage() {
  const [includePhones, setIncludePhones] = useState(() => {
    return localStorage.getItem("sonar.topology.includePhones") === "1";
  });
  useEffect(() => {
    localStorage.setItem("sonar.topology.includePhones", includePhones ? "1" : "0");
  }, [includePhones]);

  const [tab, setTab] = useState<"graph" | "map">(() => {
    const v = localStorage.getItem(TAB_KEY);
    return v === "map" ? "map" : "graph";
  });
  useEffect(() => {
    localStorage.setItem(TAB_KEY, tab);
  }, [tab]);

  const [tagFilter, setTagFilter] = useState<string[]>(loadTags);
  useEffect(() => {
    localStorage.setItem(TAG_FILTER_KEY, JSON.stringify(tagFilter));
  }, [tagFilter]);

  const { data, isLoading, error, refetch, isFetching } = useQuery({
    queryKey: ["topology", includePhones],
    queryFn: () =>
      api.get<Topology>(
        includePhones ? "/topology?includePhones=1" : "/topology",
      ),
    refetchInterval: 30_000,
  });

  // All tags across managed appliance nodes — fed to the TagFilter.
  const allTags = useMemo(() => {
    const set = new Set<string>();
    for (const n of data?.nodes ?? []) {
      for (const t of n.tags ?? []) set.add(t);
    }
    return Array.from(set).sort();
  }, [data]);

  // Apply tag filter: managed nodes must have every selected tag
  // (AND match). Foreign nodes pass through when the filter is empty
  // — when a filter is active they only stay if they connect to a
  // surviving managed node, so the operator never sees orphan
  // neighbors of a host they've filtered out.
  const filtered = useMemo<Topology | undefined>(() => {
    if (!data) return data;
    if (tagFilter.length === 0) return data;
    const keepIds = new Set<string>();
    for (const n of data.nodes) {
      if (n.kind !== "appliance") continue;
      const tags = new Set(n.tags ?? []);
      if (tagFilter.every((t) => tags.has(t))) keepIds.add(n.id);
    }
    // Bring foreign nodes in if any of their edges connects to a kept
    // managed node.
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
  }, [data, tagFilter]);

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h2 className="text-2xl font-semibold tracking-tight">Topology</h2>
          <p className="mt-0.5 text-xs text-slate-500">
            {tab === "graph"
              ? "Auto-discovered from LLDP and Cisco CDP on each appliance's last poll. Drag any node to rearrange."
              : "Each appliance plotted at its management-IP geolocation. Private addresses fall into the side panel — MaxMind has no data for them."}{" "}
            Refreshes every 30 seconds.
          </p>
        </div>
        <div className="flex items-center gap-2">
          {tab === "graph" && (
            <label
              className="inline-flex cursor-pointer select-none items-center gap-2 rounded-full border border-ink-700 bg-ink-900 px-3 py-1.5 text-xs text-slate-300 hover:border-ink-600"
              title="By default IP phones are hidden so the inter-switch backbone stays readable."
            >
              <input
                type="checkbox"
                className="accent-sonar-500"
                checked={includePhones}
                onChange={(e) => setIncludePhones(e.target.checked)}
              />
              Show IP phones
            </label>
          )}
          <TagFilter
            availableTags={allTags}
            selected={tagFilter}
            onChange={setTagFilter}
            mode="and"
          />
          <button
            onClick={() => refetch()}
            disabled={isFetching}
            className="rounded-full border border-ink-700 bg-ink-900 px-3 py-1.5 text-xs text-slate-200 hover:border-ink-600 hover:bg-ink-800 disabled:opacity-50"
          >
            {isFetching ? "Refreshing…" : "Refresh"}
          </button>
        </div>
      </div>

      <Tabs tab={tab} onChange={setTab} graphCount={filtered?.nodes.length ?? 0} />

      {isLoading && <div className="text-sm text-slate-500">Loading topology…</div>}
      {error && (
        <div className="rounded-md border border-red-900/60 bg-red-950/30 px-3 py-2 text-sm text-red-300">
          Failed to load topology.
        </div>
      )}

      {filtered && tab === "graph" && <TopologyGraph data={filtered} />}
      {filtered && tab === "map" && <TopologyMap data={filtered} />}
    </div>
  );
}

function Tabs({
  tab,
  onChange,
  graphCount,
}: {
  tab: "graph" | "map";
  onChange: (t: "graph" | "map") => void;
  graphCount: number;
}) {
  const cls = (active: boolean) =>
    "inline-flex items-center gap-1.5 rounded-t-md border-b-2 px-3 py-1.5 text-xs font-medium transition " +
    (active
      ? "border-sonar-500 text-sonar-200"
      : "border-transparent text-slate-400 hover:text-slate-200");
  return (
    <div className="flex items-center gap-2 border-b border-ink-800">
      <button onClick={() => onChange("graph")} className={cls(tab === "graph")}>
        <span>Graph</span>
        <span className="rounded-full bg-ink-800 px-1.5 py-0.5 text-[10px] tabular-nums text-slate-400">
          {graphCount}
        </span>
      </button>
      <button onClick={() => onChange("map")} className={cls(tab === "map")}>
        <svg
          width="12"
          height="12"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
        >
          <circle cx="12" cy="12" r="10" />
          <path d="M2 12h20M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z" />
        </svg>
        <span>Map</span>
      </button>
    </div>
  );
}

// =====================================================================
// Graph view (existing force-directed implementation, lifted out)
// =====================================================================

function TopologyGraph({ data }: { data: Topology }) {
  const navigate = useNavigate();
  const [size, setSize] = useState({ w: 1200, h: 720 });
  const wrapRef = (el: HTMLDivElement | null) => {
    if (!el) return;
    const ro = new ResizeObserver((entries) => {
      for (const e of entries) {
        const cr = e.contentRect;
        setSize({ w: Math.max(800, cr.width), h: Math.max(600, cr.height) });
      }
    });
    ro.observe(el);
  };

  if (data.nodes.length === 0) {
    return (
      <div className="rounded-xl border border-ink-800 bg-ink-900 p-10 text-center text-sm text-slate-400">
        No appliances match the current filter. Add one under{" "}
        <Link to="/appliances" className="text-sonar-400 hover:underline">
          Appliances
        </Link>{" "}
        or clear the tag filter above.
      </div>
    );
  }

  const onlyManaged = data.nodes.every((n) => n.kind === "appliance");
  const noEdges = data.edges.length === 0;

  const nodes: TopoNode[] = data.nodes.map((n) => ({ id: n.id, ref: n }));
  const edges: TopoEdge[] = data.edges.map((e) => ({
    from: e.from,
    to: e.to,
    ref: e,
  }));

  return (
    <div
      ref={wrapRef}
      className="relative h-[72vh] overflow-hidden rounded-xl border border-ink-800 bg-ink-900"
    >
      {noEdges && (
        <div className="absolute left-3 top-3 z-10 max-w-md rounded-md border border-amber-900/60 bg-amber-950/40 px-3 py-2 text-xs text-amber-200">
          {onlyManaged
            ? "Showing managed appliances only — no LLDP or CDP neighbors discovered yet."
            : "No discovery links yet."}{" "}
          Make sure LLDP/CDP is enabled on your switches; on Cisco IOS it's{" "}
          <code className="font-mono">cdp run</code> +{" "}
          <code className="font-mono">lldp run</code> globally.
        </div>
      )}

      <ForceGraph<TopoNode, TopoEdge>
        nodes={nodes}
        edges={edges}
        width={size.w}
        height={size.h}
        renderEdge={(e, a, b) => (
          <line
            key={`${e.from}->${e.to}`}
            x1={a.x}
            y1={a.y}
            x2={b.x}
            y2={b.y}
            stroke={e.ref.operUp ? "#475569" : "#1e293b"}
            strokeWidth={1.4}
            strokeDasharray={e.ref.operUp ? undefined : "4 4"}
            opacity={0.7}
          />
        )}
        renderNode={(s) => <NodeBubble sim={s} />}
        onNodeClick={(n) => {
          if (n.ref.kind === "appliance") {
            navigate(`/appliances/${n.ref.id}`);
          }
        }}
      />

      <Legend />
    </div>
  );
}

function NodeBubble({ sim }: { sim: SimNode<TopoNode> }) {
  const n = sim.data.ref;
  const r = nodeRadius(n);
  const isAppliance = n.kind === "appliance";
  const fill = isAppliance ? STATUS_FILL[n.status] : "#1e293b";
  const ring = isAppliance ? STATUS_RING[n.status] : "#475569";

  return (
    <g>
      <circle
        cx={sim.x}
        cy={sim.y}
        r={r}
        fill={fill}
        stroke={ring}
        strokeWidth={1.5}
        opacity={isAppliance ? 0.95 : 0.7}
      />
      {isAppliance && (
        <text
          x={sim.x}
          y={sim.y + 4}
          textAnchor="middle"
          className="pointer-events-none select-none fill-white text-[10px] font-semibold"
        >
          {n.portsUp ?? "?"}/{n.portsTotal ?? "?"}
        </text>
      )}
      <text
        x={sim.x}
        y={sim.y + r + 14}
        textAnchor="middle"
        className="pointer-events-none select-none fill-slate-200 text-[11px]"
      >
        {n.label}
      </text>
      {n.mgmtIp && (
        <text
          x={sim.x}
          y={sim.y + r + 26}
          textAnchor="middle"
          className="pointer-events-none select-none fill-slate-500 font-mono text-[9px]"
        >
          {n.mgmtIp}
        </text>
      )}
    </g>
  );
}

function Legend() {
  return (
    <div className="absolute bottom-3 right-3 flex items-center gap-3 rounded-md border border-ink-800 bg-ink-950/80 px-3 py-1.5 text-[10px] uppercase tracking-wider text-slate-400 backdrop-blur">
      <Dot color={STATUS_FILL.up} /> up
      <Dot color={STATUS_FILL.degraded} /> degraded
      <Dot color={STATUS_FILL.down} /> down
      <Dot color="#1e293b" ring="#475569" /> foreign
    </div>
  );
}

function Dot({ color, ring }: { color: string; ring?: string }) {
  return (
    <span className="inline-flex items-center gap-1">
      <span
        className="inline-block h-2.5 w-2.5 rounded-full"
        style={{
          background: color,
          border: ring ? `1px solid ${ring}` : undefined,
        }}
      />
    </span>
  );
}

// =====================================================================
// Map view
// =====================================================================

const worldFeatures = (() => {
  const topo = worldTopo as unknown as TopoJsonTopology;
  const obj = topo.objects.countries as GeometryCollection;
  const fc = feature(topo, obj) as unknown as GeoJSON.FeatureCollection;
  return fc;
})();

interface Cluster {
  lat: number;
  lon: number;
  members: TopologyNode[];
}

function clusterNodes(
  nodes: TopologyNode[],
  precision: number,
): Cluster[] {
  const map = new Map<string, Cluster>();
  for (const n of nodes) {
    if (n.lat == null || n.lon == null) continue;
    if (n.lat === 0 && n.lon === 0) continue;
    const key = `${n.lat.toFixed(precision)}:${n.lon.toFixed(precision)}`;
    let c = map.get(key);
    if (!c) {
      c = { lat: 0, lon: 0, members: [] };
      map.set(key, c);
    }
    c.members.push(n);
  }
  return Array.from(map.values()).map((c) => ({
    members: c.members,
    lat: c.members.reduce((s, m) => s + (m.lat as number), 0) / c.members.length,
    lon: c.members.reduce((s, m) => s + (m.lon as number), 0) / c.members.length,
  }));
}

function clusterRadius(n: number): number {
  if (n === 1) return 5;
  if (n < 5) return 7.5;
  if (n < 20) return 10;
  return 13;
}

function pickDominantStatus(members: TopologyNode[]): TopologyNode["status"] {
  const order: TopologyNode["status"][] = ["down", "degraded", "up", "unknown"];
  for (const s of order) {
    if (members.some((m) => m.status === s)) return s;
  }
  return "unknown";
}

function TopologyMap({ data }: { data: Topology }) {
  const navigate = useNavigate();
  const [zoom, setZoom] = useState(1);
  const [center, setCenter] = useState<[number, number]>([0, 20]);
  const [hovered, setHovered] = useState<Cluster | null>(null);
  const [pinned, setPinned] = useState<Cluster | null>(null);

  // Split into mappable vs non-mappable. "Mappable" means we have a
  // non-zero lat/lon — covers nodes whose mgmtIp landed in MaxMind.
  // Everything else (private + foreign-without-IP + IPs MaxMind didn't
  // recognise) gets stacked into the side panel so it's still
  // operator-reachable.
  const { mappable, unmapped } = useMemo(() => {
    const mappable: TopologyNode[] = [];
    const unmapped: TopologyNode[] = [];
    for (const n of data.nodes) {
      if (n.lat != null && n.lon != null && (n.lat !== 0 || n.lon !== 0)) {
        mappable.push(n);
      } else {
        unmapped.push(n);
      }
    }
    return { mappable, unmapped };
  }, [data.nodes]);

  const precision = zoom < 1.5 ? 0 : zoom < 4 ? 1 : zoom < 8 ? 2 : 3;
  const clusters = useMemo(
    () => clusterNodes(mappable, precision),
    [mappable, precision],
  );

  const active = pinned ?? hovered;

  if (data.nodes.length === 0) {
    return (
      <div className="rounded-xl border border-ink-800 bg-ink-900 p-10 text-center text-sm text-slate-400">
        No appliances match the current filter.
      </div>
    );
  }

  return (
    <div className="space-y-2">
      <div className="flex flex-wrap items-center gap-2 rounded-md border border-ink-800 bg-ink-900 px-3 py-2 text-xs">
        <span className="rounded-full border border-ink-700 bg-ink-950 px-2.5 py-0.5 text-slate-300">
          {mappable.length} on map · {unmapped.length} no GeoIP fix
        </span>
        <button
          onClick={() => {
            setZoom(1);
            setCenter([0, 20]);
          }}
          className="rounded-full border border-ink-700 bg-ink-950 px-2.5 py-0.5 text-slate-200 hover:border-ink-600 hover:bg-ink-800"
        >
          Reset view
        </button>
        <span className="ml-auto text-[10px] text-slate-500">
          drag to pan · scroll to zoom · click marker for detail
        </span>
      </div>

      <div className="relative grid h-[68vh] gap-2 lg:grid-cols-[1fr_320px]">
        <div className="relative overflow-hidden rounded-xl border border-ink-800 bg-ink-950">
          <ComposableMap
            projection="geoEqualEarth"
            projectionConfig={{ scale: 175 }}
            style={{ width: "100%", height: "100%" }}
          >
            <ZoomableGroup
              zoom={zoom}
              center={center}
              onMoveEnd={({ coordinates, zoom: z }) => {
                setCenter(coordinates);
                setZoom(z);
              }}
              minZoom={1}
              maxZoom={32}
            >
              <Geographies geography={worldFeatures}>
                {({ geographies }) =>
                  geographies.map((geo) => (
                    <Geography
                      key={geo.rsmKey}
                      geography={geo}
                      fill="#0f172a"
                      stroke="#1e293b"
                      strokeWidth={0.4}
                      style={{
                        default: { outline: "none" },
                        hover: { outline: "none", fill: "#111c33" },
                        pressed: { outline: "none" },
                      }}
                    />
                  ))
                }
              </Geographies>

              {clusters.map((c) => {
                const dominant = pickDominantStatus(c.members);
                const r = clusterRadius(c.members.length) / Math.sqrt(zoom);
                const isActive =
                  active && active.lat === c.lat && active.lon === c.lon;
                return (
                  <Marker
                    key={`${c.lat}:${c.lon}`}
                    coordinates={[c.lon, c.lat]}
                    onMouseEnter={() => setHovered(c)}
                    onMouseLeave={() =>
                      setHovered((h) => (h === c ? null : h))
                    }
                    onClick={() => setPinned((p) => (p === c ? null : c))}
                    style={{
                      default: { cursor: "pointer" },
                      hover: { cursor: "pointer" },
                      pressed: { cursor: "pointer" },
                    }}
                  >
                    <g>
                      {c.members.length > 1 && (
                        <circle
                          r={r * 1.7}
                          fill={STATUS_FILL[dominant]}
                          opacity={0.18}
                        />
                      )}
                      <circle
                        r={r}
                        fill={STATUS_FILL[dominant]}
                        stroke={isActive ? "#7dd3fc" : "#0f172a"}
                        strokeWidth={isActive ? 2 : 1}
                      />
                      {c.members.length > 1 && (
                        <text
                          textAnchor="middle"
                          y={r / 3}
                          className="pointer-events-none select-none fill-white text-[8px] font-semibold"
                        >
                          {c.members.length}
                        </text>
                      )}
                    </g>
                  </Marker>
                );
              })}
            </ZoomableGroup>
          </ComposableMap>

          <Legend />

          {active && (
            <ClusterPopover
              cluster={active}
              pinned={pinned === active}
              onClose={() => setPinned(null)}
              onOpen={(n) => navigate(`/appliances/${n.id}`)}
            />
          )}

          {mappable.length === 0 && (
            <div className="absolute inset-0 grid place-items-center px-6 text-center text-xs text-slate-500">
              <div className="max-w-md space-y-1.5">
                <div className="text-sm font-semibold text-slate-300">
                  No GeoIP fixes for any appliance.
                </div>
                <div>
                  This is normal when every device is on a private mgmt IP.
                  Public-IP appliances (e.g. Meraki cloud, internet-facing
                  edge routers) will appear here once they're polled and
                  the API has the MaxMind GeoLite2 databases loaded
                  (<code className="font-mono">make refresh-geoip</code>).
                </div>
              </div>
            </div>
          )}
        </div>

        <UnmappedPanel nodes={unmapped} />
      </div>
    </div>
  );
}

function ClusterPopover({
  cluster,
  pinned,
  onClose,
  onOpen,
}: {
  cluster: Cluster;
  pinned: boolean;
  onClose: () => void;
  onOpen: (n: TopologyNode) => void;
}) {
  return (
    <div className="absolute right-3 top-3 max-h-[60vh] w-72 overflow-auto rounded-lg border border-ink-700 bg-ink-950/95 p-3 text-xs shadow-xl backdrop-blur">
      <div className="mb-2 flex items-baseline justify-between">
        <div>
          <div className="text-[10px] uppercase tracking-wide text-slate-500">
            {cluster.members.length} appliance
            {cluster.members.length === 1 ? "" : "s"}
          </div>
          <div className="font-mono text-[10px] text-slate-400">
            {cluster.lat.toFixed(2)}°, {cluster.lon.toFixed(2)}°
          </div>
        </div>
        {pinned && (
          <button
            onClick={onClose}
            className="text-[10px] text-slate-500 hover:text-slate-200"
          >
            close
          </button>
        )}
      </div>
      <ul className="space-y-1.5">
        {cluster.members.map((m) => (
          <li
            key={m.id}
            className="rounded border border-ink-800 bg-ink-900/60 p-1.5"
          >
            <div className="flex items-baseline justify-between gap-2">
              {m.kind === "appliance" ? (
                <button
                  onClick={() => onOpen(m)}
                  className="truncate font-medium text-sonar-300 hover:underline"
                >
                  {m.label}
                </button>
              ) : (
                <span className="truncate font-medium text-slate-200">
                  {m.label}
                </span>
              )}
              <span
                className="shrink-0 rounded-full px-1.5 py-0.5 text-[9px]"
                style={{
                  background: STATUS_FILL[m.status] + "33",
                  color: STATUS_FILL[m.status],
                }}
              >
                {m.status}
              </span>
            </div>
            <div className="flex items-center justify-between gap-2 text-[10px] text-slate-500">
              <span className="truncate">
                {m.city ? `${m.city}, ` : ""}
                {m.countryName || m.countryIso || "—"}
              </span>
              {m.mgmtIp && (
                <span className="shrink-0 font-mono">{m.mgmtIp}</span>
              )}
            </div>
            {(m.asn || m.org) && (
              <div className="truncate text-[10px] text-slate-600">
                {m.asn ? `AS${m.asn} ` : ""}
                {m.org || ""}
              </div>
            )}
            {m.kind === "foreign" && (
              <div className="text-[10px] italic text-slate-600">
                LLDP/CDP neighbor (not managed)
              </div>
            )}
          </li>
        ))}
      </ul>
    </div>
  );
}

function UnmappedPanel({ nodes }: { nodes: TopologyNode[] }) {
  // Group: private-network appliances first, then foreign neighbors
  // without an IP, then anything we couldn't resolve.
  const groups = useMemo(() => {
    const privateAppliances: TopologyNode[] = [];
    const noIp: TopologyNode[] = [];
    const noFix: TopologyNode[] = [];
    for (const n of nodes) {
      if (n.isPrivate) privateAppliances.push(n);
      else if (!n.mgmtIp) noIp.push(n);
      else noFix.push(n);
    }
    privateAppliances.sort((a, b) => a.label.localeCompare(b.label));
    noIp.sort((a, b) => a.label.localeCompare(b.label));
    noFix.sort((a, b) => a.label.localeCompare(b.label));
    return { privateAppliances, noIp, noFix };
  }, [nodes]);

  return (
    <div className="overflow-auto rounded-xl border border-ink-800 bg-ink-900/60 p-3 text-xs">
      <div className="mb-2 text-[10px] font-semibold uppercase tracking-wider text-slate-500">
        Not on the map ({nodes.length})
      </div>
      {groups.privateAppliances.length > 0 && (
        <UnmappedGroup
          title="Private network"
          help="MaxMind has no data for RFC1918 / link-local / loopback addresses."
          items={groups.privateAppliances}
        />
      )}
      {groups.noIp.length > 0 && (
        <UnmappedGroup
          title="No mgmt IP"
          help="Discovered via LLDP/CDP without a usable management address."
          items={groups.noIp}
        />
      )}
      {groups.noFix.length > 0 && (
        <UnmappedGroup
          title="No GeoIP fix"
          help="MaxMind didn't return a city/lat for the IP — common for fresh IP allocations and anycast prefixes."
          items={groups.noFix}
        />
      )}
      {nodes.length === 0 && (
        <div className="text-[11px] text-slate-500">
          Every node has a fix. Nothing to show here.
        </div>
      )}
    </div>
  );
}

function UnmappedGroup({
  title,
  help,
  items,
}: {
  title: string;
  help: string;
  items: TopologyNode[];
}) {
  return (
    <div className="mb-3 last:mb-0">
      <div className="mb-1 flex items-baseline justify-between gap-2">
        <span className="text-[10px] font-semibold uppercase tracking-wider text-slate-400">
          {title}
        </span>
        <span className="text-[10px] tabular-nums text-slate-600">
          {items.length}
        </span>
      </div>
      <div className="mb-1 text-[10px] text-slate-600">{help}</div>
      <ul className="space-y-1">
        {items.map((n) => (
          <li
            key={n.id}
            className="rounded border border-ink-800 bg-ink-950/60 p-1.5"
          >
            <div className="flex items-baseline justify-between gap-2">
              {n.kind === "appliance" ? (
                <Link
                  to={`/appliances/${n.id}`}
                  className="truncate font-medium text-sonar-300 hover:underline"
                >
                  {n.label}
                </Link>
              ) : (
                <span className="truncate font-medium text-slate-200">
                  {n.label}
                </span>
              )}
              <span
                className="shrink-0 rounded-full px-1.5 py-0.5 text-[9px]"
                style={{
                  background: STATUS_FILL[n.status] + "33",
                  color: STATUS_FILL[n.status],
                }}
              >
                {n.status}
              </span>
            </div>
            {n.mgmtIp && (
              <div className="font-mono text-[10px] text-slate-500">
                {n.mgmtIp}
              </div>
            )}
          </li>
        ))}
      </ul>
    </div>
  );
}

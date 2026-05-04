// World — geographic view of every reporting agent.
//
// Each agent's MaxMind-derived (geo_lat, geo_lon) is plotted on a
// world map; markers cluster gracefully when many agents share a
// city (Robinson projection makes Currituck County look reasonable).
//
// Filters mirror the Agents page so an operator can ask "where is
// my prod fleet?" by selecting site + tag + a process-name search.
//
// Privacy banner at the top is intentional: GeoIP can be wrong by
// hundreds of miles for residential ISPs and most operators are
// surprised when an agent shows up in a "wrong" city. The banner
// is collapsible but never auto-dismissed.

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { Link } from "react-router-dom";
import {
  ComposableMap,
  Geographies,
  Geography,
  Marker,
  ZoomableGroup,
} from "react-simple-maps";
import { feature } from "topojson-client";
import type { Topology, GeometryCollection } from "topojson-specification";
import worldTopo from "../assets/world-110m.json";
import { api } from "../api/client";
import type { Agent, Site } from "../api/types";
import { formatRelative } from "../lib/format";
import TagFilter from "../components/TagFilter";

// Types: react-simple-maps wants a GeoJSON FeatureCollection. We
// convert from TopoJSON once on module load so re-renders are cheap.
const worldFeatures = (() => {
  const topo = worldTopo as unknown as Topology;
  const obj = topo.objects.countries as GeometryCollection;
  const fc = feature(topo, obj) as unknown as GeoJSON.FeatureCollection;
  return fc;
})();

// Marker colors by status. We use the same palette as Topology so
// the two pages tell a consistent story.
const STATUS_COLOR = {
  online: "#22c55e",
  stale: "#f59e0b",
  offline: "#ef4444",
  unknown: "#64748b",
} as const;

interface MapAgent {
  id: string;
  hostname: string;
  status: keyof typeof STATUS_COLOR;
  publicIp?: string;
  city?: string;
  country?: string;
  org?: string;
  asn?: number;
  lat: number;
  lon: number;
  siteId?: string | null;
  tags?: string[];
  lastSeen?: string;
}

function statusOf(a: Agent): keyof typeof STATUS_COLOR {
  if (!a.lastMetricsAt) return "unknown";
  const ageMs = Date.now() - new Date(a.lastMetricsAt).getTime();
  if (ageMs < 5 * 60_000) return "online";
  if (ageMs < 60 * 60_000) return "stale";
  return "offline";
}

// Cluster nearby agents into single markers when they're within
// ~0.5° of each other (a few dozen miles). The map projection makes
// per-pixel clustering pointless because zoom warps the
// relationship; geographic clustering is what users actually mean
// when they say "I have a bunch of hosts in Norfolk".
interface Cluster {
  lat: number;
  lon: number;
  members: MapAgent[];
}

function clusterAgents(agents: MapAgent[], precision = 1): Cluster[] {
  // precision is in degrees of lat/lon rounded; default 1° (~70mi
  // at our latitude). At zoom > 4 we drop precision so cities
  // separate.
  const map = new Map<string, Cluster>();
  for (const a of agents) {
    const key = `${a.lat.toFixed(precision)}:${a.lon.toFixed(precision)}`;
    let c = map.get(key);
    if (!c) {
      c = { lat: 0, lon: 0, members: [] };
      map.set(key, c);
    }
    c.members.push(a);
  }
  // Re-center each cluster on the centroid of its members.
  return Array.from(map.values()).map((c) => ({
    members: c.members,
    lat:
      c.members.reduce((s, m) => s + m.lat, 0) / c.members.length,
    lon:
      c.members.reduce((s, m) => s + m.lon, 0) / c.members.length,
  }));
}

function clusterRadius(n: number): number {
  if (n === 1) return 4.5;
  if (n < 5) return 7;
  if (n < 20) return 10;
  return 13;
}

export default function World() {
  const agents = useQuery({
    queryKey: ["agents"],
    queryFn: () => api.get<Agent[]>("/agents"),
    refetchInterval: 60_000,
  });
  const sites = useQuery({
    queryKey: ["sites"],
    queryFn: () => api.get<Site[]>("/sites"),
  });

  const [siteFilter, setSiteFilter] = useState<string>("");
  const [tagFilter, setTagFilter] = useState<string[]>([]);
  const [search, setSearch] = useState("");
  const [showPrivacy, setShowPrivacy] = useState(true);
  const [hovered, setHovered] = useState<Cluster | null>(null);
  const [pinned, setPinned] = useState<Cluster | null>(null);
  const [zoom, setZoom] = useState(1);
  const [center, setCenter] = useState<[number, number]>([0, 20]);

  const allTags = useMemo(() => {
    const set = new Set<string>();
    for (const a of agents.data ?? []) {
      for (const t of a.tags ?? []) set.add(t);
    }
    return Array.from(set).sort();
  }, [agents.data]);

  const located = useMemo<MapAgent[]>(() => {
    return (agents.data ?? [])
      .filter((a) => a.geoLat != null && a.geoLon != null)
      .map((a) => ({
        id: a.id,
        hostname: a.hostname || a.id.slice(0, 8),
        status: statusOf(a),
        publicIp: a.publicIp ?? undefined,
        city: a.geoCity ?? undefined,
        country: a.geoCountryName ?? a.geoCountryIso ?? undefined,
        org: a.geoOrg ?? undefined,
        asn: a.geoAsn ?? undefined,
        lat: a.geoLat as number,
        lon: a.geoLon as number,
        siteId: a.siteId ?? undefined,
        tags: a.tags ?? [],
        lastSeen: a.lastMetricsAt ?? undefined,
      }));
  }, [agents.data]);

  const filtered = useMemo<MapAgent[]>(() => {
    const q = search.trim().toLowerCase();
    return located.filter((a) => {
      if (siteFilter && a.siteId !== siteFilter) return false;
      if (tagFilter.length > 0) {
        const tags = new Set(a.tags ?? []);
        for (const t of tagFilter) if (!tags.has(t)) return false;
      }
      if (q) {
        const hay =
          a.hostname.toLowerCase() +
          " " +
          (a.city ?? "").toLowerCase() +
          " " +
          (a.country ?? "").toLowerCase() +
          " " +
          (a.org ?? "").toLowerCase() +
          " " +
          (a.publicIp ?? "");
        if (!hay.includes(q)) return false;
      }
      return true;
    });
  }, [located, siteFilter, tagFilter, search]);

  // Drop one decimal of clustering precision per ~2x zoom so cities
  // separate as the user zooms in.
  const precision = zoom < 1.5 ? 0 : zoom < 4 ? 1 : zoom < 8 ? 2 : 3;
  const clusters = useMemo(() => clusterAgents(filtered, precision), [
    filtered,
    precision,
  ]);

  const totalLocated = located.length;
  const totalUnlocated = (agents.data?.length ?? 0) - totalLocated;

  const active = pinned ?? hovered;

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h2 className="text-2xl font-semibold tracking-tight">World</h2>
          <p className="mt-0.5 text-xs text-slate-500">
            Each agent's public IP is mapped via MaxMind GeoLite2 (offline,
            country + city accuracy). Refreshes every minute.
          </p>
        </div>
        <div className="flex flex-wrap items-center gap-2 text-xs">
          <span className="rounded-full border border-ink-700 bg-ink-900 px-2.5 py-1 text-slate-300">
            {filtered.length} on map · {totalLocated} located ·{" "}
            {totalUnlocated} no GeoIP
          </span>
          <button
            onClick={() => {
              setZoom(1);
              setCenter([0, 20]);
            }}
            className="rounded-full border border-ink-700 bg-ink-900 px-3 py-1 text-slate-200 hover:border-ink-600 hover:bg-ink-800"
          >
            Reset view
          </button>
        </div>
      </div>

      {showPrivacy && (
        <div className="flex items-start justify-between gap-3 rounded-md border border-amber-900/60 bg-amber-950/30 px-3 py-2 text-[11px] text-amber-200">
          <div className="space-y-0.5">
            <div className="font-semibold">Heads up: GeoIP is approximate.</div>
            <div className="opacity-80">
              MaxMind locates IPs to the city/region level for major ISPs but
              can be off by hundreds of miles for residential, mobile, and VPN
              addresses. Treat marker positions as "where the ISP says this IP
              lives," not where the box physically is.
            </div>
          </div>
          <button
            onClick={() => setShowPrivacy(false)}
            className="shrink-0 rounded border border-amber-800 px-2 py-0.5 text-[10px] uppercase tracking-wide text-amber-300 hover:bg-amber-900/30"
          >
            Got it
          </button>
        </div>
      )}

      <div className="flex flex-wrap items-center gap-2 rounded-md border border-ink-800 bg-ink-900 px-3 py-2 text-xs">
        <input
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder="Search hostname, city, country, ASN…"
          className="h-7 w-72 rounded-md border border-ink-700 bg-ink-950 px-2 text-slate-200 placeholder:text-slate-600 focus:border-sonar-500 focus:outline-none"
        />
        <select
          value={siteFilter}
          onChange={(e) => setSiteFilter(e.target.value)}
          className="h-7 rounded-md border border-ink-700 bg-ink-950 px-2 text-slate-200 focus:border-sonar-500 focus:outline-none"
        >
          <option value="">All sites</option>
          {(sites.data ?? []).map((s) => (
            <option key={s.id} value={s.id}>
              {s.name}
            </option>
          ))}
        </select>
        <TagFilter
          availableTags={allTags}
          selected={tagFilter}
          onChange={setTagFilter}
          mode="and"
        />
        <span className="ml-auto text-[10px] text-slate-500">
          drag to pan · scroll to zoom · click marker for detail
        </span>
      </div>

      <div className="relative h-[68vh] overflow-hidden rounded-xl border border-ink-800 bg-ink-950">
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
                    fill="var(--map-land)"
                    stroke="var(--map-stroke)"
                    strokeWidth={0.4}
                    style={{
                      default: { outline: "none" },
                      hover: { outline: "none", fill: "var(--map-hover)" },
                      pressed: { outline: "none" },
                    }}
                  />
                ))
              }
            </Geographies>

            {clusters.map((c) => {
              const dominant = pickDominantStatus(c.members);
              const r = clusterRadius(c.members.length) / Math.sqrt(zoom);
              const isActive = active && active.lat === c.lat && active.lon === c.lon;
              return (
                <Marker
                  key={`${c.lat}:${c.lon}`}
                  coordinates={[c.lon, c.lat]}
                  onMouseEnter={() => setHovered(c)}
                  onMouseLeave={() => setHovered((h) => (h === c ? null : h))}
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
                        fill={STATUS_COLOR[dominant]}
                        opacity={0.18}
                      />
                    )}
                    <circle
                      r={r}
                      fill={STATUS_COLOR[dominant]}
                      stroke={isActive ? "#7dd3fc" : "var(--map-land)"}
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
        {active && <ClusterPopover cluster={active} pinned={pinned === active} onClose={() => setPinned(null)} />}
      </div>

      {totalUnlocated > 0 && (
        <div className="rounded-md border border-ink-800 bg-ink-900 px-3 py-2 text-[11px] text-slate-400">
          {totalUnlocated} agent{totalUnlocated === 1 ? "" : "s"} have no GeoIP
          fix yet — typical reasons: probe older than 2026.4.26.x (no
          public-IP discovery), MaxMind databases not loaded on the API,
          or the host's public IP falls into a reserved/anycast block.
        </div>
      )}
    </div>
  );
}

function pickDominantStatus(members: MapAgent[]): MapAgent["status"] {
  const order: MapAgent["status"][] = ["offline", "stale", "online", "unknown"];
  for (const s of order) {
    if (members.some((m) => m.status === s)) return s;
  }
  return "unknown";
}

function ClusterPopover({
  cluster,
  pinned,
  onClose,
}: {
  cluster: Cluster;
  pinned: boolean;
  onClose: () => void;
}) {
  return (
    <div className="absolute right-3 top-3 max-h-[60vh] w-72 overflow-auto rounded-lg border border-ink-700 bg-ink-950/95 p-3 text-xs shadow-xl backdrop-blur">
      <div className="mb-2 flex items-baseline justify-between">
        <div>
          <div className="text-[10px] uppercase tracking-wide text-slate-500">
            {cluster.members.length} agent{cluster.members.length === 1 ? "" : "s"}
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
              <Link
                to={`/agents/${m.id}`}
                className="truncate font-medium text-sonar-300 hover:underline"
              >
                {m.hostname}
              </Link>
              <span
                className="shrink-0 rounded-full px-1.5 py-0.5 text-[9px]"
                style={{
                  background: STATUS_COLOR[m.status] + "22",
                  color: STATUS_COLOR[m.status],
                }}
              >
                {m.status}
              </span>
            </div>
            <div className="flex items-center justify-between gap-2 text-[10px] text-slate-500">
              <span className="truncate">
                {m.city ? `${m.city}, ` : ""}
                {m.country || "—"}
              </span>
              {m.publicIp && (
                <span className="shrink-0 font-mono">{m.publicIp}</span>
              )}
            </div>
            {(m.asn || m.org) && (
              <div className="truncate text-[10px] text-slate-600">
                {m.asn ? `AS${m.asn} ` : ""}
                {m.org || ""}
              </div>
            )}
            {m.lastSeen && (
              <div className="text-[10px] text-slate-600">
                seen {formatRelative(m.lastSeen)}
              </div>
            )}
          </li>
        ))}
      </ul>
    </div>
  );
}

function Legend() {
  return (
    <div className="absolute bottom-3 left-3 flex items-center gap-3 rounded-md border border-ink-800 bg-ink-950/80 px-3 py-1.5 text-[10px] uppercase tracking-wider text-slate-400 backdrop-blur">
      <Dot color={STATUS_COLOR.online} /> online
      <Dot color={STATUS_COLOR.stale} /> stale
      <Dot color={STATUS_COLOR.offline} /> offline
      <Dot color={STATUS_COLOR.unknown} /> unknown
    </div>
  );
}

function Dot({ color }: { color: string }) {
  return (
    <span
      className="inline-block h-2.5 w-2.5 rounded-full"
      style={{ background: color }}
    />
  );
}

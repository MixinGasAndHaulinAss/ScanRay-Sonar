// OverviewMiniMap — a compact, non-interactive world map for the
// Devices · Performance overview card. Uses the same react-simple-maps
// + topojson pipeline as pages/World.tsx but trimmed to the essentials
// (no zoom controls, no filters, no per-host hover popover) so it
// drops into a card and renders fast.
//
// Markers are colored by the agent's composite score so an operator
// can spot geographic clusters of unhealthy hosts at a glance:
//   green (>=8), sky (5-7.9), amber (3-4.9), red (<3).

import { ComposableMap, Geographies, Geography, Marker } from "react-simple-maps";
import { feature } from "topojson-client";
import type { GeometryCollection, Topology } from "topojson-specification";
import worldTopo from "../assets/world-110m.json";

const worldFeatures = (() => {
  const topo = worldTopo as unknown as Topology;
  const obj = topo.objects.countries as GeometryCollection;
  return feature(topo, obj) as unknown as GeoJSON.FeatureCollection;
})();

interface MapHost {
  id: string;
  hostname: string;
  lat: number;
  lon: number;
  score: number;
}

function scoreColor(score: number): string {
  if (score >= 8) return "#22c55e";
  if (score >= 5) return "#0ea5e9";
  if (score >= 3) return "#f59e0b";
  return "#ef4444";
}

interface Cluster {
  lat: number;
  lon: number;
  members: MapHost[];
  worstScore: number;
}

function cluster(hosts: MapHost[], precision = 1): Cluster[] {
  const buckets = new Map<string, MapHost[]>();
  for (const h of hosts) {
    const key = `${h.lat.toFixed(precision)}:${h.lon.toFixed(precision)}`;
    let arr = buckets.get(key);
    if (!arr) {
      arr = [];
      buckets.set(key, arr);
    }
    arr.push(h);
  }
  return Array.from(buckets.values()).map((members) => ({
    members,
    lat: members.reduce((s, m) => s + m.lat, 0) / members.length,
    lon: members.reduce((s, m) => s + m.lon, 0) / members.length,
    worstScore: members.reduce((s, m) => Math.min(s, m.score), 10),
  }));
}

export default function OverviewMiniMap({
  hosts,
  height = 240,
}: {
  hosts: MapHost[];
  height?: number;
}) {
  const clusters = cluster(hosts, 1);
  return (
    <div
      className="overflow-hidden rounded-md border border-ink-800 bg-ink-950"
      style={{ height }}
    >
      <ComposableMap
        projection="geoEqualEarth"
        projectionConfig={{ scale: 145 }}
        style={{ width: "100%", height: "100%" }}
      >
        <Geographies geography={worldFeatures}>
          {({ geographies }) =>
            geographies.map((g) => (
              <Geography
                key={g.rsmKey}
                geography={g}
                fill="#0f172a"
                stroke="#1e293b"
                strokeWidth={0.35}
                style={{
                  default: { outline: "none" },
                  hover: { outline: "none", fill: "#0f172a" },
                  pressed: { outline: "none" },
                }}
              />
            ))
          }
        </Geographies>

        {clusters.map((c) => {
          const r = c.members.length === 1 ? 3.5 : c.members.length < 5 ? 5 : 7;
          const fill = scoreColor(c.worstScore);
          const label =
            c.members.length === 1
              ? `${c.members[0].hostname} (score ${c.members[0].score.toFixed(1)})`
              : `${c.members.length} hosts at this location · worst score ${c.worstScore.toFixed(1)}`;
          return (
            <Marker key={`${c.lat}:${c.lon}`} coordinates={[c.lon, c.lat]}>
              <title>{label}</title>
              {c.members.length > 1 && (
                <circle r={r * 1.6} fill={fill} opacity={0.22} />
              )}
              <circle r={r} fill={fill} stroke="#0f172a" strokeWidth={1} />
              {c.members.length > 1 && (
                <text
                  textAnchor="middle"
                  y={r / 3}
                  className="pointer-events-none select-none fill-white text-[7px] font-semibold"
                >
                  {c.members.length}
                </text>
              )}
            </Marker>
          );
        })}
      </ComposableMap>
    </div>
  );
}

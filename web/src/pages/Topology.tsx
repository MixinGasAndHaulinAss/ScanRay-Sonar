// Topology — switch fabric + Meraki WAN/VPN view. Renders managed
// appliances, foreign neighbors, Internet cloud, and tunnel edges as a
// draggable force-directed graph with zoom/pan.

import { useEffect, useMemo, useRef, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api } from "../api/client";
import type { Topology, TopologyEdge, TopologyNode } from "../api/types";
import ForceGraph, {
  type ForceEdgeInput,
  type ForceGraphHandle,
  type ForceNodeInput,
  type SimNode,
} from "../components/ForceGraph";
import TopologyFilterBar, {
  filterTopologyByTags,
  type TagMatchMode,
} from "../components/TopologyFilterBar";

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
  if (n.kind === "cloud") return 28;
  if (n.kind === "foreign") return 16;
  return 22 + Math.min((n.uplinkCount ?? 0) * 1.5, 8);
}

function linkMedium(e: TopologyEdge): string {
  const m = (e.linkKind as { medium?: unknown } | undefined)?.medium;
  return typeof m === "string" ? m.toLowerCase() : "";
}

function linkLayer(e: TopologyEdge): number {
  return Number((e.linkKind as { layer?: unknown } | undefined)?.layer) || 0;
}

function isVPNEdge(e: TopologyEdge): boolean {
  return linkMedium(e) === "vpn" || e.protocol.includes("vpn");
}

function isWANEdge(e: TopologyEdge): boolean {
  return linkMedium(e) === "wan" || e.protocol === "uplink";
}

interface TopoNode extends ForceNodeInput {
  ref: TopologyNode;
}

interface TopoEdge extends ForceEdgeInput {
  ref: TopologyEdge;
}

const TAG_FILTER_KEY = "sonar.topology.tags";
const TAG_MODE_KEY = "sonar.topology.tagMode";

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

function seedPosition(
  n: TopologyNode,
  w: number,
  h: number,
): { initialX: number; initialY: number } {
  // Soft hierarchy: Internet near top, firewalls under it, switches lower.
  if (n.kind === "cloud") {
    return { initialX: w * 0.5, initialY: h * 0.12 };
  }
  const tags = new Set((n.tags ?? []).map((t) => t.toLowerCase()));
  const isFW = tags.has("firewall") || /mx|firewall|asa|palo|forti/i.test(n.model ?? n.label);
  const isAP = tags.has("wap") || /mr|access.?point/i.test(n.model ?? n.label);
  let u = 0;
  for (let i = 0; i < n.id.length; i++) u = (u * 31 + n.id.charCodeAt(i)) >>> 0;
  const jitterX = ((u % 1000) / 1000 - 0.5) * w * 0.55;
  if (isFW) {
    return { initialX: w * 0.5 + jitterX * 0.5, initialY: h * 0.28 + ((u % 80) - 40) };
  }
  if (isAP) {
    return { initialX: w * 0.5 + jitterX, initialY: h * 0.78 + ((u % 60) - 30) };
  }
  if (n.kind === "foreign") {
    return { initialX: w * 0.5 + jitterX, initialY: h * 0.55 + ((u % 100) - 50) };
  }
  return { initialX: w * 0.5 + jitterX, initialY: h * 0.55 + ((u % 120) - 60) };
}

export default function Topology() {
  const [includePhones, setIncludePhones] = useState(() => {
    return localStorage.getItem("sonar.topology.includePhones") === "1";
  });
  useEffect(() => {
    localStorage.setItem("sonar.topology.includePhones", includePhones ? "1" : "0");
  }, [includePhones]);

  const [tagFilter, setTagFilter] = useState<string[]>(loadTags);
  useEffect(() => {
    localStorage.setItem(TAG_FILTER_KEY, JSON.stringify(tagFilter));
  }, [tagFilter]);

  const [tagMode, setTagMode] = useState<TagMatchMode>(loadTagMode);
  useEffect(() => {
    localStorage.setItem(TAG_MODE_KEY, tagMode);
  }, [tagMode]);

  const { data, isLoading, error, refetch, isFetching } = useQuery({
    queryKey: ["topology", includePhones],
    queryFn: () =>
      api.get<Topology>(
        includePhones ? "/topology?includePhones=1" : "/topology",
      ),
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
    return filterTopologyByTags(data, tagFilter, tagMode);
  }, [data, tagFilter, tagMode]);

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h2 className="text-2xl font-semibold tracking-tight">Topology</h2>
          <p className="mt-0.5 text-xs text-slate-500">
            LLDP/CDP fabric plus Meraki WAN uplinks and Auto VPN. Drag nodes;
            scroll to zoom. Refreshes every 30 seconds.
          </p>
        </div>
        <TopologyFilterBar
          availableTags={allTags}
          selectedTags={tagFilter}
          onTagsChange={setTagFilter}
          matchMode={tagMode}
          onMatchModeChange={setTagMode}
          includePhones={includePhones}
          onIncludePhonesChange={setIncludePhones}
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

export function TopologyGraph({ data }: { data: Topology }) {
  const navigate = useNavigate();
  const graphRef = useRef<ForceGraphHandle>(null);
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

  const nodes: TopoNode[] = data.nodes.map((n) => {
    const seed = seedPosition(n, size.w, size.h);
    return { id: n.id, ref: n, initialX: seed.initialX, initialY: seed.initialY };
  });
  const edges: TopoEdge[] = data.edges.map((e) => ({
    from: e.from,
    to: e.to,
    rest: isVPNEdge(e) || isWANEdge(e) ? 200 : 140,
    ref: e,
  }));

  return (
    <div ref={wrapRef} className="relative h-[72vh] overflow-hidden rounded-xl border border-ink-800 bg-ink-900">
      {noEdges && (
        <div className="absolute left-3 top-3 z-10 max-w-md rounded-md border border-amber-900/60 bg-amber-950/40 px-3 py-2 text-xs text-amber-200">
          {onlyManaged
            ? "Showing managed appliances only — no LLDP/CDP neighbors or WAN/VPN links discovered yet."
            : "No discovery links yet."}{" "}
          Enable LLDP/CDP on switches, or Meraki Dashboard sync for MX WAN/VPN edges.
        </div>
      )}

      <div className="absolute right-3 top-3 z-10 flex gap-1">
        <button
          type="button"
          className="rounded border border-ink-700 bg-ink-950/90 px-2 py-1 text-xs text-slate-300 hover:bg-ink-800"
          onClick={() => graphRef.current?.zoomBy(1.2)}
          title="Zoom in"
        >
          +
        </button>
        <button
          type="button"
          className="rounded border border-ink-700 bg-ink-950/90 px-2 py-1 text-xs text-slate-300 hover:bg-ink-800"
          onClick={() => graphRef.current?.zoomBy(1 / 1.2)}
          title="Zoom out"
        >
          −
        </button>
        <button
          type="button"
          className="rounded border border-ink-700 bg-ink-950/90 px-2 py-1 text-xs text-slate-300 hover:bg-ink-800"
          onClick={() => graphRef.current?.fit(50)}
          title="Fit all nodes"
        >
          Fit
        </button>
        <button
          type="button"
          className="rounded border border-ink-700 bg-ink-950/90 px-2 py-1 text-xs text-slate-300 hover:bg-ink-800"
          onClick={() => graphRef.current?.resetView()}
          title="Reset pan/zoom"
        >
          Reset
        </button>
      </div>

      <ForceGraph<TopoNode, TopoEdge>
        ref={graphRef}
        nodes={nodes}
        edges={edges}
        width={size.w}
        height={size.h}
        enableZoomPan
        nodeRadius={(n) => nodeRadius(n.ref)}
        worldPadding={56}
        renderEdge={(e, a, b) => {
          const vpn = isVPNEdge(e.ref);
          const wan = isWANEdge(e.ref);
          const layer = linkLayer(e.ref);
          let sw = layer === 2 ? 2 : 1.4;
          if (vpn || wan) sw = 2.2;
          const util = e.ref.utilizationPct;
          let stroke = e.ref.operUp ? "#475569" : "#1e293b";
          if (vpn) stroke = e.ref.operUp ? "#a855f7" : "#4c1d95";
          else if (wan) stroke = e.ref.operUp ? "#f59e0b" : "#78350f";
          else if (util != null) {
            if (util >= 80) stroke = "#ef4444";
            else if (util >= 50) stroke = "#f59e0b";
            else stroke = "#22c55e";
          }
          const midX = (a.x + b.x) / 2;
          const midY = (a.y + b.y) / 2;
          let label: string | null = null;
          if (vpn) label = e.ref.fromPort || "VPN";
          else if (wan) label = e.ref.fromPort || "WAN";
          else if (util != null) label = `${util.toFixed(0)}%`;
          else if (e.ref.inBps != null || e.ref.outBps != null)
            label = formatEdgeBps(e.ref.inBps, e.ref.outBps);
          const dashed = !e.ref.operUp || vpn;
          return (
            <g key={`${e.from}->${e.to}:${e.ref.protocol}:${e.ref.fromPort ?? ""}`}>
              <line
                x1={a.x}
                y1={a.y}
                x2={b.x}
                y2={b.y}
                stroke={stroke}
                strokeWidth={sw}
                strokeDasharray={dashed ? "6 4" : undefined}
                opacity={0.9}
              />
              {label && (
                <text
                  x={midX}
                  y={midY - 4}
                  textAnchor="middle"
                  className="pointer-events-none select-none fill-slate-300 font-mono text-[9px]"
                >
                  {label}
                </text>
              )}
            </g>
          );
        }}
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

  if (n.kind === "cloud") {
    return (
      <g>
        <CloudGlyph cx={sim.x} cy={sim.y} r={r} />
        <text
          x={sim.x}
          y={sim.y + r + 14}
          textAnchor="middle"
          className="pointer-events-none select-none fill-slate-200 text-[11px] font-medium"
        >
          {n.label}
        </text>
      </g>
    );
  }

  const isAppliance = n.kind === "appliance";
  const fill = isAppliance ? STATUS_FILL[n.status] : "#1e293b";
  const ring = isAppliance ? STATUS_RING[n.status] : "#475569";
  const title = [n.label, ...(n.tags ?? []).slice(0, 6)].join(" · ");

  return (
    <g>
      <title>{title}</title>
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

function CloudGlyph({ cx, cy, r }: { cx: number; cy: number; r: number }) {
  const s = r / 28;
  return (
    <g transform={`translate(${cx - 28 * s}, ${cy - 18 * s}) scale(${s})`}>
      <path
        d="M22 36h34c8 0 14-6 14-13s-6-13-14-13c-1.5-7-8-12-15.5-12-7 0-13 4-15.5 10C20 8 14 13 14 20c0 1 .1 2 .3 3C8.5 24 4 29 4 35c0 6 5 11 12 11h6"
        fill="#0f172a"
        stroke="#38bdf8"
        strokeWidth="2"
      />
    </g>
  );
}

function Legend() {
  return (
    <div className="absolute bottom-3 right-3 flex max-w-md flex-col gap-2 rounded-md border border-ink-800 bg-ink-950/85 px-3 py-2 text-[10px] uppercase tracking-wider text-slate-400 backdrop-blur">
      <div className="flex flex-wrap items-center gap-3">
        <Dot color={STATUS_FILL.up} /> up
        <Dot color={STATUS_FILL.degraded} /> degraded
        <Dot color={STATUS_FILL.down} /> down
        <Dot color="#1e293b" ring="#475569" /> foreign
        <span className="inline-flex items-center gap-1 normal-case tracking-normal text-sky-300">
          ○ Internet
        </span>
      </div>
      <div className="flex flex-wrap items-center gap-3 border-t border-ink-800 pt-2 normal-case tracking-normal text-slate-500">
        <span className="text-[9px] uppercase tracking-wide text-slate-400">Edges</span>
        <span>L2 thicker</span>
        <span className="text-amber-400">WAN</span>
        <span className="text-purple-400">VPN (dashed)</span>
        <span className="text-green-400">Util &lt;50%</span>
      </div>
    </div>
  );
}

export function TopologyLinkLegend() {
  return (
    <div className="flex flex-wrap gap-3 rounded-md border border-ink-800 bg-ink-900/60 p-3 text-xs text-slate-300">
      <span className="font-semibold text-slate-200">Link kinds:</span>
      <Swatch color="bg-sonar-400" label="L2 LLDP/CDP" />
      <Swatch color="bg-amber-400" label="WAN uplink → Internet" />
      <Swatch color="bg-purple-500" label="Meraki Auto VPN" />
      <Swatch color="bg-fuchsia-500" label="Third-party VPN" />
      <Swatch color="bg-rose-500" label="L3 OSPF (future)" />
      <Swatch color="bg-pink-500" label="L3 BGP (future)" />
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

function formatEdgeBps(inBps?: number, outBps?: number): string {
  const max = Math.max(inBps ?? 0, outBps ?? 0);
  if (max >= 1_000_000_000) return `${(max / 1_000_000_000).toFixed(1)}G`;
  if (max >= 1_000_000) return `${(max / 1_000_000).toFixed(1)}M`;
  if (max >= 1_000) return `${(max / 1_000).toFixed(0)}K`;
  return `${max}b`;
}

function Dot({ color, ring }: { color: string; ring?: string }) {
  return (
    <span className="inline-flex items-center gap-1">
      <span
        className="inline-block h-2.5 w-2.5 rounded-full"
        style={{ background: color, border: ring ? `1px solid ${ring}` : undefined }}
      />
    </span>
  );
}

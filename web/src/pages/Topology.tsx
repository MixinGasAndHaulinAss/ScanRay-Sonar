// Topology — switch fabric view. Renders managed appliances and
// foreign neighbors discovered via LLDP/CDP as a draggable
// force-directed graph.
//
// History note: an earlier revision rolled its own physics here.
// We now share the simulation with the per-host network graph on
// the agent detail page (see ../components/ForceGraph.tsx) so the
// two pages feel identical to operate.

import { useEffect, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api } from "../api/client";
import type { Topology, TopologyEdge, TopologyNode } from "../api/types";
import ForceGraph, {
  type ForceEdgeInput,
  type ForceNodeInput,
  type SimNode,
} from "../components/ForceGraph";

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

export default function Topology() {
  // Persist the phone-suppression preference per browser. Most operators
  // toggle this once and forget about it.
  const [includePhones, setIncludePhones] = useState(() => {
    return localStorage.getItem("sonar.topology.includePhones") === "1";
  });
  useEffect(() => {
    localStorage.setItem("sonar.topology.includePhones", includePhones ? "1" : "0");
  }, [includePhones]);

  const { data, isLoading, error, refetch, isFetching } = useQuery({
    queryKey: ["topology", includePhones],
    queryFn: () =>
      api.get<Topology>(
        includePhones ? "/topology?includePhones=1" : "/topology",
      ),
    refetchInterval: 30_000,
  });

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h2 className="text-2xl font-semibold tracking-tight">Topology</h2>
          <p className="mt-0.5 text-xs text-slate-500">
            Auto-discovered from LLDP and Cisco CDP on each appliance's last poll.
            Refreshes every 30 seconds. Drag any node to rearrange.
          </p>
        </div>
        <div className="flex items-center gap-2">
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
          <button
            onClick={() => refetch()}
            disabled={isFetching}
            className="rounded-full border border-ink-700 bg-ink-900 px-3 py-1.5 text-xs text-slate-200 hover:border-ink-600 hover:bg-ink-800 disabled:opacity-50"
          >
            {isFetching ? "Refreshing…" : "Refresh"}
          </button>
        </div>
      </div>

      {isLoading && <div className="text-sm text-slate-500">Loading topology…</div>}
      {error && (
        <div className="rounded-md border border-red-900/60 bg-red-950/30 px-3 py-2 text-sm text-red-300">
          Failed to load topology.
        </div>
      )}

      {data && <TopologyGraph data={data} />}
    </div>
  );
}

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
        No appliances yet. Add one under <Link to="/appliances" className="text-sonar-400 hover:underline">Appliances</Link>.
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
    <div ref={wrapRef} className="relative h-[72vh] overflow-hidden rounded-xl border border-ink-800 bg-ink-900">
      {noEdges && (
        <div className="absolute left-3 top-3 z-10 max-w-md rounded-md border border-amber-900/60 bg-amber-950/40 px-3 py-2 text-xs text-amber-200">
          {onlyManaged
            ? "Showing managed appliances only — no LLDP or CDP neighbors discovered yet."
            : "No discovery links yet."}{" "}
          Make sure LLDP/CDP is enabled on your switches; on Cisco IOS it's <code className="font-mono">cdp run</code> + <code className="font-mono">lldp run</code> globally.
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
        style={{ background: color, border: ring ? `1px solid ${ring}` : undefined }}
      />
    </span>
  );
}

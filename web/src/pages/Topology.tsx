import { useMemo, useRef, useState, useEffect } from "react";
import { Link } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api } from "../api/client";
import type { Topology, TopologyEdge, TopologyNode } from "../api/types";

// ---------------------------------------------------------------------------
// Force-directed layout
// ---------------------------------------------------------------------------
// We roll a tiny physics simulation here instead of pulling in d3-force or
// react-flow. For ≤ ~50 nodes it produces a perfectly readable layout in
// a couple hundred ticks of <1ms each, and adding a graph library to the
// bundle for one screen seemed wasteful. The simulation is deterministic
// (seeded by node ID hash) so the same topology renders the same way
// across reloads, which makes the map feel stable to operators.

interface Sim {
  id: string;
  x: number;
  y: number;
  vx: number;
  vy: number;
  fx?: number; // pinned x (during drag)
  fy?: number;
  ref: TopologyNode;
}

function hash(s: string): number {
  let h = 2166136261;
  for (let i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i);
    h = (h * 16777619) >>> 0;
  }
  return h;
}

function seededInit(node: TopologyNode, w: number, h: number): Sim {
  const u = hash(node.id);
  return {
    id: node.id,
    x: ((u % 1000) / 1000) * w,
    y: ((((u / 1000) | 0) % 1000) / 1000) * h,
    vx: 0,
    vy: 0,
    ref: node,
  };
}

function runLayout(nodes: Sim[], edges: TopologyEdge[], w: number, h: number) {
  const REPULSE = 22000; // pairwise repulsion strength (Coulomb)
  const SPRING = 0.04; // edge spring constant (Hooke)
  const REST = 140; // rest length of an edge in px
  const CENTER = 0.012; // mild gravity toward the canvas center
  const DAMP = 0.82; // velocity damping per tick
  const TICKS = 280;

  const idx = new Map<string, Sim>();
  nodes.forEach((n) => idx.set(n.id, n));

  for (let t = 0; t < TICKS; t++) {
    // Repulsion between every pair of nodes.
    for (let i = 0; i < nodes.length; i++) {
      const a = nodes[i];
      for (let j = i + 1; j < nodes.length; j++) {
        const b = nodes[j];
        let dx = a.x - b.x;
        let dy = a.y - b.y;
        let d2 = dx * dx + dy * dy;
        if (d2 < 0.01) {
          dx = (Math.random() - 0.5) * 0.1;
          dy = (Math.random() - 0.5) * 0.1;
          d2 = dx * dx + dy * dy + 0.01;
        }
        const f = REPULSE / d2;
        const inv = 1 / Math.sqrt(d2);
        const fx = dx * inv * f;
        const fy = dy * inv * f;
        a.vx += fx;
        a.vy += fy;
        b.vx -= fx;
        b.vy -= fy;
      }
    }

    for (const e of edges) {
      const a = idx.get(e.from);
      const b = idx.get(e.to);
      if (!a || !b) continue;
      const dx = b.x - a.x;
      const dy = b.y - a.y;
      const d = Math.sqrt(dx * dx + dy * dy) || 1;
      const f = SPRING * (d - REST);
      const fx = (dx / d) * f;
      const fy = (dy / d) * f;
      a.vx += fx;
      a.vy += fy;
      b.vx -= fx;
      b.vy -= fy;
    }

    for (const n of nodes) {
      n.vx += (w / 2 - n.x) * CENTER;
      n.vy += (h / 2 - n.y) * CENTER;
      n.vx *= DAMP;
      n.vy *= DAMP;
      n.x += n.vx;
      n.y += n.vy;
      n.x = Math.max(40, Math.min(w - 40, n.x));
      n.y = Math.max(40, Math.min(h - 40, n.y));
    }
  }
}

// ---------------------------------------------------------------------------
// Visual helpers
// ---------------------------------------------------------------------------

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
  // Slight visual scaling by uplink count so a core/agg switch reads as
  // "bigger" than an access switch even when port counts are similar.
  return 22 + Math.min((n.uplinkCount ?? 0) * 1.5, 8);
}

// ---------------------------------------------------------------------------
// Page
// ---------------------------------------------------------------------------

export default function Topology() {
  // Persist the phone-suppression preference per browser. Most operators
  // toggle this once and forget about it; storing the choice means a
  // reload doesn't snap back to "show phones" right when they were
  // trying to read the backbone.
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
            Refreshes every 30 seconds.
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
  // Lock canvas size; the layout reflows on resize via the dependency.
  const [size, setSize] = useState({ w: 1200, h: 720 });
  const wrapRef = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (!wrapRef.current) return;
    const ro = new ResizeObserver((entries) => {
      for (const e of entries) {
        const cr = e.contentRect;
        setSize({ w: Math.max(800, cr.width), h: Math.max(600, cr.height) });
      }
    });
    ro.observe(wrapRef.current);
    return () => ro.disconnect();
  }, []);

  // Recompute layout whenever the topology shape changes. We hash the
  // node+edge IDs so a poll that returns the same graph doesn't shuffle
  // positions even if React Query produced a new object reference.
  const sims = useMemo(() => {
    const nodes = data.nodes.map((n) => seededInit(n, size.w, size.h));
    runLayout(nodes, data.edges, size.w, size.h);
    return nodes;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [
    size.w,
    size.h,
    data.nodes.map((n) => n.id).join("|"),
    data.edges.map((e) => `${e.from}->${e.to}`).join("|"),
  ]);

  const byId = useMemo(() => {
    const m = new Map<string, Sim>();
    sims.forEach((s) => m.set(s.id, s));
    return m;
  }, [sims]);

  const [hover, setHover] = useState<string | null>(null);

  if (data.nodes.length === 0) {
    return (
      <div className="rounded-xl border border-ink-800 bg-ink-900 p-10 text-center text-sm text-slate-400">
        No appliances yet. Add one under <Link to="/appliances" className="text-sonar-400 hover:underline">Appliances</Link>.
      </div>
    );
  }

  const onlyManaged = data.nodes.every((n) => n.kind === "appliance");
  const noEdges = data.edges.length === 0;

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

      <svg width={size.w} height={size.h} className="block">
        <defs>
          <marker
            id="arrow"
            viewBox="0 -3 6 6"
            refX="6"
            refY="0"
            markerWidth="6"
            markerHeight="6"
            orient="auto"
          >
            <path d="M0,-3 L6,0 L0,3" fill="#475569" />
          </marker>
        </defs>

        {/* Edges first so nodes sit on top */}
        {data.edges.map((e) => {
          const a = byId.get(e.from);
          const b = byId.get(e.to);
          if (!a || !b) return null;
          const isHover = hover === e.from || hover === e.to;
          return (
            <line
              key={`${e.from}->${e.to}`}
              x1={a.x}
              y1={a.y}
              x2={b.x}
              y2={b.y}
              stroke={isHover ? "#0ea5e9" : e.operUp ? "#475569" : "#1e293b"}
              strokeWidth={isHover ? 2 : 1.4}
              strokeDasharray={e.operUp ? undefined : "4 4"}
              opacity={isHover ? 1 : 0.7}
            />
          );
        })}

        {sims.map((s) => (
          <NodeBubble key={s.id} sim={s} onHover={setHover} hovered={hover === s.id} />
        ))}
      </svg>

      <Legend />
    </div>
  );
}

function NodeBubble({
  sim,
  onHover,
  hovered,
}: {
  sim: Sim;
  onHover: (id: string | null) => void;
  hovered: boolean;
}) {
  const n = sim.ref;
  const r = nodeRadius(n);
  const isAppliance = n.kind === "appliance";
  const fill = isAppliance ? STATUS_FILL[n.status] : "#1e293b";
  const ring = isAppliance ? STATUS_RING[n.status] : "#475569";

  const inner = (
    <g
      className="cursor-pointer"
      onMouseEnter={() => onHover(n.id)}
      onMouseLeave={() => onHover(null)}
    >
      <circle
        cx={sim.x}
        cy={sim.y}
        r={r + (hovered ? 4 : 0)}
        fill={fill}
        stroke={ring}
        strokeWidth={hovered ? 2 : 1.5}
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

  return isAppliance ? (
    <Link to={`/appliances/${n.id}`}>{inner}</Link>
  ) : (
    inner
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

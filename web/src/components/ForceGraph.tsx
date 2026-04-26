// ForceGraph — a tiny, dependency-free force-directed graph that
// supports drag-to-reposition. Used by:
//
//   * Topology.tsx — switch fabric (LLDP/CDP)
//   * AgentDetail.tsx — per-host process→peer network graph
//
// Why a custom widget rather than d3-force / react-flow / vis-network:
//   * For ≤ ~200 nodes the math is dirt-cheap (<1 ms per tick with the
//     constants below) and the bundle savings are real (each of those
//     libraries adds 60–200 KB of JS to the SPA).
//   * Drag interaction is two event handlers; we don't need the
//     framework abstraction.
//   * Determinism: seeding positions from a hash of the node id makes
//     reloads stable, which is invaluable for "is this the same graph
//     I was looking at a minute ago?".
//
// Layout strategy:
//   * Pre-layout pass: 280 ticks of the standard repulsion + spring +
//     gentle gravity, run synchronously when the input changes. This
//     is what produces the initial layout the user sees.
//   * Live drag: while a node is held, we run a few ticks per
//     animation frame, with the dragged node pinned (fx/fy) so its
//     neighbors flow around it. Releasing the node un-pins and lets
//     the system relax for a few more ticks before settling.

import { useCallback, useEffect, useMemo, useRef, useState } from "react";

export interface ForceNodeInput {
  id: string;
  /** Optional initial position. We honour these to make the agent
   *  network graph put the host at the center on first paint. */
  initialX?: number;
  initialY?: number;
  /** If true, the node is permanently anchored at its current
   *  position and ignores forces. Used for the host node in the
   *  per-agent network graph. */
  pinned?: boolean;
}

export interface ForceEdgeInput {
  from: string;
  to: string;
  /** Optional spring rest length override; defaults to RESTPX. */
  rest?: number;
}

export interface SimNode<N extends ForceNodeInput> {
  id: string;
  x: number;
  y: number;
  vx: number;
  vy: number;
  fx?: number;
  fy?: number;
  data: N;
}

interface Props<N extends ForceNodeInput, E extends ForceEdgeInput> {
  nodes: N[];
  edges: E[];
  width: number;
  height: number;
  /** Renderer for a node — receives the simulated position. The
   *  outer SVG is provided by ForceGraph; this returns SVG children. */
  renderNode: (node: SimNode<N>, hovered: boolean) => React.ReactNode;
  /** Renderer for an edge. Most callers only need stroke colour and
   *  width; the from/to positions come pre-computed. */
  renderEdge?: (edge: E, from: SimNode<N>, to: SimNode<N>, hovered: boolean) => React.ReactNode;
  /** Optional callback when a node is single-clicked (no drag). */
  onNodeClick?: (node: N) => void;
  /** Optional callback when the user hovers a node. Pass null on leave. */
  onNodeHover?: (id: string | null) => void;
  /** Optional radius of the rendered node. Used to size the drag hit
   *  region; defaults to 22. */
  nodeRadius?: (node: N) => number;
}

const REPULSE = 22000;
const SPRING = 0.04;
const RESTPX = 140;
const CENTER = 0.012;
const DAMP = 0.82;
const PRELAYOUT_TICKS = 280;
const LIVE_TICKS_PER_FRAME = 6;

function hashCode(s: string): number {
  let h = 2166136261;
  for (let i = 0; i < s.length; i++) {
    h ^= s.charCodeAt(i);
    h = (h * 16777619) >>> 0;
  }
  return h;
}

function seededInit<N extends ForceNodeInput>(node: N, w: number, h: number): SimNode<N> {
  const u = hashCode(node.id);
  const x = node.initialX ?? ((u % 1000) / 1000) * w;
  const y = node.initialY ?? ((((u / 1000) | 0) % 1000) / 1000) * h;
  return {
    id: node.id,
    x,
    y,
    vx: 0,
    vy: 0,
    fx: node.pinned ? x : undefined,
    fy: node.pinned ? y : undefined,
    data: node,
  };
}

function tick<N extends ForceNodeInput, E extends ForceEdgeInput>(
  sims: SimNode<N>[],
  edges: E[],
  w: number,
  h: number,
) {
  const idx = new Map<string, SimNode<N>>();
  sims.forEach((s) => idx.set(s.id, s));
  for (let i = 0; i < sims.length; i++) {
    const a = sims[i];
    for (let j = i + 1; j < sims.length; j++) {
      const b = sims[j];
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
    const f = SPRING * (d - (e.rest ?? RESTPX));
    const fx = (dx / d) * f;
    const fy = (dy / d) * f;
    a.vx += fx;
    a.vy += fy;
    b.vx -= fx;
    b.vy -= fy;
  }
  for (const n of sims) {
    if (n.fx != null && n.fy != null) {
      n.x = n.fx;
      n.y = n.fy;
      n.vx = 0;
      n.vy = 0;
      continue;
    }
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

export default function ForceGraph<
  N extends ForceNodeInput,
  E extends ForceEdgeInput,
>({
  nodes,
  edges,
  width,
  height,
  renderNode,
  renderEdge,
  onNodeClick,
  onNodeHover,
}: Props<N, E>) {
  // Re-seed when the *shape* of the input changes (different node IDs
  // or edges). Pure prop-reference change without shape change does
  // NOT cause the layout to reflow — important so that 30s polls of
  // the same data don't shuffle positions while the operator looks.
  const shapeKey = useMemo(
    () =>
      `${nodes.map((n) => n.id).join("|")}::${edges.map((e) => `${e.from}->${e.to}`).join("|")}::${width}x${height}`,
    [nodes, edges, width, height],
  );

  const [sims, setSims] = useState<SimNode<N>[]>([]);
  // Bump on every tick to force a re-render. We don't useState for
  // sims-as-data because we mutate them in place inside RAF; tying
  // re-renders to a counter is cheaper than copying the sims array.
  const [tickN, setTickN] = useState(0);

  useEffect(() => {
    const fresh = nodes.map((n) => seededInit(n, width, height));
    for (let i = 0; i < PRELAYOUT_TICKS; i++) tick(fresh, edges, width, height);
    setSims(fresh);
    setTickN((n) => n + 1);
    // We deliberately depend ONLY on shapeKey: it already encodes
    // node IDs, edge endpoints, and canvas size. Adding the array
    // refs would re-seed on every parent render (fresh array refs
    // each time React Query refetches) which throws away the
    // operator's drag positions. See: docs/dev-notes/forcegraph.md
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [shapeKey]);

  const dragRef = useRef<{ id: string; pointerId: number } | null>(null);
  const rafRef = useRef<number | null>(null);
  const movedRef = useRef<boolean>(false);

  const stopRAF = useCallback(() => {
    if (rafRef.current != null) {
      cancelAnimationFrame(rafRef.current);
      rafRef.current = null;
    }
  }, []);

  const startLiveRelax = useCallback(
    (extraTicks: number) => {
      // Run a finite-but-substantial number of live ticks after a
      // drag release so the system can fully relax. Self-cancels via
      // counter.
      let remaining = extraTicks;
      const step = () => {
        if (remaining <= 0) {
          rafRef.current = null;
          return;
        }
        for (let i = 0; i < LIVE_TICKS_PER_FRAME; i++) {
          tick(sims, edges, width, height);
        }
        remaining -= LIVE_TICKS_PER_FRAME;
        setTickN((n) => n + 1);
        rafRef.current = requestAnimationFrame(step);
      };
      stopRAF();
      rafRef.current = requestAnimationFrame(step);
    },
    [sims, edges, width, height, stopRAF],
  );

  // Continuous live ticks while a drag is active.
  useEffect(() => {
    return () => stopRAF();
  }, [stopRAF]);

  const handlePointerDown = (e: React.PointerEvent, id: string) => {
    const sim = sims.find((s) => s.id === id);
    if (!sim) return;
    if (sim.data.pinned) return; // can't move pinned nodes
    e.currentTarget.setPointerCapture(e.pointerId);
    dragRef.current = { id, pointerId: e.pointerId };
    movedRef.current = false;
    sim.fx = sim.x;
    sim.fy = sim.y;

    // Begin a continuous-tick loop while dragging.
    const loop = () => {
      if (!dragRef.current) {
        rafRef.current = null;
        return;
      }
      for (let i = 0; i < LIVE_TICKS_PER_FRAME; i++) {
        tick(sims, edges, width, height);
      }
      setTickN((n) => n + 1);
      rafRef.current = requestAnimationFrame(loop);
    };
    stopRAF();
    rafRef.current = requestAnimationFrame(loop);
  };

  const handlePointerMove = (e: React.PointerEvent) => {
    const drag = dragRef.current;
    if (!drag || drag.pointerId !== e.pointerId) return;
    const sim = sims.find((s) => s.id === drag.id);
    if (!sim) return;
    const svg = (e.currentTarget as SVGElement).ownerSVGElement;
    if (!svg) return;
    const pt = svg.createSVGPoint();
    pt.x = e.clientX;
    pt.y = e.clientY;
    const ctm = svg.getScreenCTM();
    if (!ctm) return;
    const local = pt.matrixTransform(ctm.inverse());
    sim.fx = Math.max(40, Math.min(width - 40, local.x));
    sim.fy = Math.max(40, Math.min(height - 40, local.y));
    movedRef.current = true;
  };

  const handlePointerUp = (_e: React.PointerEvent, id: string) => {
    const drag = dragRef.current;
    dragRef.current = null;
    const sim = sims.find((s) => s.id === id);
    if (sim && !sim.data.pinned) {
      // Leave fx/fy set so the node "sticks" where the operator
      // dropped it. They can grab it again to move it. Setting
      // them to undefined would let the layout drift it back.
    }
    if (!movedRef.current && drag && onNodeClick && sim) {
      onNodeClick(sim.data);
    }
    startLiveRelax(60);
  };

  return (
    <svg width={width} height={height} className="block touch-none">
      <defs>
        <marker
          id="fg-arrow"
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

      {/* tickN is read here so React re-renders when the simulation
          ticks; we deliberately do NOT re-key the wrapper, which
          would unmount the children mid-drag and drop the SVG's
          pointer capture. */}
      <g data-tick={tickN}>
        {edges.map((e) => {
          const a = sims.find((s) => s.id === e.from);
          const b = sims.find((s) => s.id === e.to);
          if (!a || !b) return null;
          if (renderEdge) return renderEdge(e, a, b, false);
          return (
            <line
              key={`${e.from}->${e.to}`}
              x1={a.x}
              y1={a.y}
              x2={b.x}
              y2={b.y}
              stroke="#475569"
              strokeWidth={1.4}
              opacity={0.7}
            />
          );
        })}
        {sims.map((s) => (
          <g
            key={s.id}
            onPointerDown={(e) => handlePointerDown(e, s.id)}
            onPointerMove={handlePointerMove}
            onPointerUp={(e) => handlePointerUp(e, s.id)}
            onPointerCancel={(e) => handlePointerUp(e, s.id)}
            onMouseEnter={() => onNodeHover?.(s.id)}
            onMouseLeave={() => onNodeHover?.(null)}
            style={{ cursor: s.data.pinned ? "default" : "grab" }}
          >
            {renderNode(s, false)}
          </g>
        ))}
      </g>
    </svg>
  );
}

// ForceGraph — a tiny, dependency-free force-directed graph that
// supports drag-to-reposition, optional pan/zoom, and a programmatic
// view handle (centerOn / fit / zoomBy) used by the per-agent
// network topology canvas to drive the +/- buttons and Reset view.
//
// Used by:
//   * Topology.tsx — switch fabric (LLDP/CDP)
//   * AgentNetworkGraph.tsx — per-host process→peer network graph
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
//
// Pan/zoom (when enableZoomPan is true):
//   * Wheel anywhere on the SVG: zoom about the cursor.
//   * Drag empty space: pan.
//   * Pointer events on nodes still drag-the-node, because the
//     background hit area is a single transparent <rect> placed
//     under the node group.

import {
  forwardRef,
  useCallback,
  useEffect,
  useImperativeHandle,
  useMemo,
  useRef,
  useState,
} from "react";

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

export interface ForceGraphHandle {
  /** Reset the pan/zoom view to identity (1×, no translation). */
  resetView(): void;
  /** Multiply the zoom factor by k about the canvas center. */
  zoomBy(k: number): void;
  /** Center the view on a node by id (no zoom change). */
  centerOn(id: string): void;
  /** Fit all nodes inside the viewport with margin. */
  fit(margin?: number): void;
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
  /** Optional callback when the user clicks empty space (cancels selection). */
  onBackgroundClick?: () => void;
  /** Optional radius of the rendered node. Used to size the drag hit
   *  region; defaults to 22. */
  nodeRadius?: (node: N) => number;
  /** When true, the wheel zooms and dragging the empty background pans. */
  enableZoomPan?: boolean;
  /** SVG-coordinate margin to enforce around node positions. Defaults 40. */
  worldPadding?: number;
  /** When true, the simulation is OFF. Initial positions come from
   *  initialX/initialY on each node (so the parent owns the layout),
   *  and dragging just moves the single grabbed node — no force
   *  propagation, no relax pass, no spasm. Re-seeds whenever the
   *  layout key changes (we hash node ids + initial positions into
   *  shapeKey so a parent re-layout is honoured). */
  staticLayout?: boolean;
}

const REPULSE = 22000;
const SPRING = 0.04;
const RESTPX = 140;
const CENTER = 0.012;
const DAMP = 0.82;
const PRELAYOUT_TICKS = 280;
const LIVE_TICKS_PER_FRAME = 6;
const MIN_ZOOM = 0.2;
const MAX_ZOOM = 4;

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
  pad: number,
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
    n.x = Math.max(pad, Math.min(w - pad, n.x));
    n.y = Math.max(pad, Math.min(h - pad, n.y));
  }
}

function ForceGraphInner<
  N extends ForceNodeInput,
  E extends ForceEdgeInput,
>(
  {
    nodes,
    edges,
    width,
    height,
    renderNode,
    renderEdge,
    onNodeClick,
    onNodeHover,
    onBackgroundClick,
    enableZoomPan = false,
    worldPadding = 40,
    staticLayout = false,
  }: Props<N, E>,
  ref: React.Ref<ForceGraphHandle>,
) {
  // Re-seed when the *shape* of the input changes (different node IDs
  // or edges). Pure prop-reference change without shape change does
  // NOT cause the layout to reflow — important so that 30s polls of
  // the same data don't shuffle positions while the operator looks.
  //
  // For staticLayout we *also* hash initial positions (rounded to
  // nearest 5 px to avoid jitter) so a parent recompute of the
  // layout is honoured. For force mode we deliberately omit
  // width/height so a 1-pixel resize doesn't blow away positions.
  const shapeKey = useMemo(() => {
    const base = `${nodes.map((n) => n.id).join("|")}::${edges
      .map((e) => `${e.from}->${e.to}`)
      .join("|")}`;
    if (!staticLayout) return base;
    const positions = nodes
      .map((n) => {
        const x = Math.round((n.initialX ?? 0) / 5) * 5;
        const y = Math.round((n.initialY ?? 0) / 5) * 5;
        return `${n.id}@${x},${y}`;
      })
      .join("|");
    return `${base}::${positions}`;
  }, [nodes, edges, staticLayout]);

  const [sims, setSims] = useState<SimNode<N>[]>([]);
  // Bump on every tick to force a re-render. We don't useState for
  // sims-as-data because we mutate them in place inside RAF; tying
  // re-renders to a counter is cheaper than copying the sims array.
  const [tickN, setTickN] = useState(0);

  // View transform for pan/zoom (only used when enableZoomPan).
  const [view, setView] = useState({ tx: 0, ty: 0, k: 1 });

  const svgRef = useRef<SVGSVGElement | null>(null);
  const groupRef = useRef<SVGGElement | null>(null);

  useEffect(() => {
    const fresh = nodes.map((n) => seededInit(n, width, height));
    if (!staticLayout) {
      for (let i = 0; i < PRELAYOUT_TICKS; i++)
        tick(fresh, edges, width, height, worldPadding);
    }
    setSims(fresh);
    setTickN((n) => n + 1);
    // We deliberately depend ONLY on shapeKey + staticLayout. shapeKey
    // already encodes node IDs, edge endpoints, and (in static mode)
    // initial positions. Adding the array refs would re-seed on every
    // parent render (React Query returns fresh array refs each
    // refetch) which would throw away the operator's drag positions.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [shapeKey, staticLayout]);

  const dragRef = useRef<{ id: string; pointerId: number } | null>(null);
  const panRef = useRef<{ pointerId: number; sx: number; sy: number; tx0: number; ty0: number } | null>(null);
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
          tick(sims, edges, width, height, worldPadding);
        }
        remaining -= LIVE_TICKS_PER_FRAME;
        setTickN((n) => n + 1);
        rafRef.current = requestAnimationFrame(step);
      };
      stopRAF();
      rafRef.current = requestAnimationFrame(step);
    },
    [sims, edges, width, height, worldPadding, stopRAF],
  );

  useEffect(() => {
    return () => stopRAF();
  }, [stopRAF]);

  // Wheel zoom about cursor. We attach a non-passive listener so we
  // can preventDefault page scroll; React's synthetic onWheel is
  // passive in modern browsers and would warn.
  useEffect(() => {
    if (!enableZoomPan) return;
    const svg = svgRef.current;
    if (!svg) return;
    const handler = (ev: WheelEvent) => {
      ev.preventDefault();
      const rect = svg.getBoundingClientRect();
      const cx = ev.clientX - rect.left;
      const cy = ev.clientY - rect.top;
      setView((v) => {
        const factor = Math.pow(1.0018, -ev.deltaY);
        const k1 = Math.max(MIN_ZOOM, Math.min(MAX_ZOOM, v.k * factor));
        // Maintain cursor as zoom anchor: world point under cursor must stay put.
        const wx = (cx - v.tx) / v.k;
        const wy = (cy - v.ty) / v.k;
        return { tx: cx - wx * k1, ty: cy - wy * k1, k: k1 };
      });
    };
    svg.addEventListener("wheel", handler, { passive: false });
    return () => svg.removeEventListener("wheel", handler);
  }, [enableZoomPan]);

  // Convert a screen-space pointer event into world (untransformed)
  // SVG coords. Uses the inner group's CTM so it accounts for the
  // pan/zoom transform automatically.
  const screenToWorld = useCallback((e: React.PointerEvent | PointerEvent) => {
    const g = groupRef.current;
    const svg = svgRef.current;
    if (!g || !svg) return { x: 0, y: 0 };
    const ctm = g.getScreenCTM();
    if (!ctm) return { x: 0, y: 0 };
    const pt = svg.createSVGPoint();
    pt.x = e.clientX;
    pt.y = e.clientY;
    const local = pt.matrixTransform(ctm.inverse());
    return { x: local.x, y: local.y };
  }, []);

  const handleNodePointerDown = (e: React.PointerEvent, id: string) => {
    const sim = sims.find((s) => s.id === id);
    if (!sim) return;
    if (sim.data.pinned) return;
    e.stopPropagation();
    e.currentTarget.setPointerCapture(e.pointerId);
    dragRef.current = { id, pointerId: e.pointerId };
    movedRef.current = false;
    // Do NOT pin yet. We pin only once the pointer actually moves
    // past the click threshold (handleNodePointerMove). Clicking a
    // node without dragging should never permanently anchor it.

    if (staticLayout) {
      // No simulation in static mode — drag just translates the one
      // node we grabbed. Nothing to RAF.
      return;
    }

    const loop = () => {
      if (!dragRef.current) {
        rafRef.current = null;
        return;
      }
      for (let i = 0; i < LIVE_TICKS_PER_FRAME; i++) {
        tick(sims, edges, width, height, worldPadding);
      }
      setTickN((n) => n + 1);
      rafRef.current = requestAnimationFrame(loop);
    };
    stopRAF();
    rafRef.current = requestAnimationFrame(loop);
  };

  const handleNodePointerMove = (e: React.PointerEvent) => {
    const drag = dragRef.current;
    if (!drag || drag.pointerId !== e.pointerId) return;
    const sim = sims.find((s) => s.id === drag.id);
    if (!sim) return;
    const w = screenToWorld(e);
    const x = Math.max(worldPadding, Math.min(width - worldPadding, w.x));
    const y = Math.max(worldPadding, Math.min(height - worldPadding, w.y));
    if (staticLayout) {
      // Move the node directly. No fx/fy gymnastics — there is no
      // simulation, so x/y is the source of truth.
      sim.x = x;
      sim.y = y;
      setTickN((n) => n + 1);
    } else {
      sim.fx = x;
      sim.fy = y;
    }
    movedRef.current = true;
  };

  const handleNodePointerUp = (_e: React.PointerEvent, id: string) => {
    const drag = dragRef.current;
    dragRef.current = null;
    const sim = sims.find((s) => s.id === id);
    if (sim && !staticLayout) {
      if (!movedRef.current) {
        // Pure click in force mode: clear any anchor we might have
        // set so the node is free to participate in the layout
        // again. (In static mode we never anchored.)
        sim.fx = undefined;
        sim.fy = undefined;
      }
      // If the operator actually dragged, fx/fy stay set and the
      // node sticks where they dropped it.
    }
    if (!movedRef.current && drag && onNodeClick && sim) {
      onNodeClick(sim.data);
      // No relax pass on a pure click — the layout was already
      // settled, and shaking it post-click is what made the
      // canvas appear to spin.
      return;
    }
    if (!staticLayout) startLiveRelax(60);
  };

  // Background pan handlers (only meaningful when enableZoomPan).
  const handleBgPointerDown = (e: React.PointerEvent) => {
    if (!enableZoomPan) {
      // Even without pan, an empty-space click should clear selection.
      onBackgroundClick?.();
      return;
    }
    if (dragRef.current) return;
    e.currentTarget.setPointerCapture(e.pointerId);
    panRef.current = {
      pointerId: e.pointerId,
      sx: e.clientX,
      sy: e.clientY,
      tx0: view.tx,
      ty0: view.ty,
    };
    movedRef.current = false;
  };

  const handleBgPointerMove = (e: React.PointerEvent) => {
    const p = panRef.current;
    if (!p || p.pointerId !== e.pointerId) return;
    const dx = e.clientX - p.sx;
    const dy = e.clientY - p.sy;
    if (Math.abs(dx) + Math.abs(dy) > 2) movedRef.current = true;
    setView((v) => ({ ...v, tx: p.tx0 + dx, ty: p.ty0 + dy }));
  };

  const handleBgPointerUp = (e: React.PointerEvent) => {
    const p = panRef.current;
    if (p && p.pointerId === e.pointerId) {
      panRef.current = null;
      if (!movedRef.current) onBackgroundClick?.();
    }
  };

  useImperativeHandle(
    ref,
    () => ({
      resetView() {
        setView({ tx: 0, ty: 0, k: 1 });
      },
      zoomBy(k) {
        setView((v) => {
          const k1 = Math.max(MIN_ZOOM, Math.min(MAX_ZOOM, v.k * k));
          // Zoom about the canvas center so +/- buttons feel stable.
          const cx = width / 2;
          const cy = height / 2;
          const wx = (cx - v.tx) / v.k;
          const wy = (cy - v.ty) / v.k;
          return { tx: cx - wx * k1, ty: cy - wy * k1, k: k1 };
        });
      },
      centerOn(id) {
        const s = sims.find((x) => x.id === id);
        if (!s) return;
        setView((v) => ({
          k: v.k,
          tx: width / 2 - s.x * v.k,
          ty: height / 2 - s.y * v.k,
        }));
      },
      fit(margin = 60) {
        if (sims.length === 0) return;
        const xs = sims.map((s) => s.x);
        const ys = sims.map((s) => s.y);
        const minX = Math.min(...xs);
        const maxX = Math.max(...xs);
        const minY = Math.min(...ys);
        const maxY = Math.max(...ys);
        const bw = Math.max(1, maxX - minX);
        const bh = Math.max(1, maxY - minY);
        const k = Math.min(
          (width - margin * 2) / bw,
          (height - margin * 2) / bh,
          MAX_ZOOM,
        );
        const cx = (minX + maxX) / 2;
        const cy = (minY + maxY) / 2;
        setView({
          k,
          tx: width / 2 - cx * k,
          ty: height / 2 - cy * k,
        });
      },
    }),
    [sims, width, height],
  );

  return (
    <svg
      ref={svgRef}
      width={width}
      height={height}
      className="block touch-none"
      style={{ cursor: enableZoomPan ? (panRef.current ? "grabbing" : "grab") : "default" }}
    >
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

      {/* Background hit area: catches empty-space clicks for both
          panning and clearing selection. The transparent rect lives
          in screen-space so it's always full-bleed. */}
      <rect
        x={0}
        y={0}
        width={width}
        height={height}
        fill="transparent"
        onPointerDown={handleBgPointerDown}
        onPointerMove={handleBgPointerMove}
        onPointerUp={handleBgPointerUp}
        onPointerCancel={handleBgPointerUp}
      />

      {/* tickN is read here so React re-renders when the simulation
          ticks; we deliberately do NOT re-key the wrapper, which
          would unmount the children mid-drag and drop the SVG's
          pointer capture. */}
      <g
        ref={groupRef}
        data-tick={tickN}
        transform={
          enableZoomPan
            ? `translate(${view.tx}, ${view.ty}) scale(${view.k})`
            : undefined
        }
      >
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
            onPointerDown={(e) => handleNodePointerDown(e, s.id)}
            onPointerMove={handleNodePointerMove}
            onPointerUp={(e) => handleNodePointerUp(e, s.id)}
            onPointerCancel={(e) => handleNodePointerUp(e, s.id)}
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

// Type-preserving forwardRef wrapper so callers can pass a ref while
// keeping the generic node/edge types intact.
const ForceGraph = forwardRef(ForceGraphInner) as <
  N extends ForceNodeInput,
  E extends ForceEdgeInput,
>(
  props: Props<N, E> & { ref?: React.Ref<ForceGraphHandle> },
) => ReturnType<typeof ForceGraphInner>;

export default ForceGraph;

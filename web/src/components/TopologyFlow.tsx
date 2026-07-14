// TopologyFlow — React Flow (@xyflow/react) + ELK layered layout.
// L2 (+ WAN for Internet hierarchy) drive ELK; VPN edges are visual-only.

import { useCallback, useEffect, useMemo, useState } from "react";
import { Link, useNavigate } from "react-router-dom";
import {
  Background,
  ConnectionMode,
  Controls,
  Handle,
  MarkerType,
  MiniMap,
  Position,
  ReactFlow,
  ReactFlowProvider,
  useEdgesState,
  useNodesState,
  useReactFlow,
  type Edge,
  type Node,
  type NodeProps,
} from "@xyflow/react";
import "@xyflow/react/dist/style.css";
import ELK from "elkjs/lib/elk.bundled.js";
import type { Topology, TopologyEdge, TopologyNode } from "../api/types";

/** Invisible anchors — custom nodes need Handles or React Flow draws no edges. */
function NodeHandles() {
  const hidden: React.CSSProperties = {
    width: 8,
    height: 8,
    opacity: 0,
    border: "none",
    background: "transparent",
  };
  return (
    <>
      <Handle type="target" position={Position.Top} id="t" style={hidden} isConnectable={false} />
      <Handle type="source" position={Position.Top} id="ts" style={hidden} isConnectable={false} />
      <Handle type="target" position={Position.Bottom} id="b" style={hidden} isConnectable={false} />
      <Handle type="source" position={Position.Bottom} id="bs" style={hidden} isConnectable={false} />
      <Handle type="target" position={Position.Left} id="l" style={hidden} isConnectable={false} />
      <Handle type="source" position={Position.Left} id="ls" style={hidden} isConnectable={false} />
      <Handle type="target" position={Position.Right} id="r" style={hidden} isConnectable={false} />
      <Handle type="source" position={Position.Right} id="rs" style={hidden} isConnectable={false} />
    </>
  );
}

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

const elk = new ELK();

type TopoNodeData = {
  ref: TopologyNode;
  compact: boolean;
};

type TopoEdgeData = {
  ref: TopologyEdge;
  kind: "l2" | "wan" | "vpn";
};

function linkMedium(e: TopologyEdge): string {
  const m = (e.linkKind as { medium?: unknown } | undefined)?.medium;
  return typeof m === "string" ? m.toLowerCase() : "";
}

function linkLayer(e: TopologyEdge): number {
  return Number((e.linkKind as { layer?: unknown } | undefined)?.layer) || 0;
}

export function isVPNEdge(e: TopologyEdge): boolean {
  return linkMedium(e) === "vpn" || e.protocol.includes("vpn");
}

export function isWANEdge(e: TopologyEdge): boolean {
  return linkMedium(e) === "wan" || e.protocol === "uplink";
}

export function isL2Edge(e: TopologyEdge): boolean {
  return (
    linkLayer(e) === 2 ||
    e.protocol === "lldp" ||
    e.protocol === "cdp" ||
    e.protocol === "both"
  );
}

function isFirewall(n: TopologyNode): boolean {
  const tags = new Set((n.tags ?? []).map((t) => t.toLowerCase()));
  return (
    tags.has("firewall") ||
    /mx|firewall|asa|palo|forti/i.test(`${n.model ?? ""} ${n.label}`)
  );
}

function isAP(n: TopologyNode): boolean {
  const tags = new Set((n.tags ?? []).map((t) => t.toLowerCase()));
  return tags.has("wap") || /mr|access.?point/i.test(`${n.model ?? ""} ${n.label}`);
}

/** ELK partition: lower numbers sit above (direction DOWN). */
function partitionOf(n: TopologyNode): number {
  if (n.kind === "cloud") return 0;
  if (n.kind === "foreign") return 4;
  if (isFirewall(n)) return 1;
  if (isAP(n)) return 3;
  return 2;
}

function nodeSize(n: TopologyNode, compact: boolean): { w: number; h: number } {
  if (n.kind === "cloud") return { w: 120, h: 64 };
  if (n.kind === "foreign") return compact ? { w: 100, h: 36 } : { w: 140, h: 48 };
  return compact ? { w: 128, h: 52 } : { w: 168, h: 68 };
}

function edgeVisual(e: TopologyEdge): {
  kind: "l2" | "wan" | "vpn";
  stroke: string;
  width: number;
  dashed: boolean;
} {
  if (isVPNEdge(e)) {
    return {
      kind: "vpn",
      stroke: e.operUp ? "#c084fc" : "#6b21a8",
      width: 1.6,
      dashed: true,
    };
  }
  if (isWANEdge(e)) {
    return {
      kind: "wan",
      stroke: e.operUp ? "#fbbf24" : "#92400e",
      width: 2,
      dashed: !e.operUp,
    };
  }
  const util = e.utilizationPct;
  let stroke = e.operUp ? "#64748b" : "#334155";
  if (util != null) {
    if (util >= 80) stroke = "#ef4444";
    else if (util >= 50) stroke = "#f59e0b";
    else stroke = "#22c55e";
  }
  return {
    kind: "l2",
    stroke,
    width: linkLayer(e) === 2 ? 2.2 : 1.4,
    dashed: !e.operUp,
  };
}

function layoutEdge(e: TopologyEdge): boolean {
  // Drive hierarchy with L2 fabric + WAN to Internet. VPN is visual-only.
  return isL2Edge(e) || isWANEdge(e);
}

async function layoutWithElk(
  nodes: TopologyNode[],
  edges: TopologyEdge[],
  compact: boolean,
): Promise<Map<string, { x: number; y: number }>> {
  const graph = {
    id: "root",
    layoutOptions: {
      "elk.algorithm": "layered",
      "elk.direction": "DOWN",
      "elk.edgeRouting": "ORTHOGONAL",
      "elk.spacing.nodeNode": compact ? "36" : "48",
      "elk.layered.spacing.nodeNodeBetweenLayers": "72",
      "elk.spacing.edgeNode": "24",
      "elk.spacing.edgeEdge": "16",
      "elk.partitioning.activate": "true",
      "elk.layered.nodePlacement.strategy": "NETWORK_SIMPLEX",
      "elk.layered.crossingMinimization.strategy": "LAYER_SWEEP",
    },
    children: nodes.map((n) => {
      const { w, h } = nodeSize(n, compact);
      return {
        id: n.id,
        width: w,
        height: h,
        layoutOptions: {
          "elk.partitioning.partition": String(partitionOf(n)),
        },
      };
    }),
    edges: edges.filter(layoutEdge).map((e, i) => ({
      id: `layout-${i}-${e.from}-${e.to}`,
      sources: [e.from],
      targets: [e.to],
    })),
  };

  const laid = await elk.layout(graph);
  const pos = new Map<string, { x: number; y: number }>();
  for (const c of laid.children ?? []) {
    pos.set(c.id, { x: c.x ?? 0, y: c.y ?? 0 });
  }
  return pos;
}

function CloudNode({ data }: NodeProps<Node<TopoNodeData>>) {
  const n = data.ref;
  return (
    <div className="relative flex w-[120px] flex-col items-center gap-1">
      <NodeHandles />
      <svg width="56" height="36" viewBox="0 0 56 36" aria-hidden>
        <path
          d="M22 36h34c8 0 14-6 14-13s-6-13-14-13c-1.5-7-8-12-15.5-12-7 0-13 4-15.5 10C20 8 14 13 14 20c0 1 .1 2 .3 3C8.5 24 4 29 4 35c0 6 5 11 12 11h6"
          fill="#0f172a"
          stroke="#38bdf8"
          strokeWidth="2"
          transform="translate(-4,-8) scale(0.95)"
        />
      </svg>
      <span className="text-[11px] font-medium text-sky-200">{n.label}</span>
    </div>
  );
}

function ApplianceNode({ data }: NodeProps<Node<TopoNodeData>>) {
  const n = data.ref;
  const fill = STATUS_FILL[n.status];
  const ring = STATUS_RING[n.status];
  const title = [n.label, ...(n.tags ?? []).slice(0, 6)].join(" · ");
  return (
    <div
      title={title}
      className="relative rounded-lg border px-2.5 py-1.5 shadow-sm"
      style={{
        background: "rgb(var(--ink-950) / 0.92)",
        borderColor: ring,
        minWidth: data.compact ? 112 : 148,
      }}
    >
      <NodeHandles />
      <div className="flex items-center gap-2">
        <span
          className="inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-full text-[10px] font-semibold text-white"
          style={{ background: fill, boxShadow: `0 0 0 1.5px ${ring}` }}
        >
          {n.portsUp ?? "?"}/{n.portsTotal ?? "?"}
        </span>
        <div className="min-w-0">
          <div className="truncate text-[11px] font-medium text-slate-100">{n.label}</div>
          {!data.compact && n.mgmtIp && (
            <div className="truncate font-mono text-[9px] text-slate-500">{n.mgmtIp}</div>
          )}
          {!data.compact && n.model && (
            <div className="truncate text-[9px] text-slate-500">{n.model}</div>
          )}
        </div>
      </div>
    </div>
  );
}

function ForeignNode({ data }: NodeProps<Node<TopoNodeData>>) {
  const n = data.ref;
  return (
    <div
      title={n.label}
      className="relative rounded-md border border-slate-700 bg-slate-900/80 px-2 py-1 opacity-80"
      style={{ minWidth: data.compact ? 88 : 120 }}
    >
      <NodeHandles />
      <div className="truncate text-[10px] text-slate-300">{n.label}</div>
      {!data.compact && n.mgmtIp && (
        <div className="truncate font-mono text-[9px] text-slate-500">{n.mgmtIp}</div>
      )}
    </div>
  );
}

const nodeTypes = {
  cloud: CloudNode,
  appliance: ApplianceNode,
  foreign: ForeignNode,
};

function toFlowNodes(
  nodes: TopologyNode[],
  positions: Map<string, { x: number; y: number }>,
  compact: boolean,
): Node<TopoNodeData>[] {
  return nodes.map((n) => {
    const p = positions.get(n.id) ?? { x: 0, y: 0 };
    return {
      id: n.id,
      type: n.kind,
      position: p,
      data: { ref: n, compact },
      draggable: true,
    };
  });
}

function toFlowEdges(
  edges: TopologyEdge[],
  nodeIds: Set<string>,
): Edge<TopoEdgeData>[] {
  return edges
    .filter((e) => nodeIds.has(e.from) && nodeIds.has(e.to) && e.from !== e.to)
    .map((e, i) => {
      const v = edgeVisual(e);
      const label =
        v.kind === "wan"
          ? e.fromPort || "WAN"
          : v.kind === "vpn"
            ? e.fromPort || (e.protocol === "meraki-autovpn" ? "Auto VPN" : "VPN")
            : e.utilizationPct != null
              ? `${e.utilizationPct.toFixed(0)}%`
              : undefined;
      return {
        id: `e-${i}-${e.from}-${e.to}-${e.protocol}-${e.fromPort ?? ""}`,
        source: e.from,
        target: e.to,
        // Prefer bottom→top for DOWN layout; Loose mode still routes if reversed.
        sourceHandle: "bs",
        targetHandle: "t",
        type: "smoothstep",
        animated: v.kind === "vpn" && e.operUp,
        label: label && (v.kind !== "l2" || e.utilizationPct != null) ? label : undefined,
        labelStyle: { fill: "#cbd5e1", fontSize: 9 },
        labelBgStyle: { fill: "#0f172a", fillOpacity: 0.75 },
        labelBgPadding: [3, 2] as [number, number],
        style: {
          stroke: v.stroke,
          strokeWidth: Math.max(v.width, 2),
          strokeDasharray: v.dashed ? "6 4" : undefined,
          opacity: v.kind === "vpn" ? 0.8 : 1,
        },
        markerEnd: {
          type: MarkerType.ArrowClosed,
          width: 14,
          height: 14,
          color: v.stroke,
        },
        data: { ref: e, kind: v.kind },
        zIndex: v.kind === "l2" ? 2 : 1,
      };
    });
}

function TopologyFlowInner({ data }: { data: Topology }) {
  const navigate = useNavigate();
  const { fitView, zoomIn, zoomOut, setViewport } = useReactFlow();
  const compact = data.nodes.length > 40;
  const [nodes, setNodes, onNodesChange] = useNodesState<Node<TopoNodeData>>([]);
  const [edges, setEdges, onEdgesChange] = useEdgesState<Edge<TopoEdgeData>>([]);
  const [layouting, setLayouting] = useState(true);
  const [layoutError, setLayoutError] = useState<string | null>(null);

  const shapeKey = useMemo(
    () =>
      `${data.nodes.map((n) => n.id).join("|")}::${data.edges
        .map((e) => `${e.from}-${e.to}-${e.protocol}`)
        .join("|")}`,
    [data],
  );

  useEffect(() => {
    let cancelled = false;
    setLayouting(true);
    setLayoutError(null);
    (async () => {
      try {
        const positions = await layoutWithElk(data.nodes, data.edges, compact);
        if (cancelled) return;
        const idSet = new Set(data.nodes.map((n) => n.id));
        setNodes(toFlowNodes(data.nodes, positions, compact));
        setEdges(toFlowEdges(data.edges, idSet));
        setLayouting(false);
        requestAnimationFrame(() => {
          fitView({ padding: 0.12, duration: 200 });
        });
      } catch (err) {
        if (cancelled) return;
        console.error("ELK layout failed", err);
        setLayoutError("Layout failed — showing unordered grid.");
        // Fallback grid so the page never blanks.
        const fallback = new Map<string, { x: number; y: number }>();
        data.nodes.forEach((n, i) => {
          const col = i % 8;
          const row = Math.floor(i / 8);
          fallback.set(n.id, { x: col * 180, y: row * 100 });
        });
        const idSet = new Set(data.nodes.map((n) => n.id));
        setNodes(toFlowNodes(data.nodes, fallback, compact));
        setEdges(toFlowEdges(data.edges, idSet));
        setLayouting(false);
        requestAnimationFrame(() => fitView({ padding: 0.12 }));
      }
    })();
    return () => {
      cancelled = true;
    };
  }, [shapeKey, compact, data.nodes, data.edges, setNodes, setEdges, fitView]);

  const onNodeClick = useCallback(
    (_: React.MouseEvent, node: Node<TopoNodeData>) => {
      if (node.data.ref.kind === "appliance") {
        navigate(`/appliances/${node.data.ref.id}`);
      }
    },
    [navigate],
  );

  if (data.nodes.length === 0) {
    return (
      <div className="rounded-xl border border-ink-800 bg-ink-900 p-10 text-center text-sm text-slate-400">
        No appliances match the current filter. Add one under{" "}
        <Link to="/appliances" className="text-sonar-400 hover:underline">
          Appliances
        </Link>{" "}
        or clear the tag filter / enable more link layers above.
      </div>
    );
  }

  const noEdges = data.edges.length === 0;
  const onlyManaged = data.nodes.every((n) => n.kind === "appliance");

  return (
    <div className="relative h-[72vh] overflow-hidden rounded-xl border border-ink-800 bg-ink-900">
      {noEdges && (
        <div className="absolute left-3 top-3 z-10 max-w-md rounded-md border border-amber-900/60 bg-amber-950/40 px-3 py-2 text-xs text-amber-200">
          {onlyManaged
            ? "No L2 / WAN / VPN links in the current view."
            : "No discovery links yet."}
        </div>
      )}
      {layoutError && (
        <div className="absolute left-3 top-12 z-10 rounded-md border border-amber-900/60 bg-amber-950/40 px-3 py-2 text-xs text-amber-200">
          {layoutError}
        </div>
      )}
      {layouting && (
        <div className="absolute inset-0 z-20 flex items-center justify-center bg-ink-900/60 text-sm text-slate-300 backdrop-blur-[1px]">
          Laying out topology…
        </div>
      )}

      <div className="absolute right-3 top-3 z-10 flex gap-1">
        <button
          type="button"
          className="rounded border border-ink-700 bg-ink-950/90 px-2 py-1 text-xs text-slate-300 hover:bg-ink-800"
          onClick={() => zoomIn({ duration: 150 })}
        >
          +
        </button>
        <button
          type="button"
          className="rounded border border-ink-700 bg-ink-950/90 px-2 py-1 text-xs text-slate-300 hover:bg-ink-800"
          onClick={() => zoomOut({ duration: 150 })}
        >
          −
        </button>
        <button
          type="button"
          className="rounded border border-ink-700 bg-ink-950/90 px-2 py-1 text-xs text-slate-300 hover:bg-ink-800"
          onClick={() => fitView({ padding: 0.12, duration: 200 })}
        >
          Fit
        </button>
        <button
          type="button"
          className="rounded border border-ink-700 bg-ink-950/90 px-2 py-1 text-xs text-slate-300 hover:bg-ink-800"
          onClick={() => setViewport({ x: 0, y: 0, zoom: 1 }, { duration: 200 })}
        >
          Reset
        </button>
      </div>

      <ReactFlow
        nodes={nodes}
        edges={edges}
        onNodesChange={onNodesChange}
        onEdgesChange={onEdgesChange}
        onNodeClick={onNodeClick}
        nodeTypes={nodeTypes}
        fitView
        minZoom={0.15}
        maxZoom={2.5}
        proOptions={{ hideAttribution: true }}
        nodesConnectable={false}
        edgesReconnectable={false}
        elementsSelectable
        connectionMode={ConnectionMode.Loose}
        defaultEdgeOptions={{
          interactionWidth: 16,
          type: "smoothstep",
          style: { strokeWidth: 2 },
        }}
        className="topology-flow"
      >
        <Background gap={20} size={1} color="#1e293b" />
        <Controls showInteractive={false} className="!bg-ink-950 !border-ink-700 !shadow-none" />
        <MiniMap
          pannable
          zoomable
          nodeStrokeWidth={2}
          maskColor="rgb(15 23 42 / 0.7)"
          className="!bg-ink-950 !border-ink-700"
          nodeColor={(n) => {
            const ref = (n.data as TopoNodeData | undefined)?.ref;
            if (!ref) return "#64748b";
            if (ref.kind === "cloud") return "#38bdf8";
            if (ref.kind === "foreign") return "#334155";
            return STATUS_FILL[ref.status];
          }}
        />
      </ReactFlow>

      <div className="pointer-events-none absolute bottom-3 left-3 z-10 rounded-md border border-ink-800 bg-ink-950/85 px-3 py-2 text-[10px] uppercase tracking-wider text-slate-400 backdrop-blur">
        <div className="flex flex-wrap items-center gap-3">
          <span className="inline-flex items-center gap-1">
            <span className="inline-block h-2.5 w-2.5 rounded-full" style={{ background: STATUS_FILL.up }} />{" "}
            up
          </span>
          <span className="inline-flex items-center gap-1">
            <span
              className="inline-block h-2.5 w-2.5 rounded-full"
              style={{ background: STATUS_FILL.degraded }}
            />{" "}
            degraded
          </span>
          <span className="inline-flex items-center gap-1">
            <span className="inline-block h-2.5 w-2.5 rounded-full" style={{ background: STATUS_FILL.down }} />{" "}
            down
          </span>
          <span className="inline-flex items-center gap-1">
            <span
              className="inline-block h-2.5 w-2.5 rounded-full border border-slate-500"
              style={{ background: "#1e293b" }}
            />{" "}
            foreign
          </span>
          <span className="normal-case tracking-normal text-sky-300">Internet</span>
        </div>
        <div className="mt-1.5 border-t border-ink-800 pt-1.5 normal-case tracking-normal text-slate-500">
          ELK layered layout · drag nodes · scroll to zoom
        </div>
      </div>
    </div>
  );
}

export default function TopologyFlow({ data }: { data: Topology }) {
  return (
    <ReactFlowProvider>
      <TopologyFlowInner data={data} />
    </ReactFlowProvider>
  );
}

// AgentNetworkFlow — React Flow renderer for the agent radial network graph.
// Layout positions are computed by the parent; this only draws.

import { useCallback, useEffect, useMemo } from "react";
import {
  Background,
  ConnectionMode,
  Handle,
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
import type { AgentNetworkGraph, AgentNetworkPeer } from "../api/types";

export type NodeKind = "host" | "process" | "endpoint" | "isp";

export interface ProcessAgg {
  key: string;
  name: string;
  pid?: number;
  peers: Set<string>;
  totalConns: number;
}

export interface IspAgg {
  key: string;
  org: string;
  asn?: number;
  peers: Set<string>;
  countries: Set<string>;
}

export interface NetNodeData {
  id: string;
  kind: NodeKind;
  label: string;
  sub?: string;
  host?: AgentNetworkGraph["agent"];
  process?: ProcessAgg;
  endpoint?: AgentNetworkPeer;
  isp?: IspAgg;
  initialX: number;
  initialY: number;
  pinned?: boolean;
}

export interface NetEdgeInput {
  from: string;
  to: string;
  tier: "h-p" | "p-i" | "p-e" | "e-i";
  weight?: number;
}

export interface DisplayOptions {
  showIsp: boolean;
  showEndpoints: boolean;
  uniqueProcesses: boolean;
  showProcessLabels: boolean;
  showEndpointLabels: boolean;
  showIspLabels: boolean;
  showHostLabel: boolean;
}

type RFNodeData = {
  ref: NetNodeData;
  selected: boolean;
  options: DisplayOptions;
};

function NodeHandles() {
  const hidden: React.CSSProperties = {
    width: 6,
    height: 6,
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

function truncate(s: string, n: number): string {
  return s.length <= n ? s : s.slice(0, n - 1) + "…";
}

function HostRF({ data }: NodeProps<Node<RFNodeData>>) {
  const n = data.ref;
  const selected = data.selected;
  return (
    <div className="relative flex flex-col items-center">
      <NodeHandles />
      <div
        className="flex h-14 w-14 items-center justify-center rounded-full text-white shadow-lg"
        style={{
          background: "#0ea5e9",
          boxShadow: selected
            ? "0 0 0 4px #7dd3fc55, 0 0 0 2px #7dd3fc"
            : "0 0 0 6px #0ea5e933",
        }}
      >
        <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2">
          <rect x="3" y="6" width="18" height="12" rx="2" />
          <line x1="3" y1="14" x2="21" y2="14" />
          <line x1="8" y1="18" x2="16" y2="18" />
          <line x1="12" y1="18" x2="12" y2="22" />
        </svg>
      </div>
      {data.options.showHostLabel && (
        <div className="mt-1 text-center">
          <div className="text-[12px] font-semibold text-slate-100">{truncate(n.label, 28)}</div>
          {n.sub && <div className="font-mono text-[10px] text-slate-500">{n.sub}</div>}
        </div>
      )}
    </div>
  );
}

function ProcessRF({ data }: NodeProps<Node<RFNodeData>>) {
  const n = data.ref;
  const proc = n.process!;
  const text = proc.name + (proc.pid != null ? ` · ${proc.pid}` : "");
  return (
    <div className="relative">
      <NodeHandles />
      <div
        className="inline-flex items-center gap-1 rounded-full bg-slate-100 px-2.5 py-1 text-[11px] font-medium text-slate-800"
        style={{
          boxShadow: data.selected ? "0 0 0 2px #0ea5e9" : "0 0 0 1px #cbd5e1",
        }}
      >
        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="#2563eb" strokeWidth="2.2">
          <circle cx="12" cy="12" r="3" />
          <path d="M12 1v2M12 21v2M4.2 4.2l1.4 1.4M18.4 18.4l1.4 1.4M1 12h2M21 12h2M4.2 19.8l1.4-1.4M18.4 5.6l1.4-1.4" />
        </svg>
        {data.options.showProcessLabels ? truncate(text, 32) : null}
      </div>
    </div>
  );
}

function EndpointRF({ data }: NodeProps<Node<RFNodeData>>) {
  const n = data.ref;
  return (
    <div className="relative flex flex-col items-center">
      <NodeHandles />
      <div
        className="flex h-7 w-7 items-center justify-center rounded-full bg-sky-500/20 text-sky-300"
        style={{ boxShadow: data.selected ? "0 0 0 2px #38bdf8" : "0 0 0 1px #38bdf8" }}
      >
        <svg width="12" height="12" viewBox="0 0 24 24" fill="currentColor">
          <path d="M12 2C8.1 2 5 5.1 5 9c0 5.2 7 13 7 13s7-7.8 7-13c0-3.9-3.1-7-7-7zm0 9.5A2.5 2.5 0 1 1 12 6a2.5 2.5 0 0 1 0 5.5z" />
        </svg>
      </div>
      {data.options.showEndpointLabels && (
        <div className="mt-0.5 font-mono text-[9px] text-slate-400">{truncate(n.label, 18)}</div>
      )}
    </div>
  );
}

function IspRF({ data }: NodeProps<Node<RFNodeData>>) {
  const n = data.ref;
  return (
    <div className="relative">
      <NodeHandles />
      <div
        className="inline-flex items-center gap-1 rounded-full bg-slate-100 px-2.5 py-1 text-[11px] font-medium text-slate-800"
        style={{
          boxShadow: data.selected ? "0 0 0 2px #94a3b8" : "0 0 0 1px #cbd5e1",
        }}
      >
        <svg width="12" height="12" viewBox="0 0 24 24" fill="none" stroke="#334155" strokeWidth="1.8">
          <circle cx="12" cy="12" r="9" />
          <path d="M3 12h18M12 3a14 14 0 0 1 0 18M12 3a14 14 0 0 0 0 18" />
        </svg>
        {data.options.showIspLabels ? truncate(n.label, 28) : null}
      </div>
    </div>
  );
}

const nodeTypes = {
  host: HostRF,
  process: ProcessRF,
  endpoint: EndpointRF,
  isp: IspRF,
};

function edgeStyle(
  tier: NetEdgeInput["tier"],
  selected: boolean,
): React.CSSProperties {
  switch (tier) {
    case "h-p":
      return {
        stroke: selected ? "#7dd3fc" : "#64748b",
        strokeWidth: selected ? 2.2 : 1.6,
        opacity: selected ? 1 : 0.55,
      };
    case "p-i":
    case "p-e":
      return {
        stroke: selected ? "#38bdf8" : "#0ea5e9",
        strokeWidth: selected ? 2.2 : 1.4,
        opacity: selected ? 1 : 0.55,
      };
    case "e-i":
      return {
        stroke: selected ? "#94a3b8" : "#475569",
        strokeWidth: selected ? 1.6 : 1.1,
        strokeDasharray: "3 4",
        opacity: selected ? 1 : 0.55,
      };
  }
}

export interface AgentNetworkFlowHandle {
  zoomIn(): void;
  zoomOut(): void;
  fit(): void;
  reset(): void;
  centerOn(id: string): void;
}

function AgentNetworkFlowInner({
  nodes: inputNodes,
  edges: inputEdges,
  selectedId,
  options,
  onSelect,
  onReady,
}: {
  nodes: NetNodeData[];
  edges: NetEdgeInput[];
  selectedId: string | null;
  options: DisplayOptions;
  onSelect: (id: string | null) => void;
  onReady: (api: AgentNetworkFlowHandle) => void;
}) {
  const { fitView, zoomIn, zoomOut, setViewport, setCenter, getNode } = useReactFlow();
  const [nodes, setNodes, onNodesChange] = useNodesState<Node<RFNodeData>>([]);
  const [edges, setEdges, onEdgesChange] = useEdgesState<Edge>([]);

  const shapeKey = useMemo(
    () =>
      `${inputNodes.map((n) => `${n.id}@${n.initialX},${n.initialY}`).join("|")}::${inputEdges
        .map((e) => `${e.from}-${e.to}-${e.tier}`)
        .join("|")}`,
    [inputNodes, inputEdges],
  );

  useEffect(() => {
    const rfNodes: Node<RFNodeData>[] = inputNodes.map((n) => ({
      id: n.id,
      type: n.kind,
      position: { x: n.initialX, y: n.initialY },
      data: { ref: n, selected: selectedId === n.id, options },
      draggable: !n.pinned,
    }));
    const rfEdges: Edge[] = inputEdges.map((e, i) => {
      const near =
        selectedId != null && (e.from === selectedId || e.to === selectedId);
      return {
        id: `ae-${i}-${e.from}-${e.to}-${e.tier}`,
        source: e.from,
        target: e.to,
        sourceHandle: "bs",
        targetHandle: "t",
        type: "straight",
        style: edgeStyle(e.tier, near),
        zIndex: near ? 3 : 1,
      };
    });
    setNodes(rfNodes);
    setEdges(rfEdges);
    requestAnimationFrame(() => fitView({ padding: 0.15, duration: 200, maxZoom: 1.2 }));
  }, [shapeKey]); // eslint-disable-line react-hooks/exhaustive-deps

  // Refresh selection styling without re-layout.
  useEffect(() => {
    setNodes((cur) =>
      cur.map((n) => ({
        ...n,
        data: { ...n.data, selected: selectedId === n.id, options },
      })),
    );
    setEdges((cur) =>
      cur.map((e) => {
        const near =
          selectedId != null && (e.source === selectedId || e.target === selectedId);
        const tier = (inputEdges.find(
          (x) => x.from === e.source && x.to === e.target,
        )?.tier ?? "h-p") as NetEdgeInput["tier"];
        return { ...e, style: edgeStyle(tier, near), zIndex: near ? 3 : 1 };
      }),
    );
  }, [selectedId, options, setNodes, setEdges, inputEdges]);

  useEffect(() => {
    onReady({
      zoomIn: () => zoomIn({ duration: 150 }),
      zoomOut: () => zoomOut({ duration: 150 }),
      fit: () => fitView({ padding: 0.15, duration: 200, maxZoom: 1.2 }),
      reset: () => setViewport({ x: 0, y: 0, zoom: 1 }, { duration: 200 }),
      centerOn: (id: string) => {
        const n = getNode(id);
        if (n) setCenter(n.position.x + 40, n.position.y + 20, { zoom: 1.2, duration: 200 });
      },
    });
  }, [onReady, fitView, zoomIn, zoomOut, setViewport, setCenter, getNode]);

  const onNodeClick = useCallback(
    (_: React.MouseEvent, node: Node) => {
      onSelect(node.id === selectedId ? null : node.id);
    },
    [onSelect, selectedId],
  );

  return (
    <ReactFlow
      nodes={nodes}
      edges={edges}
      onNodesChange={onNodesChange}
      onEdgesChange={onEdgesChange}
      onNodeClick={onNodeClick}
      onPaneClick={() => onSelect(null)}
      nodeTypes={nodeTypes}
      fitView
      minZoom={0.2}
      maxZoom={2.5}
      proOptions={{ hideAttribution: true }}
      nodesConnectable={false}
      edgesReconnectable={false}
      connectionMode={ConnectionMode.Loose}
      className="agent-network-flow"
    >
      <Background gap={20} size={1} color="#1e293b" />
    </ReactFlow>
  );
}

export default function AgentNetworkFlow(props: {
  nodes: NetNodeData[];
  edges: NetEdgeInput[];
  selectedId: string | null;
  options: DisplayOptions;
  onSelect: (id: string | null) => void;
  onReady: (api: AgentNetworkFlowHandle) => void;
}) {
  return (
    <ReactFlowProvider>
      <AgentNetworkFlowInner {...props} />
    </ReactFlowProvider>
  );
}

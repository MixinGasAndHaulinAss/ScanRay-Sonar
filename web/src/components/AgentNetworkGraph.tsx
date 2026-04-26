// AgentNetworkGraph — the "Network Topology" canvas on the agent
// detail page. Renders a deterministic radial graph:
//
//     host (centre)
//        ↓
//     processes (inner ring, evenly spaced)
//        ↓
//     ISPs (outer ring, sorted angularly so each ISP sits near the
//          processes that talk to it)
//
// Endpoints (one node per remote IP) are HIDDEN by default and
// reachable through the "Show endpoints" toggle for deep inspection.
// In the default view we draw direct process→ISP edges, which is
// what most operators actually want to see ("which providers is
// each process talking to?").
//
// Why a fixed radial layout rather than a force-directed one:
//   * The data is hierarchical (host → process → provider). A
//     ring layout makes that hierarchy visible at a glance.
//   * Force layouts on this kind of one-to-many fan-out push leaf
//     nodes into the corners and crisscross every edge.
//   * No simulation means dragging a node moves only that node.
//     Nothing else jiggles, nothing oscillates.
//
// Data flow:
//   useQuery → api.get<AgentNetworkGraph>("/agents/{id}/network-graph")
//   peer list (already aggregated by remote IP, GeoIP-enriched on
//   the server side) → in-memory aggregation + radial placement
//   → ForceGraph in staticLayout mode

import { useQuery } from "@tanstack/react-query";
import { useEffect, useMemo, useRef, useState } from "react";
import { Link } from "react-router-dom";
import { api } from "../api/client";
import type { AgentNetworkGraph, AgentNetworkPeer } from "../api/types";
import ForceGraph, {
  type ForceEdgeInput,
  type ForceGraphHandle,
  type ForceNodeInput,
  type SimNode,
} from "./ForceGraph";

type NodeKind = "host" | "process" | "endpoint" | "isp";

interface ProcessAgg {
  key: string;
  name: string;
  pid?: number;
  peers: Set<string>;
  totalConns: number;
}

interface IspAgg {
  key: string;
  org: string;
  asn?: number;
  peers: Set<string>;
  countries: Set<string>;
}

interface NetNodeData extends ForceNodeInput {
  kind: NodeKind;
  label: string;
  sub?: string;
  /** for "host" */
  host?: AgentNetworkGraph["agent"];
  /** for "process" */
  process?: ProcessAgg;
  /** for "endpoint" */
  endpoint?: AgentNetworkPeer;
  /** for "isp" */
  isp?: IspAgg;
}

interface NetEdgeInput extends ForceEdgeInput {
  tier: "h-p" | "p-i" | "p-e" | "e-i";
  /** Number of connections this edge represents — used to weight the
   *  stroke. Optional; falls back to 1. */
  weight?: number;
}

const DIRECTIONS: Array<"all" | "outbound" | "inbound"> = ["all", "outbound", "inbound"];
const SCOPES: Array<"all" | "public" | "private"> = ["all", "public", "private"];

interface DisplayOptions {
  showIsp: boolean;
  showEndpoints: boolean;
  uniqueProcesses: boolean;
  showProcessLabels: boolean;
  showEndpointLabels: boolean;
  showIspLabels: boolean;
  showHostLabel: boolean;
}

const DEFAULT_OPTIONS: DisplayOptions = {
  showIsp: true,
  // Endpoints (one node per remote IP) are deep-inspection detail.
  // Default off — most operators want host → process → provider.
  showEndpoints: false,
  uniqueProcesses: false,
  showProcessLabels: true,
  showEndpointLabels: false,
  showIspLabels: true,
  showHostLabel: true,
};

const W_DEFAULT = 1200;
const H_DEFAULT = 720;

export default function AgentNetworkGraphSection({ agentId }: { agentId: string }) {
  const { data, isLoading, isError } = useQuery({
    queryKey: ["agent-netgraph", agentId],
    queryFn: () => api.get<AgentNetworkGraph>(`/agents/${agentId}/network-graph`),
    refetchInterval: 30_000,
  });

  const [direction, setDirection] = useState<(typeof DIRECTIONS)[number]>("all");
  const [scope, setScope] = useState<(typeof SCOPES)[number]>("all");
  const [procFilter, setProcFilter] = useState("");
  const [selected, setSelected] = useState<string | null>(null);
  const [options, setOptions] = useState<DisplayOptions>(DEFAULT_OPTIONS);
  const [legendOpen, setLegendOpen] = useState(false);
  const [optionsCollapsed, setOptionsCollapsed] = useState(false);

  const graphRef = useRef<ForceGraphHandle | null>(null);
  const containerRef = useRef<HTMLDivElement | null>(null);
  const [size, setSize] = useState({ w: W_DEFAULT, h: H_DEFAULT });

  // Watch container size so the SVG fills the available area. We
  // attach the observer in an effect — an inline ref callback would
  // be re-invoked on every parent render, leaking observers and
  // (crucially) causing re-renders on click that would shake the
  // ForceGraph if any pixel changed.
  useEffect(() => {
    const el = containerRef.current;
    if (!el) return;
    const apply = () => {
      const cr = el.getBoundingClientRect();
      const w = Math.max(640, Math.round(cr.width));
      const h = Math.max(420, Math.round(cr.height));
      setSize((cur) => (cur.w === w && cur.h === h ? cur : { w, h }));
    };
    apply();
    const ro = new ResizeObserver(apply);
    ro.observe(el);
    return () => ro.disconnect();
  }, []);

  const filteredPeers = useMemo(() => {
    const list = data?.peers ?? [];
    const q = procFilter.trim().toLowerCase();
    return list.filter((p) => {
      if (direction !== "all" && p.direction !== direction) return false;
      if (scope === "public" && p.isPrivate) return false;
      if (scope === "private" && !p.isPrivate) return false;
      if (q && !p.processes.some((pr) => pr.name.toLowerCase().includes(q))) return false;
      return true;
    });
  }, [data, direction, scope, procFilter]);

  const counts = useMemo(() => {
    let inbound = 0;
    let outbound = 0;
    let publicCount = 0;
    let privateCount = 0;
    for (const p of data?.peers ?? []) {
      if (p.direction === "inbound") inbound++;
      else outbound++;
      if (p.isPrivate) privateCount++;
      else publicCount++;
    }
    return { inbound, outbound, publicCount, privateCount, total: data?.peers.length ?? 0 };
  }, [data]);

  const { nodes, edges } = useMemo(() => {
    const ns: NetNodeData[] = [];
    const es: NetEdgeInput[] = [];
    if (!data) return { nodes: ns, edges: es };

    const W = size.w;
    const H = size.h;
    const cx = W / 2;
    const cy = H / 2;

    // ---- 1. Aggregate processes (deterministic order by key) ----
    const procMap = new Map<string, ProcessAgg>();
    for (const peer of filteredPeers) {
      for (const pr of peer.processes) {
        const key =
          options.uniqueProcesses && pr.pid != null
            ? `${pr.name}#${pr.pid}`
            : pr.name || "(unknown)";
        let agg = procMap.get(key);
        if (!agg) {
          agg = {
            key,
            name: pr.name || "(unknown)",
            pid: options.uniqueProcesses ? pr.pid : undefined,
            peers: new Set(),
            totalConns: 0,
          };
          procMap.set(key, agg);
        }
        agg.peers.add(peer.ip);
        agg.totalConns += pr.count;
      }
    }
    const processes = Array.from(procMap.values()).sort((a, b) =>
      a.key.localeCompare(b.key),
    );

    // ---- 2. Aggregate ISPs and a procISP adjacency map ----------
    // procISP[procKey][ispKey] = total conn count
    const ispMap = new Map<string, IspAgg>();
    const procISP = new Map<string, Map<string, number>>();
    const ispKeyFor = (peer: AgentNetworkPeer) => {
      const org =
        peer.org ||
        (peer.isPrivate ? "Private network" : peer.countryName || "Unknown");
      return peer.asn ? `AS${peer.asn}` : org;
    };
    if (options.showIsp) {
      for (const peer of filteredPeers) {
        const ispKey = ispKeyFor(peer);
        const org =
          peer.org ||
          (peer.isPrivate ? "Private network" : peer.countryName || "Unknown");
        let agg = ispMap.get(ispKey);
        if (!agg) {
          agg = {
            key: ispKey,
            org,
            asn: peer.asn,
            peers: new Set(),
            countries: new Set(),
          };
          ispMap.set(ispKey, agg);
        }
        agg.peers.add(peer.ip);
        if (peer.countryIso) agg.countries.add(peer.countryIso);
        for (const pr of peer.processes) {
          const procKey =
            options.uniqueProcesses && pr.pid != null
              ? `${pr.name}#${pr.pid}`
              : pr.name || "(unknown)";
          let m = procISP.get(procKey);
          if (!m) {
            m = new Map();
            procISP.set(procKey, m);
          }
          m.set(ispKey, (m.get(ispKey) ?? 0) + pr.count);
        }
      }
    }
    const isps = Array.from(ispMap.values());

    // ---- 3. Compute radial layout -------------------------------
    // Inner ring (processes): radius scales with count and average
    // pill width so pills don't overlap. Same for the outer ring
    // (ISPs). With small counts we floor at sensible defaults so a
    // 1-process agent doesn't render a microscopic ring.
    const procPillW = (p: ProcessAgg) =>
      Math.max(80, p.name.length * 6.4 + 32);
    const ispPillW = (i: IspAgg) => Math.max(100, i.org.length * 6.6 + 38);
    const avgProcW =
      processes.length > 0
        ? processes.reduce((s, p) => s + procPillW(p), 0) / processes.length
        : 120;
    const avgIspW =
      isps.length > 0
        ? isps.reduce((s, i) => s + ispPillW(i), 0) / isps.length
        : 140;
    // Chord length between adjacent pills on a ring of N evenly
    // spaced points = 2 r sin(π/N). We need that ≥ pillWidth · 1.15.
    const chordR = (n: number, pillW: number) =>
      n > 1 ? (pillW * 1.15) / (2 * Math.sin(Math.PI / n)) : 0;
    const r1 = Math.max(170, chordR(processes.length, avgProcW));
    const r2 = options.showIsp
      ? Math.max(r1 + 200, chordR(isps.length, avgIspW))
      : r1;

    // Process angles: evenly spaced, starting at the top.
    const procAngles = new Map<string, number>();
    processes.forEach((p, i) => {
      const a = -Math.PI / 2 + (i / Math.max(1, processes.length)) * 2 * Math.PI;
      procAngles.set(p.key, a);
    });

    // ISP "ideal" angle: weighted circular mean of the angles of
    // its connected processes. Then we sort ISPs by ideal angle and
    // distribute them evenly on the outer ring in that order — so
    // an ISP is placed close to the angular cluster of processes
    // that talk to it. This dramatically cuts edge crossings vs.
    // even-distribution-by-name.
    const ispAngles = new Map<string, number>();
    if (options.showIsp && isps.length > 0) {
      const ideal = isps.map((isp) => {
        let sx = 0;
        let sy = 0;
        let tw = 0;
        const m = procISP;
        for (const [pKey, ispCounts] of m) {
          const w = ispCounts.get(isp.key);
          if (!w) continue;
          const a = procAngles.get(pKey);
          if (a == null) continue;
          sx += Math.cos(a) * w;
          sy += Math.sin(a) * w;
          tw += w;
        }
        return {
          isp,
          a: tw > 0 ? Math.atan2(sy, sx) : 0,
        };
      });
      ideal.sort((u, v) => u.a - v.a);
      ideal.forEach((entry, i) => {
        const a = -Math.PI / 2 + (i / ideal.length) * 2 * Math.PI;
        ispAngles.set(entry.isp.key, a);
      });
    }

    // ---- 4. Emit nodes with deterministic positions -------------
    ns.push({
      id: "host",
      kind: "host",
      label: data.agent.hostname,
      sub: data.agent.primaryIp ?? data.agent.publicIp ?? undefined,
      host: data.agent,
      pinned: true,
      initialX: cx,
      initialY: cy,
    });

    for (const proc of processes) {
      const a = procAngles.get(proc.key)!;
      ns.push({
        id: `proc:${proc.key}`,
        kind: "process",
        label: proc.name,
        sub: proc.pid != null ? `pid ${proc.pid}` : undefined,
        process: proc,
        initialX: cx + r1 * Math.cos(a),
        initialY: cy + r1 * Math.sin(a),
      });
      es.push({
        from: "host",
        to: `proc:${proc.key}`,
        tier: "h-p",
        weight: proc.totalConns,
      });
    }

    if (options.showIsp) {
      for (const isp of isps) {
        const a = ispAngles.get(isp.key) ?? 0;
        ns.push({
          id: `isp:${isp.key}`,
          kind: "isp",
          label: isp.org,
          sub: isp.asn ? `AS${isp.asn}` : undefined,
          isp,
          initialX: cx + r2 * Math.cos(a),
          initialY: cy + r2 * Math.sin(a),
        });
      }
    }

    if (options.showEndpoints) {
      // Endpoint nodes ride a third ring between the inner and
      // outer rings, placed at the angular average of their
      // (first) connected process and ISP.
      const rMid = options.showIsp ? (r1 + r2) / 2 : r1 + 120;
      for (const peer of filteredPeers) {
        const epId = `ep:${peer.direction}:${peer.ip}`;
        const firstProc = peer.processes[0];
        const procKey = firstProc
          ? options.uniqueProcesses && firstProc.pid != null
            ? `${firstProc.name}#${firstProc.pid}`
            : firstProc.name || "(unknown)"
          : null;
        const pa = procKey ? procAngles.get(procKey) ?? 0 : 0;
        const ia = options.showIsp
          ? ispAngles.get(ispKeyFor(peer)) ?? pa
          : pa;
        // Average of unit vectors → handles wrap-around correctly.
        const mx = Math.cos(pa) + Math.cos(ia);
        const my = Math.sin(pa) + Math.sin(ia);
        const ma =
          mx === 0 && my === 0 ? pa : Math.atan2(my, mx);
        ns.push({
          id: epId,
          kind: "endpoint",
          label: peer.host || peer.ip,
          sub:
            peer.org ||
            (peer.isPrivate ? "private" : peer.countryIso ?? undefined),
          endpoint: peer,
          initialX: cx + rMid * Math.cos(ma),
          initialY: cy + rMid * Math.sin(ma),
        });
        for (const pr of peer.processes) {
          const k =
            options.uniqueProcesses && pr.pid != null
              ? `${pr.name}#${pr.pid}`
              : pr.name || "(unknown)";
          es.push({
            from: `proc:${k}`,
            to: epId,
            tier: "p-e",
            weight: pr.count,
          });
        }
        if (options.showIsp) {
          es.push({
            from: epId,
            to: `isp:${ispKeyFor(peer)}`,
            tier: "e-i",
            weight: peer.totalConns,
          });
        }
      }
    } else if (options.showIsp) {
      // No endpoints: draw direct process → ISP edges.
      for (const [procKey, ispCounts] of procISP) {
        for (const [ispKey, count] of ispCounts) {
          es.push({
            from: `proc:${procKey}`,
            to: `isp:${ispKey}`,
            tier: "p-i",
            weight: count,
          });
        }
      }
    }

    return { nodes: ns, edges: es };
  }, [
    data,
    filteredPeers,
    size,
    options.showIsp,
    options.showEndpoints,
    options.uniqueProcesses,
  ]);

  const selectedNode = useMemo(() => {
    if (!selected) return null;
    return nodes.find((n) => n.id === selected) ?? null;
  }, [nodes, selected]);

  if (isLoading) {
    return (
      <SectionShell>
        <div className="p-6 text-xs text-slate-500">Loading peer graph…</div>
      </SectionShell>
    );
  }
  if (isError || !data) {
    return (
      <SectionShell>
        <div className="p-6 text-xs text-red-300">Failed to load network graph.</div>
      </SectionShell>
    );
  }
  if (data.peers.length === 0) {
    return (
      <SectionShell>
        <div className="p-6 text-xs text-slate-500">
          No active peers at last snapshot. The probe needs at least one
          ESTABLISHED conversation outside loopback to populate this graph.
        </div>
      </SectionShell>
    );
  }

  return (
    <div className="rounded-xl border border-ink-800 bg-ink-900">
      {/* Header / filters bar */}
      <div className="flex flex-wrap items-center gap-2 border-b border-ink-800 px-3 py-2">
        <h3 className="text-sm font-semibold text-slate-200">Network Topology</h3>
        <span className="text-[11px] text-slate-500">
          {counts.total} peers · {counts.outbound} out · {counts.inbound} in · {counts.publicCount} public
        </span>
        <div className="ml-auto flex flex-wrap items-center gap-2">
          <Pill label="Direction" values={DIRECTIONS} value={direction} onChange={setDirection} />
          <Pill label="Scope" values={SCOPES} value={scope} onChange={setScope} />
          <input
            value={procFilter}
            onChange={(e) => setProcFilter(e.target.value)}
            placeholder="Filter by process…"
            className="h-7 w-44 rounded-md border border-ink-700 bg-ink-950 px-2 text-xs text-slate-200 placeholder:text-slate-600 focus:border-sonar-500 focus:outline-none"
          />
          <button
            type="button"
            onClick={() => setLegendOpen((v) => !v)}
            className={
              "rounded-md border px-2 py-1 text-[11px] " +
              (legendOpen
                ? "border-sonar-500 bg-sonar-500/10 text-sonar-400"
                : "border-ink-700 bg-ink-900 text-slate-300 hover:border-ink-600")
            }
            title="Show legend"
          >
            <IconLegend className="mr-1 inline-block h-3.5 w-3.5 -translate-y-px" /> Map Legend
          </button>
        </div>
      </div>

      {/* Canvas + overlays */}
      <div ref={containerRef} className="relative h-[75vh] min-h-[520px] overflow-hidden bg-ink-950">
        <ForceGraph<NetNodeData, NetEdgeInput>
          ref={graphRef}
          nodes={nodes}
          edges={edges}
          width={size.w}
          height={size.h}
          enableZoomPan
          staticLayout
          worldPadding={28}
          renderEdge={(e, a, b) => <NetEdge edge={e} a={a} b={b} selectedId={selected} />}
          renderNode={(s) => (
            <NetNode sim={s} selected={selected === s.id} options={options} />
          )}
          onNodeClick={(n) => setSelected((cur) => (cur === n.id ? null : n.id))}
          onBackgroundClick={() => setSelected(null)}
        />

        {/* Display options (left, collapsible) */}
        <div className="absolute left-3 top-3 w-64 rounded-lg border border-ink-700 bg-ink-900/95 text-xs shadow-xl backdrop-blur">
          <button
            type="button"
            onClick={() => setOptionsCollapsed((v) => !v)}
            className="flex w-full items-center justify-between px-3 py-2 text-left"
          >
            <span className="text-[11px] font-semibold uppercase tracking-wide text-slate-300">
              Display options
            </span>
            <span className="text-slate-500">{optionsCollapsed ? "+" : "−"}</span>
          </button>
          {!optionsCollapsed && (
            <div className="space-y-2 border-t border-ink-800 px-3 pb-3 pt-2">
              <div className="text-[10px] uppercase tracking-wider text-slate-500">
                Entity types
              </div>
              <Toggle
                label="Show ISP"
                value={options.showIsp}
                onChange={(v) => setOptions((o) => ({ ...o, showIsp: v }))}
              />
              <Toggle
                label="Show endpoints"
                value={options.showEndpoints}
                onChange={(v) => setOptions((o) => ({ ...o, showEndpoints: v }))}
              />
              <Toggle
                label="Unique processes"
                value={options.uniqueProcesses}
                onChange={(v) => setOptions((o) => ({ ...o, uniqueProcesses: v }))}
              />
              <div className="pt-2 text-[10px] uppercase tracking-wider text-slate-500">
                Label visibility
              </div>
              <Toggle
                label="Process labels"
                value={options.showProcessLabels}
                onChange={(v) => setOptions((o) => ({ ...o, showProcessLabels: v }))}
              />
              <Toggle
                label="Endpoint labels"
                value={options.showEndpointLabels}
                onChange={(v) => setOptions((o) => ({ ...o, showEndpointLabels: v }))}
              />
              <Toggle
                label="ISP labels"
                value={options.showIspLabels}
                onChange={(v) => setOptions((o) => ({ ...o, showIspLabels: v }))}
              />
              <Toggle
                label="Host label"
                value={options.showHostLabel}
                onChange={(v) => setOptions((o) => ({ ...o, showHostLabel: v }))}
              />
            </div>
          )}
        </div>

        {/* Legend popover (top-right) */}
        {legendOpen && (
          <div className="absolute right-3 top-3 w-60 rounded-lg border border-ink-700 bg-ink-900/95 p-3 text-xs shadow-xl backdrop-blur">
            <div className="mb-2 flex items-center justify-between">
              <span className="text-[11px] font-semibold uppercase tracking-wide text-slate-300">
                Legend
              </span>
              <button
                onClick={() => setLegendOpen(false)}
                className="text-slate-500 hover:text-slate-300"
              >
                ×
              </button>
            </div>
            <ul className="space-y-1.5 text-slate-300">
              <li className="flex items-center gap-2">
                <span className="inline-flex h-5 w-5 items-center justify-center rounded-full bg-sonar-500 text-white">
                  <IconHost className="h-3 w-3" />
                </span>
                Host (this agent)
              </li>
              <li className="flex items-center gap-2">
                <span className="inline-flex items-center gap-1 rounded-full bg-slate-100 px-2 py-0.5 text-[10px] font-medium text-slate-800">
                  <IconGear className="h-3 w-3 text-sonar-600" />
                  proc.exe
                </span>
                Process
              </li>
              <li className="flex items-center gap-2">
                <span className="inline-flex h-5 w-5 items-center justify-center rounded-full bg-sky-500/20 text-sky-300 ring-1 ring-sky-400">
                  <IconPin className="h-3 w-3" />
                </span>
                Endpoint address
              </li>
              <li className="flex items-center gap-2">
                <span className="inline-flex items-center gap-1 rounded-full bg-slate-100 px-2 py-0.5 text-[10px] font-medium text-slate-800">
                  <IconGlobe className="h-3 w-3 text-slate-700" />
                  ISP
                </span>
                ISP / organisation
              </li>
              <li className="pt-2 text-[10px] uppercase tracking-wide text-slate-500">
                Edges
              </li>
              <li className="flex items-center gap-2">
                <svg width="36" height="6"><line x1="0" y1="3" x2="36" y2="3" stroke="#64748b" strokeWidth="1.6" /></svg>
                Host → process
              </li>
              <li className="flex items-center gap-2">
                <svg width="36" height="6"><line x1="0" y1="3" x2="36" y2="3" stroke="#0ea5e9" strokeWidth="1.4" /></svg>
                Process → ISP
              </li>
              <li className="flex items-center gap-2">
                <svg width="36" height="6"><line x1="0" y1="3" x2="36" y2="3" stroke="#475569" strokeWidth="1.2" strokeDasharray="3 3" /></svg>
                Endpoint → ISP <span className="text-slate-500">(when shown)</span>
              </li>
            </ul>
            <div className="mt-2 border-t border-ink-800 pt-2 text-[10px] text-slate-500">
              Drag any node to reposition · scroll to zoom · drag empty space to pan.
            </div>
          </div>
        )}

        {/* Zoom controls (bottom-right) */}
        <div className="absolute bottom-3 right-3 flex flex-col rounded-md border border-ink-700 bg-ink-900/95 text-slate-200 shadow-xl backdrop-blur">
          <button
            onClick={() => graphRef.current?.zoomBy(1.25)}
            className="border-b border-ink-800 px-2 py-1 hover:bg-ink-800"
            title="Zoom in"
          >
            +
          </button>
          <button
            onClick={() => graphRef.current?.zoomBy(0.8)}
            className="border-b border-ink-800 px-2 py-1 hover:bg-ink-800"
            title="Zoom out"
          >
            −
          </button>
          <button
            onClick={() => graphRef.current?.fit(60)}
            className="border-b border-ink-800 px-2 py-1 text-[10px] hover:bg-ink-800"
            title="Fit to view"
          >
            ⤢
          </button>
          <button
            onClick={() => graphRef.current?.resetView()}
            className="px-2 py-1 text-[10px] hover:bg-ink-800"
            title="Reset view"
          >
            ⟳
          </button>
        </div>

        {/* Node details (right, only when selected) */}
        {selectedNode && (
          <NodeDetailsPanel
            node={selectedNode}
            onClose={() => setSelected(null)}
            onCenter={() => graphRef.current?.centerOn(selectedNode.id)}
          />
        )}
      </div>
    </div>
  );
}

// ===================== EDGE RENDERER =========================

function NetEdge({
  edge,
  a,
  b,
  selectedId,
}: {
  edge: NetEdgeInput;
  a: SimNode<NetNodeData>;
  b: SimNode<NetNodeData>;
  selectedId: string | null;
}) {
  const isSel =
    selectedId != null && (a.id === selectedId || b.id === selectedId);
  let stroke: string;
  let dash: string | undefined;
  let width: number;
  switch (edge.tier) {
    case "h-p":
      stroke = isSel ? "#7dd3fc" : "#64748b";
      width = isSel ? 2.2 : 1.6;
      break;
    case "p-i":
      // Direct process → ISP edge (default view). Sky blue, lightly
      // de-emphasised when no node is selected so the labels read
      // through.
      stroke = isSel ? "#38bdf8" : "#0ea5e9";
      width = isSel ? 2.2 : 1.4;
      break;
    case "p-e":
      stroke = isSel ? "#38bdf8" : "#0ea5e9";
      width = isSel ? 2.2 : 1.3;
      break;
    case "e-i":
      stroke = isSel ? "#94a3b8" : "#475569";
      width = isSel ? 1.6 : 1.1;
      dash = "3 4";
      break;
  }
  return (
    <line
      key={`${edge.from}->${edge.to}`}
      x1={a.x}
      y1={a.y}
      x2={b.x}
      y2={b.y}
      stroke={stroke}
      strokeWidth={width}
      strokeDasharray={dash}
      opacity={isSel ? 1 : 0.55}
    />
  );
}

// ===================== NODE RENDERER =========================

function NetNode({
  sim,
  selected,
  options,
}: {
  sim: SimNode<NetNodeData>;
  selected: boolean;
  options: DisplayOptions;
}) {
  const n = sim.data;
  switch (n.kind) {
    case "host":
      return <HostNode sim={sim} selected={selected} showLabel={options.showHostLabel} />;
    case "process":
      return <ProcessPill sim={sim} selected={selected} showLabel={options.showProcessLabels} />;
    case "endpoint":
      return <EndpointPin sim={sim} selected={selected} showLabel={options.showEndpointLabels} />;
    case "isp":
      return <IspPill sim={sim} selected={selected} showLabel={options.showIspLabels} />;
  }
}

function HostNode({
  sim,
  selected,
  showLabel,
}: {
  sim: SimNode<NetNodeData>;
  selected: boolean;
  showLabel: boolean;
}) {
  const n = sim.data;
  const r = 28;
  return (
    <>
      <circle
        cx={sim.x}
        cy={sim.y}
        r={r + 6}
        fill="#0ea5e933"
        stroke="none"
      />
      <circle
        cx={sim.x}
        cy={sim.y}
        r={r}
        fill="#0ea5e9"
        stroke={selected ? "#7dd3fc" : "#0284c7"}
        strokeWidth={selected ? 3 : 2}
      />
      <SvgIconHost x={sim.x - 9} y={sim.y - 9} size={18} color="#ffffff" />
      {showLabel && (
        <>
          <text
            x={sim.x}
            y={sim.y + r + 16}
            textAnchor="middle"
            className="pointer-events-none select-none fill-slate-100 text-[12px] font-semibold"
          >
            {truncate(n.label, 28)}
          </text>
          {n.sub && (
            <text
              x={sim.x}
              y={sim.y + r + 30}
              textAnchor="middle"
              className="pointer-events-none select-none fill-slate-500 font-mono text-[10px]"
            >
              {n.sub}
            </text>
          )}
        </>
      )}
    </>
  );
}

function ProcessPill({
  sim,
  selected,
  showLabel,
}: {
  sim: SimNode<NetNodeData>;
  selected: boolean;
  showLabel: boolean;
}) {
  const n = sim.data;
  const proc = n.process!;
  // Pill width is label-driven; we measure approximate width from
  // string length — good enough since SVG <text> can't trivially be
  // measured before render and we don't want to round-trip through
  // getBBox each frame.
  const text = proc.name + (proc.pid != null ? ` · ${proc.pid}` : "");
  const charW = 6.4;
  const pad = 12;
  const iconW = 14;
  const w = showLabel ? Math.max(60, iconW + pad * 2 + text.length * charW) : 28;
  const h = 22;
  const cx = sim.x;
  const cy = sim.y;
  const x = cx - w / 2;
  const y = cy - h / 2;
  return (
    <>
      <rect
        x={x}
        y={y}
        width={w}
        height={h}
        rx={h / 2}
        ry={h / 2}
        fill="#f1f5f9"
        stroke={selected ? "#0ea5e9" : "#cbd5e1"}
        strokeWidth={selected ? 2 : 1}
      />
      <SvgIconGear x={x + pad - 2} y={cy - 7} size={14} color="#2563eb" />
      {showLabel && (
        <text
          x={x + pad + iconW}
          y={cy + 4}
          className="pointer-events-none select-none fill-slate-800 text-[11px] font-medium"
        >
          {text}
        </text>
      )}
    </>
  );
}

function EndpointPin({
  sim,
  selected,
  showLabel,
}: {
  sim: SimNode<NetNodeData>;
  selected: boolean;
  showLabel: boolean;
}) {
  const n = sim.data;
  const peer = n.endpoint!;
  const r = 9;
  const fill =
    peer.direction === "inbound"
      ? "#22c55e33"
      : peer.isPrivate
        ? "#64748b33"
        : "#0ea5e933";
  const ring =
    peer.direction === "inbound"
      ? "#22c55e"
      : peer.isPrivate
        ? "#94a3b8"
        : "#38bdf8";
  return (
    <>
      <circle
        cx={sim.x}
        cy={sim.y}
        r={r + (selected ? 4 : 2)}
        fill={selected ? "#0ea5e933" : fill}
        stroke={ring}
        strokeWidth={selected ? 2 : 1.2}
      />
      <SvgIconPin x={sim.x - 6} y={sim.y - 7} size={12} color={ring} />
      {showLabel && (
        <>
          <text
            x={sim.x}
            y={sim.y + r + 12}
            textAnchor="middle"
            className="pointer-events-none select-none fill-slate-200 text-[10px]"
          >
            {truncate(peer.host || peer.ip, 22)}
          </text>
          {n.sub && (
            <text
              x={sim.x}
              y={sim.y + r + 24}
              textAnchor="middle"
              className="pointer-events-none select-none fill-slate-500 text-[9px]"
            >
              {truncate(n.sub, 24)}
            </text>
          )}
        </>
      )}
    </>
  );
}

function IspPill({
  sim,
  selected,
  showLabel,
}: {
  sim: SimNode<NetNodeData>;
  selected: boolean;
  showLabel: boolean;
}) {
  const n = sim.data;
  const isp = n.isp!;
  const text = isp.org;
  const charW = 6.6;
  const pad = 14;
  const iconW = 16;
  const w = showLabel ? Math.max(80, iconW + pad * 2 + text.length * charW) : 28;
  const h = 26;
  const cx = sim.x;
  const cy = sim.y;
  const x = cx - w / 2;
  const y = cy - h / 2;
  return (
    <>
      <rect
        x={x}
        y={y}
        width={w}
        height={h}
        rx={h / 2}
        ry={h / 2}
        fill="#f8fafc"
        stroke={selected ? "#0ea5e9" : "#94a3b8"}
        strokeWidth={selected ? 2 : 1.2}
      />
      <SvgIconGlobe x={x + pad - 4} y={cy - 8} size={16} color="#334155" />
      {showLabel && (
        <text
          x={x + pad + iconW - 2}
          y={cy + 5}
          className="pointer-events-none select-none fill-slate-800 text-[11px] font-medium"
        >
          {truncate(text, 36)}
        </text>
      )}
    </>
  );
}

// ===================== NODE DETAILS PANEL ====================

function NodeDetailsPanel({
  node,
  onClose,
  onCenter,
}: {
  node: NetNodeData;
  onClose: () => void;
  onCenter: () => void;
}) {
  return (
    <div className="absolute right-3 top-3 z-10 max-h-[calc(75vh-1.5rem)] w-80 overflow-auto rounded-lg border border-ink-700 bg-ink-900/95 p-3 text-xs shadow-xl backdrop-blur">
      <div className="mb-2 flex items-baseline justify-between gap-2">
        <div className="flex items-center gap-1.5">
          <NodeKindIcon kind={node.kind} />
          <span className="text-[11px] font-semibold uppercase tracking-wide text-slate-300">
            Node Details
          </span>
        </div>
        <div className="flex items-center gap-1">
          <button
            onClick={onCenter}
            className="rounded p-1 text-slate-400 hover:bg-ink-800 hover:text-slate-200"
            title="Center on this node"
          >
            ⌖
          </button>
          <button
            onClick={onClose}
            className="rounded p-1 text-slate-400 hover:bg-ink-800 hover:text-slate-200"
            title="Close"
          >
            ×
          </button>
        </div>
      </div>
      {node.kind === "host" && <HostDetails host={node.host!} />}
      {node.kind === "process" && <ProcessDetails proc={node.process!} />}
      {node.kind === "endpoint" && <EndpointDetails peer={node.endpoint!} />}
      {node.kind === "isp" && <IspDetails isp={node.isp!} />}
    </div>
  );
}

function HostDetails({ host }: { host: AgentNetworkGraph["agent"] }) {
  return (
    <dl className="space-y-1.5">
      <Field label="Node Type" value="Host (this agent)" />
      <Field label="Hostname" value={host.hostname} mono />
      <Field label="Primary IP" value={host.primaryIp ?? "—"} mono />
      <Field label="Public IP" value={host.publicIp ?? "—"} mono />
      <Field
        label="Location"
        value={
          host.city || host.countryIso
            ? `${host.city ?? ""}${host.city && host.countryIso ? ", " : ""}${host.countryIso ?? ""}`
            : "—"
        }
      />
      <Field label="ISP" value={host.org ?? "—"} />
      <Field label="ASN" value={host.asn ? `AS${host.asn}` : "—"} mono />
      <div className="pt-1">
        <Link
          to={`/agents/${host.id}`}
          className="text-[11px] text-sonar-400 hover:underline"
        >
          Open agent detail
        </Link>
      </div>
    </dl>
  );
}

function ProcessDetails({ proc }: { proc: ProcessAgg }) {
  return (
    <dl className="space-y-1.5">
      <Field label="Node Type" value="Process" />
      <Field label="Name" value={proc.name} mono />
      {proc.pid != null && <Field label="PID" value={String(proc.pid)} mono />}
      <Field label="Endpoints" value={String(proc.peers.size)} />
      <Field label="Connections" value={String(proc.totalConns)} />
    </dl>
  );
}

function EndpointDetails({ peer }: { peer: AgentNetworkPeer }) {
  return (
    <dl className="space-y-1.5">
      <Field label="Node Type" value="Endpoint Address" />
      <Field
        label="Address"
        value={peer.ip + (peer.ports?.length ? `:${peer.ports.slice(0, 3).join("/")}` : "")}
        mono
      />
      <Field label="DNS Name" value={peer.host || "—"} mono />
      <Field label="Direction" value={peer.direction} />
      <Field label="Scope" value={peer.isPrivate ? "private" : "public"} />
      <Field label="Connections" value={String(peer.totalConns)} />
      <Field
        label="Ports"
        value={(peer.ports ?? []).slice(0, 8).join(", ") || "—"}
        mono
      />
      <Field
        label="Country"
        value={
          peer.countryName
            ? peer.countryName + (peer.countryIso ? ` (${peer.countryIso})` : "")
            : peer.countryIso || "—"
        }
      />
      <Field label="City" value={peer.city || "—"} />
      <Field label="ISP" value={peer.org || "—"} />
      <Field label="ASN" value={peer.asn ? `AS${peer.asn}` : "—"} mono />
      <div className="pt-1">
        <div className="mb-1 text-[10px] uppercase tracking-wider text-slate-500">
          Processes touching this endpoint
        </div>
        <ul className="space-y-0.5">
          {peer.processes.map((p, i) => (
            <li
              key={i}
              className="flex items-center justify-between rounded bg-ink-950/60 px-2 py-1"
            >
              <span className="font-mono text-slate-200">{p.name || "—"}</span>
              <span className="tabular-nums text-slate-500">
                {p.pid != null ? `pid ${p.pid} · ` : ""}
                {p.count}×
              </span>
            </li>
          ))}
        </ul>
      </div>
      {/* Fields the reference UI shows that we don't capture yet —
          rendered transparently with "—" so operators learn the shape
          of the data model and we can light them up later when the
          probe collects them. */}
      <div className="pt-1">
        <div className="mb-1 text-[10px] uppercase tracking-wider text-slate-500">
          Performance (probe v-future)
        </div>
        <Field label="Upload Rate" value="—" />
        <Field label="Download Rate" value="—" />
        <Field label="Avg. Latency" value="—" />
        <Field label="Max Latency" value="—" />
        <Field label="Packet Loss" value="—" />
      </div>
    </dl>
  );
}

function IspDetails({ isp }: { isp: IspAgg }) {
  return (
    <dl className="space-y-1.5">
      <Field label="Node Type" value="ISP" />
      <Field label="Organisation" value={isp.org} />
      <Field label="ASN" value={isp.asn ? `AS${isp.asn}` : "—"} mono />
      <Field label="Endpoints" value={String(isp.peers.size)} />
      <Field
        label="Countries"
        value={Array.from(isp.countries).sort().join(", ") || "—"}
      />
    </dl>
  );
}

function NodeKindIcon({ kind }: { kind: NodeKind }) {
  const cls = "h-3.5 w-3.5";
  switch (kind) {
    case "host":
      return <IconHost className={cls + " text-sonar-400"} />;
    case "process":
      return <IconGear className={cls + " text-sonar-400"} />;
    case "endpoint":
      return <IconPin className={cls + " text-sonar-400"} />;
    case "isp":
      return <IconGlobe className={cls + " text-sonar-400"} />;
  }
}

function Field({
  label,
  value,
  mono,
}: {
  label: string;
  value: string;
  mono?: boolean;
}) {
  return (
    <div className="flex items-baseline justify-between gap-2 border-b border-ink-800/60 py-0.5 last:border-b-0">
      <dt className="shrink-0 text-[10px] uppercase tracking-wider text-slate-500">
        {label}
      </dt>
      <dd
        className={
          "min-w-0 truncate text-right text-[11px] text-slate-200 " +
          (mono ? "font-mono" : "")
        }
        title={value}
      >
        {value}
      </dd>
    </div>
  );
}

// ===================== MISC UI =========================

function Toggle({
  label,
  value,
  onChange,
}: {
  label: string;
  value: boolean;
  onChange: (v: boolean) => void;
}) {
  return (
    <button
      type="button"
      onClick={() => onChange(!value)}
      className="flex w-full items-center justify-between rounded px-1 py-1 text-left hover:bg-ink-800/60"
    >
      <span className="text-slate-300">{label}</span>
      <span
        className={
          "relative inline-flex h-4 w-7 items-center rounded-full transition " +
          (value ? "bg-sonar-500" : "bg-ink-700")
        }
      >
        <span
          className={
            "inline-block h-3 w-3 transform rounded-full bg-white transition " +
            (value ? "translate-x-3.5" : "translate-x-0.5")
          }
        />
      </span>
    </button>
  );
}

function Pill<T extends string>({
  label,
  values,
  value,
  onChange,
}: {
  label: string;
  values: readonly T[];
  value: T;
  onChange: (v: T) => void;
}) {
  return (
    <div className="flex items-center gap-1">
      <span className="text-[10px] uppercase tracking-wide text-slate-500">
        {label}
      </span>
      <div className="flex overflow-hidden rounded-md border border-ink-700 text-[11px]">
        {values.map((v) => (
          <button
            key={v}
            type="button"
            onClick={() => onChange(v)}
            className={
              "px-2 py-0.5 transition " +
              (value === v
                ? "bg-sonar-500/20 text-sonar-400"
                : "bg-ink-900 text-slate-400 hover:bg-ink-800")
            }
          >
            {v}
          </button>
        ))}
      </div>
    </div>
  );
}

function SectionShell({ children }: { children: React.ReactNode }) {
  return (
    <div className="rounded-xl border border-ink-800 bg-ink-900">
      <div className="border-b border-ink-800 px-3 py-2 text-sm font-semibold text-slate-200">
        Network Topology
      </div>
      {children}
    </div>
  );
}

function truncate(s: string, n: number) {
  if (!s) return "";
  return s.length > n ? s.slice(0, n - 1) + "…" : s;
}

// ===================== ICONS (inline SVG) =====================
//
// Two flavours per icon, because nested <svg> inside an outer <svg>
// has surprising sizing rules across browsers. Using path-only
// renders that scale via SVG transform="scale(k)" keeps things
// predictable on the canvas; the wrapped <svg> variant is for HTML
// contexts (legend popover, header buttons).

interface IconProps {
  size?: number;
  className?: string;
  style?: React.CSSProperties;
}

// Wrapped <svg> versions — for use in HTML (legend / header).

function IconHost({ size = 16, className, style }: IconProps) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className={className} style={style}>
      <PathHost />
    </svg>
  );
}

function IconGear({ size = 16, className, style }: IconProps) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2.2" strokeLinecap="round" strokeLinejoin="round" className={className} style={style}>
      <PathGear />
    </svg>
  );
}

function IconGlobe({ size = 16, className, style }: IconProps) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round" className={className} style={style}>
      <PathGlobe />
    </svg>
  );
}

function IconPin({ size = 16, className, style }: IconProps) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className={className} style={style}>
      <PathPin />
    </svg>
  );
}

function IconLegend({ size = 14, className, style }: IconProps) {
  return (
    <svg width={size} height={size} viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" className={className} style={style}>
      <rect x="3" y="3" width="7" height="7" />
      <rect x="14" y="3" width="7" height="7" />
      <rect x="3" y="14" width="7" height="7" />
      <rect x="14" y="14" width="7" height="7" />
    </svg>
  );
}

// In-canvas (path-only) versions — for use *inside* the outer
// ForceGraph SVG. Render a <g> with the right transform so the
// 24×24 source path becomes the requested pixel size.

interface SvgIconProps {
  x: number;
  y: number;
  size: number;
  color: string;
  strokeWidth?: number;
  fillBg?: string;
}

function SvgIconGear({ x, y, size, color, strokeWidth = 2.2 }: SvgIconProps) {
  const k = size / 24;
  return (
    <g
      transform={`translate(${x}, ${y}) scale(${k})`}
      fill="none"
      stroke={color}
      strokeWidth={strokeWidth / k}
      strokeLinecap="round"
      strokeLinejoin="round"
      pointerEvents="none"
    >
      <PathGear />
    </g>
  );
}

function SvgIconGlobe({ x, y, size, color, strokeWidth = 1.8 }: SvgIconProps) {
  const k = size / 24;
  return (
    <g
      transform={`translate(${x}, ${y}) scale(${k})`}
      fill="none"
      stroke={color}
      strokeWidth={strokeWidth / k}
      strokeLinecap="round"
      strokeLinejoin="round"
      pointerEvents="none"
    >
      <PathGlobe />
    </g>
  );
}

function SvgIconPin({ x, y, size, color, strokeWidth = 2 }: SvgIconProps) {
  const k = size / 24;
  return (
    <g
      transform={`translate(${x}, ${y}) scale(${k})`}
      fill="none"
      stroke={color}
      strokeWidth={strokeWidth / k}
      strokeLinecap="round"
      strokeLinejoin="round"
      pointerEvents="none"
    >
      <PathPin pinFill={color} />
    </g>
  );
}

function SvgIconHost({ x, y, size, color, strokeWidth = 2 }: SvgIconProps) {
  const k = size / 24;
  return (
    <g
      transform={`translate(${x}, ${y}) scale(${k})`}
      fill="none"
      stroke={color}
      strokeWidth={strokeWidth / k}
      strokeLinecap="round"
      strokeLinejoin="round"
      pointerEvents="none"
    >
      <PathHost />
    </g>
  );
}

// Path primitives — shared by both the HTML <svg> and in-canvas
// transform-based renderers.

function PathHost() {
  return (
    <>
      <rect x="3" y="6" width="18" height="12" rx="2" />
      <line x1="3" y1="14" x2="21" y2="14" />
      <line x1="8" y1="18" x2="16" y2="18" />
      <line x1="12" y1="18" x2="12" y2="22" />
    </>
  );
}

function PathGear() {
  return (
    <>
      <circle cx="12" cy="12" r="3" />
      <path d="M19.4 15a1.65 1.65 0 0 0 .33 1.82l.06.06a2 2 0 1 1-2.83 2.83l-.06-.06a1.65 1.65 0 0 0-1.82-.33 1.65 1.65 0 0 0-1 1.51V21a2 2 0 1 1-4 0v-.09a1.65 1.65 0 0 0-1-1.51 1.65 1.65 0 0 0-1.82.33l-.06.06a2 2 0 1 1-2.83-2.83l.06-.06a1.65 1.65 0 0 0 .33-1.82 1.65 1.65 0 0 0-1.51-1H3a2 2 0 1 1 0-4h.09a1.65 1.65 0 0 0 1.51-1 1.65 1.65 0 0 0-.33-1.82l-.06-.06a2 2 0 1 1 2.83-2.83l.06.06a1.65 1.65 0 0 0 1.82.33H9a1.65 1.65 0 0 0 1-1.51V3a2 2 0 1 1 4 0v.09a1.65 1.65 0 0 0 1 1.51 1.65 1.65 0 0 0 1.82-.33l.06-.06a2 2 0 1 1 2.83 2.83l-.06.06a1.65 1.65 0 0 0-.33 1.82V9a1.65 1.65 0 0 0 1.51 1H21a2 2 0 1 1 0 4h-.09a1.65 1.65 0 0 0-1.51 1z" />
    </>
  );
}

function PathGlobe() {
  return (
    <>
      <circle cx="12" cy="12" r="10" />
      <line x1="2" y1="12" x2="22" y2="12" />
      <path d="M12 2a15 15 0 0 1 0 20" />
      <path d="M12 2a15 15 0 0 0 0 20" />
    </>
  );
}

function PathPin({ pinFill }: { pinFill?: string }) {
  return (
    <>
      <path d="M12 22s7-7.5 7-13A7 7 0 0 0 5 9c0 5.5 7 13 7 13z" />
      <circle cx="12" cy="9" r="2.5" fill={pinFill ?? "currentColor"} stroke="none" />
    </>
  );
}

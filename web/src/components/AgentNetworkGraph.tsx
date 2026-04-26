// AgentNetworkGraph — the "Network connections" section on the
// agent detail page. Renders a force-directed graph centered on the
// host, with one outer node per remote peer (after aggregation by
// remote IP). The host is pinned in the middle so the layout reads
// like a star/sun rather than a generic blob.
//
// Data shape comes straight from /agents/{id}/network-graph:
//   * agent: hostname + GeoIP/public IP for the center label
//   * peers: aggregated remote endpoints with process attribution
//
// The component owns:
//   * Filter row (direction toggle + "private only/public only" + a
//     small process-name search; useful when a chatty box has
//     hundreds of peers from a single TLS scanner).
//   * Side panel that shows the selected peer's full detail —
//     processes, ports, ASN/Org — without leaving the graph view.

import { useQuery } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { api } from "../api/client";
import type { AgentNetworkGraph, AgentNetworkPeer } from "../api/types";
import ForceGraph, {
  type ForceEdgeInput,
  type ForceNodeInput,
  type SimNode,
} from "./ForceGraph";

interface NetNodeData extends ForceNodeInput {
  kind: "host" | "peer";
  label: string;
  sub?: string;
  peer?: AgentNetworkPeer;
}

const DIRECTIONS: Array<"all" | "outbound" | "inbound"> = ["all", "outbound", "inbound"];
const SCOPES: Array<"all" | "public" | "private"> = ["all", "public", "private"];

export default function AgentNetworkGraphSection({ agentId }: { agentId: string }) {
  const { data, isLoading, isError } = useQuery({
    queryKey: ["agent-netgraph", agentId],
    queryFn: () => api.get<AgentNetworkGraph>(`/agents/${agentId}/network-graph`),
    refetchInterval: 30_000,
  });

  const [direction, setDirection] = useState<(typeof DIRECTIONS)[number]>("all");
  const [scope, setScope] = useState<(typeof SCOPES)[number]>("all");
  const [proc, setProc] = useState("");
  const [selected, setSelected] = useState<string | null>(null);

  const filteredPeers = useMemo(() => {
    const list = data?.peers ?? [];
    const q = proc.trim().toLowerCase();
    return list.filter((p) => {
      if (direction !== "all" && p.direction !== direction) return false;
      if (scope === "public" && p.isPrivate) return false;
      if (scope === "private" && !p.isPrivate) return false;
      if (q && !p.processes.some((pr) => pr.name.toLowerCase().includes(q))) return false;
      return true;
    });
  }, [data, direction, scope, proc]);

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

  // Width is fixed-ish at 720; layout is happiest with ample space
  // in a smaller-than-page canvas because the host is pinned at the
  // middle.
  const W = 760;
  const H = 480;

  const nodes = useMemo<NetNodeData[]>(() => {
    if (!data) return [];
    const host: NetNodeData = {
      id: "__host__",
      kind: "host",
      label: data.agent.hostname,
      sub: data.agent.primaryIp ?? undefined,
      pinned: true,
      initialX: W / 2,
      initialY: H / 2,
    };
    const peers = filteredPeers.map<NetNodeData>((p) => ({
      id: "peer:" + p.direction + ":" + p.ip,
      kind: "peer",
      label: p.host || p.ip,
      sub: p.org || (p.isPrivate ? "private" : p.countryIso),
      peer: p,
    }));
    return [host, ...peers];
  }, [data, filteredPeers]);

  const edges = useMemo<ForceEdgeInput[]>(
    () =>
      filteredPeers.map((p) => ({
        from: "__host__",
        to: "peer:" + p.direction + ":" + p.ip,
        rest: 110 + Math.min(80, Math.log2(p.totalConns + 2) * 14),
      })),
    [filteredPeers],
  );

  const selectedPeer = useMemo(() => {
    if (!selected || !data) return null;
    if (!selected.startsWith("peer:")) return null;
    return nodes.find((n) => n.id === selected)?.peer ?? null;
  }, [selected, data, nodes]);

  if (isLoading) {
    return (
      <Section title="Network connections">
        <div className="text-xs text-slate-500">Loading peer graph…</div>
      </Section>
    );
  }
  if (isError || !data) {
    return (
      <Section title="Network connections">
        <div className="text-xs text-red-300">Failed to load network graph.</div>
      </Section>
    );
  }
  if (data.peers.length === 0) {
    return (
      <Section title="Network connections">
        <div className="text-xs text-slate-500">
          No active peers at last snapshot. The probe needs at least one
          ESTABLISHED conversation outside loopback to populate this
          graph.
        </div>
      </Section>
    );
  }

  return (
    <Section title={`Network connections (${counts.total} peers · ${counts.outbound} out · ${counts.inbound} in · ${counts.publicCount} public)`}>
      <div className="flex flex-wrap items-center gap-2 pb-2">
        <Pill
          label="Direction"
          values={DIRECTIONS}
          value={direction}
          onChange={setDirection}
        />
        <Pill label="Scope" values={SCOPES} value={scope} onChange={setScope} />
        <input
          value={proc}
          onChange={(e) => setProc(e.target.value)}
          placeholder="Filter by process name…"
          className="h-7 w-56 rounded-md border border-ink-700 bg-ink-950 px-2 text-xs text-slate-200 placeholder:text-slate-600 focus:border-sonar-500 focus:outline-none"
        />
        <span className="text-[11px] text-slate-500">
          showing {filteredPeers.length} of {counts.total}
        </span>
        <span className="ml-auto text-[10px] uppercase tracking-wide text-slate-500">
          drag a peer to reposition · click for details
        </span>
      </div>

      <div className="grid grid-cols-1 gap-3 lg:grid-cols-[1fr_18rem]">
        <div className="rounded-lg border border-ink-800 bg-ink-950/40">
          <ForceGraph<NetNodeData, ForceEdgeInput>
            nodes={nodes}
            edges={edges}
            width={W}
            height={H}
            renderEdge={(e, a, b) => {
              const isSel = selected != null && (a.id === selected || b.id === selected);
              return (
                <line
                  key={a.id + "->" + b.id}
                  x1={a.x}
                  y1={a.y}
                  x2={b.x}
                  y2={b.y}
                  stroke={isSel ? "#0ea5e9" : "#334155"}
                  strokeWidth={isSel ? 2 : 1.2}
                  opacity={isSel ? 1 : 0.55}
                />
              );
            }}
            renderNode={(s) => <NetNode sim={s} selected={selected === s.id} />}
            onNodeClick={(n) => setSelected((cur) => (cur === n.id ? null : n.id))}
          />
        </div>
        <PeerSidePanel peer={selectedPeer} />
      </div>
    </Section>
  );
}

function NetNode({
  sim,
  selected,
}: {
  sim: SimNode<NetNodeData>;
  selected: boolean;
}) {
  const n = sim.data;
  if (n.kind === "host") {
    return (
      <>
        <circle
          cx={sim.x}
          cy={sim.y}
          r={32}
          fill="#0ea5e9"
          stroke={selected ? "#7dd3fc" : "#0284c7"}
          strokeWidth={2.5}
        />
        <text
          x={sim.x}
          y={sim.y + 4}
          textAnchor="middle"
          className="pointer-events-none select-none fill-white text-[10px] font-semibold"
        >
          host
        </text>
        <text
          x={sim.x}
          y={sim.y + 50}
          textAnchor="middle"
          className="pointer-events-none select-none fill-slate-100 text-[12px] font-medium"
        >
          {n.label}
        </text>
        {n.sub && (
          <text
            x={sim.x}
            y={sim.y + 64}
            textAnchor="middle"
            className="pointer-events-none select-none fill-slate-400 font-mono text-[9px]"
          >
            {n.sub}
          </text>
        )}
      </>
    );
  }
  const peer = n.peer!;
  const r = 12 + Math.min(16, Math.log2(peer.totalConns + 1) * 3);
  const fill =
    peer.direction === "inbound"
      ? "#22c55e"
      : peer.isPrivate
        ? "#64748b"
        : "#94a3b8";
  return (
    <>
      <circle
        cx={sim.x}
        cy={sim.y}
        r={r + (selected ? 3 : 0)}
        fill={fill}
        opacity={0.9}
        stroke={selected ? "#7dd3fc" : "#0f172a"}
        strokeWidth={selected ? 2 : 1}
      />
      <text
        x={sim.x}
        y={sim.y + r + 12}
        textAnchor="middle"
        className="pointer-events-none select-none fill-slate-200 text-[10px]"
      >
        {truncate(n.label, 22)}
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
  );
}

function PeerSidePanel({ peer }: { peer: AgentNetworkPeer | null }) {
  if (!peer) {
    return (
      <div className="rounded-lg border border-ink-800 bg-ink-900/60 p-3 text-xs text-slate-500">
        Click any peer in the graph for details.
      </div>
    );
  }
  return (
    <div className="space-y-2 rounded-lg border border-ink-800 bg-ink-900/60 p-3 text-xs">
      <div>
        <div className="text-[10px] uppercase tracking-wide text-slate-500">Peer</div>
        <div className="font-mono text-sm text-slate-100">{peer.host || peer.ip}</div>
        {peer.host && (
          <div className="font-mono text-[10px] text-slate-500">{peer.ip}</div>
        )}
      </div>
      <div className="grid grid-cols-2 gap-1.5">
        <Field label="Direction" value={peer.direction} />
        <Field
          label="Scope"
          value={peer.isPrivate ? "private" : "public"}
        />
        {peer.asn ? (
          <Field label="ASN" value={"AS" + peer.asn} />
        ) : (
          <Field label="ASN" value="—" />
        )}
        <Field label="Org" value={peer.org || "—"} />
        <Field
          label="Country"
          value={
            peer.countryName
              ? peer.countryName + (peer.countryIso ? ` (${peer.countryIso})` : "")
              : peer.countryIso || "—"
          }
        />
        <Field label="City" value={peer.city || "—"} />
        <Field
          label="Conns"
          value={String(peer.totalConns)}
        />
        <Field
          label="Ports"
          value={(peer.ports ?? []).slice(0, 6).join(", ") || "—"}
        />
      </div>
      <div>
        <div className="mb-1 text-[10px] uppercase tracking-wide text-slate-500">
          Processes
        </div>
        <ul className="space-y-0.5">
          {peer.processes.map((p, i) => (
            <li
              key={i}
              className="flex items-center justify-between rounded bg-ink-950/50 px-2 py-1"
            >
              <span className="font-mono text-slate-200">{p.name || "—"}</span>
              <span className="tabular-nums text-slate-500">{p.count}×</span>
            </li>
          ))}
        </ul>
      </div>
    </div>
  );
}

function Field({ label, value }: { label: string; value: string }) {
  return (
    <div className="min-w-0">
      <div className="text-[9px] uppercase tracking-wide text-slate-500">{label}</div>
      <div className="truncate text-slate-200" title={value}>
        {value}
      </div>
    </div>
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
      <div className="flex overflow-hidden rounded-md border border-ink-800 text-[11px]">
        {values.map((v) => (
          <button
            key={v}
            type="button"
            onClick={() => onChange(v)}
            className={
              "px-2 py-0.5 transition " +
              (value === v
                ? "bg-sonar-700/40 text-sonar-200"
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

function Section({ title, children }: { title: string; children: React.ReactNode }) {
  return (
    <div className="rounded-xl border border-ink-800 bg-ink-900 p-4">
      <h3 className="mb-2 text-xs font-semibold uppercase tracking-wide text-slate-400">
        {title}
      </h3>
      {children}
    </div>
  );
}

function truncate(s: string, n: number) {
  if (!s) return "";
  return s.length > n ? s.slice(0, n - 1) + "…" : s;
}

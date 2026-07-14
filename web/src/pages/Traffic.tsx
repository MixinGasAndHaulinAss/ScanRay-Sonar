import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api } from "../api/client";

interface FlowRow {
  srcAddr: string;
  dstAddr: string;
  bytes: number;
  packets: number;
}

interface FlowsResponse {
  generatedAt: string;
  flows: FlowRow[];
}

export default function Traffic() {
  const [ip, setIp] = useState("");
  const [search, setSearch] = useState("");

  const { data, isLoading, error, refetch, isFetching } = useQuery({
    queryKey: ["traffic-flows", search],
    queryFn: () => {
      const q = search.trim() ? `?ip=${encodeURIComponent(search.trim())}` : "";
      return api.get<FlowsResponse>(`/traffic/flows${q}`);
    },
    refetchInterval: 60_000,
  });

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h2 className="text-2xl font-semibold tracking-tight">Traffic</h2>
          <p className="mt-0.5 text-xs text-slate-500">
            Top talkers from NetFlow/IPFIX summaries (last hour). Search by host IP.
          </p>
        </div>
        <form
          className="flex items-center gap-2"
          onSubmit={(e) => {
            e.preventDefault();
            setSearch(ip);
            refetch();
          }}
        >
          <input
            placeholder="Filter by IP"
            className="rounded-md border border-ink-700 bg-ink-950 px-3 py-2 font-mono text-xs"
            value={ip}
            onChange={(e) => setIp(e.target.value)}
          />
          <button
            type="submit"
            className="rounded-full border border-ink-700 bg-ink-900 px-3 py-1.5 text-xs text-slate-200 hover:border-ink-600"
          >
            Search
          </button>
          <button
            type="button"
            onClick={() => refetch()}
            disabled={isFetching}
            className="rounded-full border border-ink-700 bg-ink-900 px-3 py-1.5 text-xs text-slate-200 hover:border-ink-600 disabled:opacity-50"
          >
            {isFetching ? "Refreshing…" : "Refresh"}
          </button>
        </form>
      </div>

      {isLoading && <p className="text-sm text-slate-500">Loading flows…</p>}
      {error && (
        <div className="rounded-md border border-red-900/60 bg-red-950/30 px-3 py-2 text-sm text-red-300">
          Failed to load traffic data.
        </div>
      )}

      <div className="overflow-hidden rounded-xl border border-ink-800 bg-ink-900">
        <table className="w-full text-left text-sm">
          <thead className="bg-ink-800/60 text-xs uppercase text-slate-400">
            <tr>
              <th className="px-3 py-2">Source</th>
              <th className="px-3 py-2">Destination</th>
              <th className="px-3 py-2 text-right">Bytes</th>
              <th className="px-3 py-2 text-right">Packets</th>
            </tr>
          </thead>
          <tbody>
            {data?.flows?.map((f, i) => (
              <tr key={`${f.srcAddr}-${f.dstAddr}-${i}`} className="border-t border-ink-800">
                <td className="px-3 py-2 font-mono text-[11px]">{f.srcAddr}</td>
                <td className="px-3 py-2 font-mono text-[11px]">{f.dstAddr}</td>
                <td className="px-3 py-2 text-right font-mono text-[11px]">
                  {f.bytes.toLocaleString()}
                </td>
                <td className="px-3 py-2 text-right font-mono text-[11px]">
                  {f.packets.toLocaleString()}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        {!data?.flows?.length && !isLoading && (
          <div className="px-4 py-8 text-center text-sm text-slate-500">
            No flow data yet. Enable{" "}
            <code className="font-mono text-xs">SONAR_FLOW_LISTEN=:2055</code> on the poller or
            sonar-flowd.
          </div>
        )}
      </div>
    </div>
  );
}

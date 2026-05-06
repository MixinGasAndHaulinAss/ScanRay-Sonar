import { useQuery } from "@tanstack/react-query";
import { Link, useParams } from "react-router-dom";
import { api } from "../api/client";
import type { Topology } from "../api/types";
import { TopologyGraph } from "./Topology";

export default function SiteNetworkMap() {
  const { siteId } = useParams<{ siteId: string }>();
  const { data, isLoading, error } = useQuery({
    queryKey: ["site-network-map", siteId],
    queryFn: () => api.get<Topology>(`/sites/${siteId}/network-map`),
    enabled: !!siteId,
    refetchInterval: 30_000,
  });

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h2 className="text-2xl font-semibold tracking-tight">Site network map</h2>
          <p className="mt-0.5 text-xs text-slate-500">
            Topology limited to appliances in this site (same graph engine as the global view).
          </p>
        </div>
        <Link
          to="/sites"
          className="rounded-full border border-ink-700 bg-ink-900 px-3 py-1.5 text-xs text-slate-300 hover:bg-ink-800"
        >
          ← Sites
        </Link>
      </div>

      {isLoading && <p className="text-sm text-slate-500">Loading map…</p>}
      {error && (
        <div className="rounded-md border border-red-900/60 bg-red-950/30 px-3 py-2 text-sm text-red-300">
          Could not load network map for this site.
        </div>
      )}
      {data && <TopologyGraph data={data} />}
    </div>
  );
}

// DevicesData — DEX historical index explorer (scores, health, patches, …).

import { useQuery } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { api } from "../../api/client";
import type { Site } from "../../api/types";
import { EmptyHint, ErrorHint } from "./common";

type IndexMeta = { name: string; description: string; rowEstimate: number };
type QueryResult = {
  index: string;
  page: number;
  size: number;
  columns: string[];
  rows: Record<string, unknown>[];
};
type DeviceGroup = { id: string; name: string; siteId: string };

export default function DevicesData() {
  const sites = useQuery({ queryKey: ["sites"], queryFn: () => api.get<Site[]>("/sites") });
  const indices = useQuery({
    queryKey: ["agents-data-indices"],
    queryFn: () => api.get<IndexMeta[]>("/agents/data"),
  });
  const groups = useQuery({
    queryKey: ["device-groups"],
    queryFn: () => api.get<DeviceGroup[]>("/device-groups"),
  });

  const [index, setIndex] = useState("scores");
  const [siteId, setSiteId] = useState("");
  const [groupId, setGroupId] = useState("");
  const [sinceHours, setSinceHours] = useState("24");

  const since = useMemo(() => {
    const h = parseInt(sinceHours, 10) || 24;
    return new Date(Date.now() - h * 3600_000).toISOString();
  }, [sinceHours]);

  const qs = useMemo(() => {
    const p = new URLSearchParams({ size: "100", since });
    if (siteId) p.set("siteId", siteId);
    if (groupId) p.set("groupId", groupId);
    return p.toString();
  }, [siteId, groupId, since]);

  const data = useQuery({
    queryKey: ["agents-data", index, qs],
    queryFn: () => api.get<QueryResult>(`/agents/data/${index}?${qs}`),
    enabled: !!index,
  });

  if (indices.isLoading) return <EmptyHint>Loading indices…</EmptyHint>;
  if (indices.isError) return <ErrorHint>Failed to load data indices.</ErrorHint>;

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-end gap-3">
        <label className="text-xs text-slate-400">
          Index
          <select
            className="mt-1 block rounded border border-ink-700 bg-ink-900 px-2 py-1.5 text-sm text-slate-100"
            value={index}
            onChange={(e) => setIndex(e.target.value)}
          >
            {(indices.data ?? []).map((i) => (
              <option key={i.name} value={i.name}>
                {i.name} — {i.description}
              </option>
            ))}
          </select>
        </label>
        <label className="text-xs text-slate-400">
          Site
          <select
            className="mt-1 block rounded border border-ink-700 bg-ink-900 px-2 py-1.5 text-sm text-slate-100"
            value={siteId}
            onChange={(e) => setSiteId(e.target.value)}
          >
            <option value="">All sites</option>
            {(sites.data ?? []).map((s) => (
              <option key={s.id} value={s.id}>
                {s.name}
              </option>
            ))}
          </select>
        </label>
        <label className="text-xs text-slate-400">
          Group
          <select
            className="mt-1 block rounded border border-ink-700 bg-ink-900 px-2 py-1.5 text-sm text-slate-100"
            value={groupId}
            onChange={(e) => setGroupId(e.target.value)}
          >
            <option value="">All groups</option>
            {(groups.data ?? []).map((g) => (
              <option key={g.id} value={g.id}>
                {g.name}
              </option>
            ))}
          </select>
        </label>
        <label className="text-xs text-slate-400">
          Since (hours)
          <input
            className="mt-1 block w-24 rounded border border-ink-700 bg-ink-900 px-2 py-1.5 text-sm text-slate-100"
            value={sinceHours}
            onChange={(e) => setSinceHours(e.target.value)}
          />
        </label>
        <a
          className="rounded bg-ink-800 px-3 py-1.5 text-sm text-sonar-300 hover:bg-ink-700"
          href={`/api/v1/agents/data/${index}?${qs}&export=1`}
        >
          CSV export
        </a>
      </div>

      {data.isLoading && <EmptyHint>Querying…</EmptyHint>}
      {data.isError && <ErrorHint>Query failed.</ErrorHint>}
      {data.data && (
        <div className="overflow-auto rounded border border-ink-800">
          <table className="min-w-full text-left text-xs">
            <thead className="bg-ink-900 text-slate-400">
              <tr>
                {(data.data.columns ?? []).map((c) => (
                  <th key={c} className="px-2 py-2 font-medium">
                    {c}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {(data.data.rows ?? []).map((row, i) => (
                <tr key={i} className="border-t border-ink-800/80 text-slate-200">
                  {(data.data.columns ?? []).map((c) => (
                    <td key={c} className="max-w-[14rem] truncate px-2 py-1.5 font-mono">
                      {row[c] == null ? "—" : String(row[c])}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
          {(data.data.rows ?? []).length === 0 && (
            <p className="p-4 text-sm text-slate-500">No rows for this window.</p>
          )}
        </div>
      )}
    </div>
  );
}

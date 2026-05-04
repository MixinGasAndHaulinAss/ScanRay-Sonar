// Devices — the canonical host-list table that used to live as the
// entire body of pages/Agents.tsx. Extracted unchanged so the new
// Overview dropdown can swap views without disturbing the table's
// existing behavior (tag filter, sticky-search, per-row metrics).
//
// All the bits that aren't "host table" — enrollment tokens, the Add
// Agent dialog — moved to pages/agents/Enrollment.tsx; this file is
// pure list now.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { api } from "../../api/client";
import type { Agent, Site } from "../../api/types";
import TagFilter from "../../components/TagFilter";
import { formatBytes, formatPct, formatRelative, pctBarColor } from "../../lib/format";

const TAG_FILTER_KEY = "sonar.agents.tagFilter";
function loadTagFilter(): string[] {
  try {
    const raw = localStorage.getItem(TAG_FILTER_KEY);
    if (!raw) return [];
    const parsed = JSON.parse(raw);
    return Array.isArray(parsed) ? parsed.filter((t) => typeof t === "string") : [];
  } catch {
    return [];
  }
}
function saveTagFilter(tags: string[]) {
  try {
    localStorage.setItem(TAG_FILTER_KEY, JSON.stringify(tags));
  } catch {
    /* localStorage may be disabled */
  }
}

export default function Devices() {
  const qc = useQueryClient();
  const agents = useQuery({
    queryKey: ["agents"],
    queryFn: () => api.get<Agent[]>("/agents"),
    refetchInterval: 30_000,
  });
  const sites = useQuery({ queryKey: ["sites"], queryFn: () => api.get<Site[]>("/sites") });

  const [tagFilter, setTagFilter] = useState<string[]>(loadTagFilter);
  useEffect(() => saveTagFilter(tagFilter), [tagFilter]);
  const [search, setSearch] = useState("");

  const allTags = useMemo(() => {
    const set = new Set<string>();
    agents.data?.forEach((a) => a.tags?.forEach((t) => set.add(t)));
    return Array.from(set).sort();
  }, [agents.data]);

  const visibleAgents = useMemo(() => {
    const list = agents.data ?? [];
    const q = search.trim().toLowerCase();
    return list.filter((a) => {
      if (tagFilter.length > 0) {
        const tags = new Set(a.tags ?? []);
        for (const t of tagFilter) if (!tags.has(t)) return false;
      }
      if (q) {
        const hay = `${a.hostname} ${a.os} ${a.osVersion} ${a.primaryIp ?? ""} ${a.publicIp ?? ""} ${(a.tags ?? []).join(" ")}`;
        if (!hay.toLowerCase().includes(q)) return false;
      }
      return true;
    });
  }, [agents.data, tagFilter, search]);

  const toggleTagFilter = (tag: string) =>
    setTagFilter((prev) => (prev.includes(tag) ? prev.filter((t) => t !== tag) : [...prev, tag]));

  const delAgent = useMutation({
    mutationFn: (id: string) => api.del<void>(`/agents/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["agents"] }),
  });

  const siteName = (id: string) => sites.data?.find((s) => s.id === id)?.name ?? id.slice(0, 8);

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <h3 className="text-sm font-semibold uppercase tracking-wide text-slate-400">Hosts</h3>
        <div className="flex flex-wrap items-center gap-2">
          <input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Search hostname / IP / tag…"
            className="h-8 rounded-md border border-ink-700 bg-ink-950 px-2 text-xs text-slate-100 placeholder:text-slate-600"
          />
          <TagFilter
            availableTags={allTags}
            selected={tagFilter}
            onChange={setTagFilter}
            mode="and"
          />
          <span className="text-xs tabular-nums text-slate-500">
            {visibleAgents.length} / {agents.data?.length ?? 0}
          </span>
        </div>
      </div>
      <div className="overflow-hidden rounded-xl border border-ink-800 bg-ink-900">
        <table className="w-full text-left text-sm">
          <thead className="bg-ink-800/60 text-xs uppercase tracking-wide text-slate-400">
            <tr>
              <th className="px-4 py-2">Hostname</th>
              <th className="px-4 py-2">Site</th>
              <th className="px-4 py-2">Tags</th>
              <th className="px-4 py-2">OS</th>
              <th className="px-4 py-2">CPU</th>
              <th className="px-4 py-2">Memory</th>
              <th className="px-4 py-2">Disk</th>
              <th className="px-4 py-2">IP</th>
              <th className="px-4 py-2">Last seen</th>
              <th className="px-4 py-2">Status</th>
              <th className="px-4 py-2 text-right">Actions</th>
            </tr>
          </thead>
          <tbody>
            {agents.isLoading && (
              <tr>
                <td colSpan={11} className="px-4 py-6 text-center text-slate-500">
                  Loading…
                </td>
              </tr>
            )}
            {!agents.isLoading && visibleAgents.length === 0 && (
              <tr>
                <td colSpan={11} className="px-4 py-6 text-center text-slate-500">
                  {agents.data?.length === 0
                    ? "No agents enrolled yet. Switch the View dropdown to “Enrollment tokens” to issue an install one-liner."
                    : "No agents match the current filter."}
                </td>
              </tr>
            )}
            {visibleAgents.map((a) => {
              const online =
                a.lastSeenAt && Date.now() - new Date(a.lastSeenAt).getTime() < 5 * 60_000;
              const memPct =
                a.memUsedBytes != null && a.memTotalBytes && a.memTotalBytes > 0
                  ? (Number(a.memUsedBytes) / Number(a.memTotalBytes)) * 100
                  : null;
              const diskPct =
                a.rootDiskUsedBytes != null &&
                a.rootDiskTotalBytes &&
                a.rootDiskTotalBytes > 0
                  ? (Number(a.rootDiskUsedBytes) / Number(a.rootDiskTotalBytes)) * 100
                  : null;
              return (
                <tr key={a.id} className="border-t border-ink-800 hover:bg-ink-800/30">
                  <td className="px-4 py-2">
                    <Link
                      to={`/agents/${a.id}`}
                      className="font-medium text-sonar-300 hover:underline"
                    >
                      {a.hostname}
                    </Link>
                    {a.pendingReboot && (
                      <span
                        title="Reboot pending"
                        className="ml-2 rounded bg-amber-900/50 px-1.5 py-0.5 text-[10px] text-amber-300"
                      >
                        reboot
                      </span>
                    )}
                  </td>
                  <td className="px-4 py-2 text-slate-400">{siteName(a.siteId)}</td>
                  <td className="px-4 py-2">
                    {a.tags && a.tags.length > 0 ? (
                      <div className="flex max-w-[14rem] flex-wrap gap-1">
                        {a.tags.map((t) => (
                          <button
                            key={t}
                            onClick={(e) => {
                              e.preventDefault();
                              toggleTagFilter(t);
                            }}
                            className={
                              "rounded-full border px-1.5 py-0.5 text-[10px] " +
                              (tagFilter.includes(t)
                                ? "border-sonar-500 bg-sonar-700/40 text-sonar-100"
                                : "border-ink-700 bg-ink-950/40 text-slate-400 hover:border-sonar-700 hover:text-sonar-200")
                            }
                            title={`Filter by ${t}`}
                          >
                            {t}
                          </button>
                        ))}
                      </div>
                    ) : (
                      <span className="text-[10px] text-slate-600">—</span>
                    )}
                  </td>
                  <td className="px-4 py-2 text-slate-400">
                    <div>
                      {a.os} {a.osVersion}
                    </div>
                    <div className="text-[10px] text-slate-600">
                      v{a.agentVersion || "?"}
                    </div>
                  </td>
                  <td className="px-4 py-2">
                    <MetricCell pct={a.cpuPct ?? null} />
                  </td>
                  <td className="px-4 py-2">
                    <MetricCell
                      pct={memPct}
                      sub={a.memTotalBytes ? formatBytes(Number(a.memTotalBytes)) : ""}
                    />
                  </td>
                  <td className="px-4 py-2">
                    <MetricCell
                      pct={diskPct}
                      sub={
                        a.rootDiskTotalBytes ? formatBytes(Number(a.rootDiskTotalBytes)) : ""
                      }
                    />
                  </td>
                  <td className="px-4 py-2 font-mono text-xs text-slate-400">
                    {a.primaryIp || "—"}
                  </td>
                  <td className="px-4 py-2 text-xs text-slate-500" title={a.lastSeenAt ?? ""}>
                    {formatRelative(a.lastSeenAt)}
                  </td>
                  <td className="px-4 py-2">
                    <span
                      className={
                        online
                          ? "rounded bg-emerald-900/40 px-2 py-0.5 text-xs text-emerald-300"
                          : "rounded bg-slate-800 px-2 py-0.5 text-xs text-slate-400"
                      }
                    >
                      {online ? "online" : "offline"}
                    </span>
                  </td>
                  <td className="px-4 py-2 text-right">
                    <button
                      onClick={() => {
                        if (confirm(`Remove agent "${a.hostname}"?`)) delAgent.mutate(a.id);
                      }}
                      className="rounded-md border border-ink-700 px-2 py-1 text-xs text-red-300 hover:bg-red-900/30"
                    >
                      Remove
                    </button>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function MetricCell({ pct, sub }: { pct: number | null; sub?: string }) {
  if (pct == null) return <span className="text-xs text-slate-600">—</span>;
  const clamped = Math.min(100, Math.max(0, pct));
  return (
    <div className="min-w-[80px] space-y-1">
      <div className="text-xs tabular-nums text-slate-200">{formatPct(pct)}</div>
      <div className="h-1 w-20 overflow-hidden rounded bg-ink-800">
        <div
          className={"h-full " + pctBarColor(clamped)}
          style={{ width: `${clamped}%` }}
        />
      </div>
      {sub && <div className="text-[10px] text-slate-600">{sub}</div>}
    </div>
  );
}

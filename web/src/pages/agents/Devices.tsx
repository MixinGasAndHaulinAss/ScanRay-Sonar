// Devices — ControlUp-style Details fleet grid: dense live table with
// search, tags, sort, group-by site, heat bars, CSV export.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Fragment, useEffect, useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { api } from "../../api/client";
import type { Agent, Site } from "../../api/types";
import TagFilter from "../../components/TagFilter";
import { formatBytes, formatPct, formatRelative, pctBarColor } from "../../lib/format";

const TAG_FILTER_KEY = "sonar.agents.tagFilter";
const GROUP_KEY = "sonar.devices.groupBy";
const SORT_KEY = "sonar.devices.sort";

type SortKey = "hostname" | "cpu" | "mem" | "disk" | "lastSeen" | "status";
type SortDir = "asc" | "desc";

function loadTagFilter(): string[] {
  try {
    const raw = localStorage.getItem(TAG_FILTER_KEY);
    if (!raw) return [];
    const parsed = JSON.parse(raw);
    return Array.isArray(parsed) ? parsed.filter((t: unknown) => typeof t === "string") : [];
  } catch {
    return [];
  }
}
function saveTagFilter(tags: string[]) {
  try {
    localStorage.setItem(TAG_FILTER_KEY, JSON.stringify(tags));
  } catch {
    /* ignore */
  }
}

function isOnline(a: Agent): boolean {
  return !!(a.lastSeenAt && Date.now() - new Date(a.lastSeenAt).getTime() < 5 * 60_000);
}

function memPctOf(a: Agent): number | null {
  if (a.memUsedBytes == null || !a.memTotalBytes || a.memTotalBytes <= 0) return null;
  return (Number(a.memUsedBytes) / Number(a.memTotalBytes)) * 100;
}

function diskPctOf(a: Agent): number | null {
  if (a.rootDiskUsedBytes == null || !a.rootDiskTotalBytes || a.rootDiskTotalBytes <= 0) return null;
  return (Number(a.rootDiskUsedBytes) / Number(a.rootDiskTotalBytes)) * 100;
}

export default function Devices() {
  const qc = useQueryClient();
  const agents = useQuery({
    queryKey: ["agents"],
    queryFn: () => api.get<Agent[]>("/agents"),
    refetchInterval: 15_000,
  });
  const sites = useQuery({ queryKey: ["sites"], queryFn: () => api.get<Site[]>("/sites") });

  const [tagFilter, setTagFilter] = useState<string[]>(loadTagFilter);
  useEffect(() => saveTagFilter(tagFilter), [tagFilter]);
  const [search, setSearch] = useState("");
  const [groupBySite, setGroupBySite] = useState(() => {
    try {
      return localStorage.getItem(GROUP_KEY) === "site";
    } catch {
      return false;
    }
  });
  const [sort, setSort] = useState<{ key: SortKey; dir: SortDir }>(() => {
    try {
      const raw = localStorage.getItem(SORT_KEY);
      if (raw) return JSON.parse(raw) as { key: SortKey; dir: SortDir };
    } catch {
      /* ignore */
    }
    return { key: "hostname", dir: "asc" };
  });

  useEffect(() => {
    try {
      localStorage.setItem(GROUP_KEY, groupBySite ? "site" : "");
      localStorage.setItem(SORT_KEY, JSON.stringify(sort));
    } catch {
      /* ignore */
    }
  }, [groupBySite, sort]);

  const allTags = useMemo(() => {
    const set = new Set<string>();
    agents.data?.forEach((a) => a.tags?.forEach((t) => set.add(t)));
    return Array.from(set).sort();
  }, [agents.data]);

  const siteName = (id: string) => sites.data?.find((s) => s.id === id)?.name ?? id.slice(0, 8);

  const visibleAgents = useMemo(() => {
    const list = agents.data ?? [];
    const q = search.trim().toLowerCase();
    let filtered = list.filter((a) => {
      if (tagFilter.length > 0) {
        const tags = new Set(a.tags ?? []);
        for (const t of tagFilter) if (!tags.has(t)) return false;
      }
      if (q) {
        const hay = `${a.hostname} ${a.os} ${a.osVersion} ${a.primaryIp ?? ""} ${a.publicIp ?? ""} ${(a.tags ?? []).join(" ")} ${siteName(a.siteId)}`;
        if (!hay.toLowerCase().includes(q)) return false;
      }
      return true;
    });

    const dir = sort.dir === "asc" ? 1 : -1;
    filtered = [...filtered].sort((a, b) => {
      const cmp = (x: number | string | null, y: number | string | null) => {
        if (x == null && y == null) return 0;
        if (x == null) return 1;
        if (y == null) return -1;
        if (x < y) return -1 * dir;
        if (x > y) return 1 * dir;
        return 0;
      };
      switch (sort.key) {
        case "cpu":
          return cmp(a.cpuPct ?? null, b.cpuPct ?? null);
        case "mem":
          return cmp(memPctOf(a), memPctOf(b));
        case "disk":
          return cmp(diskPctOf(a), diskPctOf(b));
        case "lastSeen":
          return cmp(
            a.lastSeenAt ? new Date(a.lastSeenAt).getTime() : null,
            b.lastSeenAt ? new Date(b.lastSeenAt).getTime() : null,
          );
        case "status":
          return cmp(isOnline(a) ? 1 : 0, isOnline(b) ? 1 : 0);
        default:
          return cmp(a.hostname.toLowerCase(), b.hostname.toLowerCase());
      }
    });
    return filtered;
  }, [agents.data, tagFilter, search, sort, sites.data]);

  const groups = useMemo(() => {
    if (!groupBySite) return [{ key: "", label: "", rows: visibleAgents }];
    const map = new Map<string, Agent[]>();
    for (const a of visibleAgents) {
      const k = a.siteId;
      if (!map.has(k)) map.set(k, []);
      map.get(k)!.push(a);
    }
    return Array.from(map.entries())
      .sort((a, b) => siteName(a[0]).localeCompare(siteName(b[0])))
      .map(([key, rows]) => ({ key, label: siteName(key), rows }));
  }, [visibleAgents, groupBySite, sites.data]);

  const toggleTagFilter = (tag: string) =>
    setTagFilter((prev) => (prev.includes(tag) ? prev.filter((t) => t !== tag) : [...prev, tag]));

  const toggleSort = (key: SortKey) => {
    setSort((prev) =>
      prev.key === key ? { key, dir: prev.dir === "asc" ? "desc" : "asc" } : { key, dir: "asc" },
    );
  };

  const delAgent = useMutation({
    mutationFn: (id: string) => api.del<void>(`/agents/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["agents"] }),
  });

  const exportCsv = () => {
    const headers = [
      "hostname",
      "site",
      "os",
      "agentVersion",
      "cpuPct",
      "memPct",
      "diskPct",
      "primaryIp",
      "publicIp",
      "lastSeenAt",
      "online",
      "tags",
    ];
    const lines = [headers.join(",")];
    for (const a of visibleAgents) {
      const row = [
        a.hostname,
        siteName(a.siteId),
        `${a.os} ${a.osVersion}`,
        a.agentVersion ?? "",
        a.cpuPct ?? "",
        memPctOf(a)?.toFixed(1) ?? "",
        diskPctOf(a)?.toFixed(1) ?? "",
        a.primaryIp ?? "",
        a.publicIp ?? "",
        a.lastSeenAt ?? "",
        isOnline(a) ? "online" : "offline",
        (a.tags ?? []).join("|"),
      ].map((c) => `"${String(c).replace(/"/g, '""')}"`);
      lines.push(row.join(","));
    }
    const blob = new Blob([lines.join("\n")], { type: "text/csv;charset=utf-8" });
    const url = URL.createObjectURL(blob);
    const el = document.createElement("a");
    el.href = url;
    el.download = `sonar-devices-${new Date().toISOString().slice(0, 10)}.csv`;
    el.click();
    URL.revokeObjectURL(url);
  };

  const SortTh = ({ k, children }: { k: SortKey; children: React.ReactNode }) => (
    <th className="cursor-pointer select-none px-3 py-2 hover:text-slate-200" onClick={() => toggleSort(k)}>
      <span className="inline-flex items-center gap-1">
        {children}
        {sort.key === k && <span className="text-sonar-400">{sort.dir === "asc" ? "↑" : "↓"}</span>}
      </span>
    </th>
  );

  return (
    <div className="space-y-3">
      <div className="flex flex-wrap items-end justify-between gap-3">
        <div className="flex flex-wrap items-center gap-2">
          <input
            value={search}
            onChange={(e) => setSearch(e.target.value)}
            placeholder="Search hostname / IP / tag / site…"
            className="h-8 w-64 rounded-md border border-ink-700 bg-ink-950 px-2 text-xs text-slate-100 placeholder:text-slate-600"
          />
          <TagFilter
            availableTags={allTags}
            selected={tagFilter}
            onChange={setTagFilter}
            mode="and"
          />
          <label className="flex items-center gap-1.5 text-xs text-slate-400">
            <input
              type="checkbox"
              checked={groupBySite}
              onChange={(e) => setGroupBySite(e.target.checked)}
              className="rounded border-ink-600"
            />
            Group by site
          </label>
        </div>
        <div className="flex items-center gap-2">
          <span className="text-xs tabular-nums text-slate-500">
            {visibleAgents.length} / {agents.data?.length ?? 0}
            {" · "}
            {visibleAgents.filter(isOnline).length} online
          </span>
          <button
            type="button"
            onClick={exportCsv}
            className="h-8 rounded-md border border-ink-700 px-2 text-xs text-slate-300 hover:bg-ink-800"
          >
            Export CSV
          </button>
        </div>
      </div>

      <div className="overflow-auto rounded-xl border border-ink-800 bg-ink-900">
        <table className="w-full min-w-[1100px] text-left text-sm">
          <thead className="sticky top-0 z-10 bg-ink-800 text-xs uppercase tracking-wide text-slate-400">
            <tr>
              <SortTh k="hostname">Hostname</SortTh>
              <th className="px-3 py-2">Site</th>
              <th className="px-3 py-2">Tags</th>
              <th className="px-3 py-2">OS</th>
              <SortTh k="cpu">CPU</SortTh>
              <SortTh k="mem">Memory</SortTh>
              <SortTh k="disk">Disk</SortTh>
              <th className="px-3 py-2">IP</th>
              <SortTh k="lastSeen">Last seen</SortTh>
              <SortTh k="status">Status</SortTh>
              <th className="px-3 py-2 text-right">Actions</th>
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
                    ? "No agents enrolled yet. Open Enrollment tokens to issue an install one-liner."
                    : "No agents match the current filter."}
                </td>
              </tr>
            )}
            {groups.map((g) => (
              <Fragment key={g.key || "all"}>
                {groupBySite && (
                  <tr className="bg-ink-950/80">
                    <td colSpan={11} className="px-3 py-1.5 text-xs font-semibold text-slate-300">
                      {g.label}{" "}
                      <span className="font-normal text-slate-500">({g.rows.length})</span>
                    </td>
                  </tr>
                )}
                {g.rows.map((a) => {
                  const online = isOnline(a);
                  const memPct = memPctOf(a);
                  const diskPct = diskPctOf(a);
                  return (
                    <tr key={a.id} className="border-t border-ink-800/80 hover:bg-ink-800/30 even:bg-ink-950/20">
                      <td className="px-3 py-2">
                        <Link
                          to={`/agents/${a.id}`}
                          className="font-medium text-sonar-300 hover:underline"
                        >
                          {a.hostname}
                        </Link>
                        {a.pendingReboot && (
                          <span className="ml-2 rounded bg-amber-900/50 px-1.5 py-0.5 text-[10px] text-amber-300">
                            reboot
                          </span>
                        )}
                      </td>
                      <td className="px-3 py-2 text-slate-400">{siteName(a.siteId)}</td>
                      <td className="px-3 py-2">
                        {a.tags && a.tags.length > 0 ? (
                          <div className="flex max-w-[12rem] flex-wrap gap-1">
                            {a.tags.map((t) => (
                              <button
                                key={t}
                                type="button"
                                onClick={(e) => {
                                  e.preventDefault();
                                  toggleTagFilter(t);
                                }}
                                className={
                                  "rounded-full border px-1.5 py-0.5 text-[10px] " +
                                  (tagFilter.includes(t)
                                    ? "border-sonar-500 bg-sonar-700/40 text-sonar-100"
                                    : "border-ink-700 bg-ink-950/40 text-slate-400 hover:border-sonar-700")
                                }
                              >
                                {t}
                              </button>
                            ))}
                          </div>
                        ) : (
                          <span className="text-[10px] text-slate-600">—</span>
                        )}
                      </td>
                      <td className="px-3 py-2 text-slate-400">
                        <div>
                          {a.os} {a.osVersion}
                        </div>
                        <div className="text-[10px] text-slate-600">v{a.agentVersion || "?"}</div>
                      </td>
                      <td className="px-3 py-2">
                        <MetricCell pct={a.cpuPct ?? null} />
                      </td>
                      <td className="px-3 py-2">
                        <MetricCell
                          pct={memPct}
                          sub={a.memTotalBytes ? formatBytes(Number(a.memTotalBytes)) : ""}
                        />
                      </td>
                      <td className="px-3 py-2">
                        <MetricCell
                          pct={diskPct}
                          sub={
                            a.rootDiskTotalBytes
                              ? formatBytes(Number(a.rootDiskTotalBytes))
                              : ""
                          }
                        />
                      </td>
                      <td className="px-3 py-2 font-mono text-xs text-slate-400">
                        {a.primaryIp || "—"}
                      </td>
                      <td className="px-3 py-2 text-xs text-slate-500" title={a.lastSeenAt ?? ""}>
                        {formatRelative(a.lastSeenAt)}
                      </td>
                      <td className="px-3 py-2">
                        <span
                          className={
                            online
                              ? "inline-flex items-center gap-1 rounded-full bg-emerald-900/40 px-2 py-0.5 text-xs text-emerald-300"
                              : "rounded-full bg-slate-800 px-2 py-0.5 text-xs text-slate-400"
                          }
                        >
                          {online && (
                            <span className="h-1.5 w-1.5 rounded-full bg-emerald-400" />
                          )}
                          {online ? "Connected" : "Offline"}
                        </span>
                      </td>
                      <td className="px-3 py-2 text-right">
                        <button
                          type="button"
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
              </Fragment>
            ))}
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
    <div className="min-w-[72px] space-y-1">
      <div className="text-xs tabular-nums text-slate-200">{formatPct(pct)}</div>
      <div className="h-1 w-16 overflow-hidden rounded bg-ink-800">
        <div className={"h-full " + pctBarColor(clamped)} style={{ width: `${clamped}%` }} />
      </div>
      {sub && <div className="text-[10px] text-slate-600">{sub}</div>}
    </div>
  );
}

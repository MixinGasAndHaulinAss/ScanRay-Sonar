// DevicesEvents — agent system-events timeline.

import { useQuery } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { Link } from "react-router-dom";
import { api } from "../../api/client";
import type { Site } from "../../api/types";
import { formatRelative } from "../../lib/format";
import { EmptyHint, ErrorHint } from "./common";

type SysEvent = {
  id: number;
  time: string;
  siteId: string;
  agentId?: string;
  kind: string;
  severity: string;
  title: string;
  body?: string;
};

const KINDS = [
  "",
  "alarm.opened",
  "alarm.cleared",
  "alarm.acked",
  "group.changed",
  "agent.offline",
  "agent.online",
  "compliance.changed",
  "config.changed",
];

export default function DevicesEvents() {
  const sites = useQuery({ queryKey: ["sites"], queryFn: () => api.get<Site[]>("/sites") });
  const [siteId, setSiteId] = useState("");
  const [kind, setKind] = useState("");
  const [severity, setSeverity] = useState("");

  const qs = useMemo(() => {
    const p = new URLSearchParams({ limit: "150" });
    if (siteId) p.set("siteId", siteId);
    if (kind) p.set("kind", kind);
    if (severity) p.set("severity", severity);
    return p.toString();
  }, [siteId, kind, severity]);

  const events = useQuery({
    queryKey: ["agents-events", qs],
    queryFn: () => api.get<SysEvent[]>(`/agents/events?${qs}`),
    refetchInterval: 15_000,
  });

  if (events.isLoading) return <EmptyHint>Loading events…</EmptyHint>;
  if (events.isError) return <ErrorHint>Failed to load events.</ErrorHint>;

  const rows = events.data ?? [];

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap gap-3">
        <select
          className="rounded border border-ink-700 bg-ink-900 px-2 py-1.5 text-sm text-slate-100"
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
        <select
          className="rounded border border-ink-700 bg-ink-900 px-2 py-1.5 text-sm text-slate-100"
          value={kind}
          onChange={(e) => setKind(e.target.value)}
        >
          {KINDS.map((k) => (
            <option key={k || "all"} value={k}>
              {k || "All kinds"}
            </option>
          ))}
        </select>
        <select
          className="rounded border border-ink-700 bg-ink-900 px-2 py-1.5 text-sm text-slate-100"
          value={severity}
          onChange={(e) => setSeverity(e.target.value)}
        >
          <option value="">All severities</option>
          <option value="critical">critical</option>
          <option value="warning">warning</option>
          <option value="info">info</option>
        </select>
      </div>

      <ol className="space-y-2">
        {rows.map((e) => (
          <li
            key={`${e.id}-${e.time}`}
            className="rounded border border-ink-800 bg-ink-950/40 px-3 py-2 text-sm"
          >
            <div className="flex flex-wrap items-baseline justify-between gap-2">
              <div className="flex flex-wrap items-center gap-2">
                <span className="rounded bg-ink-800 px-1.5 py-0.5 font-mono text-[10px] uppercase text-slate-400">
                  {e.kind}
                </span>
                <span className="text-slate-100">{e.title}</span>
                {e.agentId && (
                  <Link className="text-xs text-sonar-400 hover:underline" to={`/agents/${e.agentId}`}>
                    agent
                  </Link>
                )}
              </div>
              <span className="text-xs text-slate-500">{formatRelative(e.time)}</span>
            </div>
            {e.body ? <p className="mt-1 text-xs text-slate-400">{e.body}</p> : null}
          </li>
        ))}
      </ol>
      {rows.length === 0 && <EmptyHint>No system events yet.</EmptyHint>}
    </div>
  );
}

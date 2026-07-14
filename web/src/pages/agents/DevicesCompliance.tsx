// DevicesCompliance — fleet compliance posture.

import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { Link } from "react-router-dom";
import { api } from "../../api/client";
import type { Site } from "../../api/types";
import { EmptyHint, ErrorHint } from "./common";

type Summary = {
  agentCount: number;
  avgScore: number;
  openIssues: number;
  openCves: number;
  pendingRebootCount: number;
  agents: {
    id: string;
    hostname: string;
    complianceScore: number;
    complianceSeverity: string;
    issuesCount: number;
    pendingReboot: boolean;
  }[];
};

export default function DevicesCompliance() {
  const sites = useQuery({ queryKey: ["sites"], queryFn: () => api.get<Site[]>("/sites") });
  const [siteId, setSiteId] = useState("");
  const qs = siteId ? `?siteId=${siteId}` : "";
  const summary = useQuery({
    queryKey: ["agents-compliance", siteId],
    queryFn: () => api.get<Summary>(`/agents/compliance${qs}`),
    refetchInterval: 30_000,
  });

  if (summary.isLoading) return <EmptyHint>Loading compliance…</EmptyHint>;
  if (summary.isError) return <ErrorHint>Failed to load compliance.</ErrorHint>;

  const s = summary.data!;

  return (
    <div className="space-y-4">
      <div className="flex flex-wrap items-center gap-3">
        <select
          className="rounded border border-ink-700 bg-ink-900 px-2 py-1.5 text-sm text-slate-100"
          value={siteId}
          onChange={(e) => setSiteId(e.target.value)}
        >
          <option value="">All sites</option>
          {(sites.data ?? []).map((site) => (
            <option key={site.id} value={site.id}>
              {site.name}
            </option>
          ))}
        </select>
        <div className="flex flex-wrap gap-2 text-sm">
          <Stat label="Agents" value={String(s.agentCount)} />
          <Stat label="Avg score" value={s.avgScore?.toFixed?.(1) ?? String(s.avgScore)} />
          <Stat label="Open issues" value={String(s.openIssues)} />
          <Stat label="Open CVEs" value={String(s.openCves)} />
          <Stat label="Pending reboot" value={String(s.pendingRebootCount)} />
        </div>
      </div>

      <div className="overflow-auto rounded border border-ink-800">
        <table className="min-w-full text-left text-sm">
          <thead className="bg-ink-900 text-xs uppercase text-slate-400">
            <tr>
              <th className="px-3 py-2">Host</th>
              <th className="px-3 py-2">Score</th>
              <th className="px-3 py-2">Severity</th>
              <th className="px-3 py-2">Issues</th>
              <th className="px-3 py-2">Reboot</th>
            </tr>
          </thead>
          <tbody>
            {(s.agents ?? []).map((a) => (
              <tr key={a.id} className="border-t border-ink-800/80">
                <td className="px-3 py-2">
                  <Link className="text-sonar-300 hover:underline" to={`/agents/${a.id}`}>
                    {a.hostname}
                  </Link>
                </td>
                <td className="px-3 py-2 font-mono">{a.complianceScore?.toFixed?.(1) ?? a.complianceScore}</td>
                <td className="px-3 py-2 text-slate-300">{a.complianceSeverity || "—"}</td>
                <td className="px-3 py-2">{a.issuesCount}</td>
                <td className="px-3 py-2">{a.pendingReboot ? "yes" : "—"}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function Stat({ label, value }: { label: string; value: string }) {
  return (
    <span className="rounded bg-ink-800 px-2 py-1 text-slate-300">
      {label}: <strong className="text-slate-100">{value}</strong>
    </span>
  );
}

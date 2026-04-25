import { useQuery } from "@tanstack/react-query";
import { api } from "../api/client";
import type { Agent, Appliance, Site } from "../api/types";

function StatCard({ label, value, hint }: { label: string; value: string | number; hint?: string }) {
  return (
    <div className="rounded-xl border border-ink-800 bg-ink-900 p-4 shadow-sm">
      <div className="text-xs uppercase tracking-wide text-slate-500">{label}</div>
      <div className="mt-1 text-2xl font-semibold text-white">{value}</div>
      {hint && <div className="mt-1 text-xs text-slate-400">{hint}</div>}
    </div>
  );
}

export default function Dashboard() {
  const sites = useQuery({ queryKey: ["sites"], queryFn: () => api.get<Site[]>("/sites") });
  const agents = useQuery({ queryKey: ["agents"], queryFn: () => api.get<Agent[]>("/agents") });
  const appliances = useQuery({ queryKey: ["appliances"], queryFn: () => api.get<Appliance[]>("/appliances") });

  const onlineAgents = agents.data?.filter((a) => a.lastSeenAt && Date.now() - new Date(a.lastSeenAt).getTime() < 5 * 60_000)
    .length ?? 0;

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-2xl font-semibold tracking-tight">Overview</h2>
        <p className="text-sm text-slate-400">
          Phase 1 baseline. Live telemetry, traffic visualization, and topology arrive in later phases.
        </p>
      </div>

      <div className="grid grid-cols-2 gap-4 md:grid-cols-4">
        <StatCard label="Sites" value={sites.data?.length ?? "—"} />
        <StatCard label="Agents" value={agents.data?.length ?? "—"} hint={`${onlineAgents} online (last 5m)`} />
        <StatCard label="Appliances" value={appliances.data?.length ?? "—"} />
        <StatCard label="Open alerts" value="0" hint="Phase 4" />
      </div>

      <div className="rounded-xl border border-ink-800 bg-ink-900 p-6">
        <h3 className="mb-2 text-sm font-medium text-slate-300">Welcome to ScanRay Sonar</h3>
        <p className="text-sm text-slate-400">
          This is the Phase 1 foundation: authentication, multi-site, encrypted secrets, and the OpenAPI surface.
          Add sites, then enroll agents and appliances. Real-time dashboards, topology, traffic visualization, and DNS
          health follow in Phases 2–5.
        </p>
      </div>
    </div>
  );
}

import { useQuery } from "@tanstack/react-query";
import { api } from "../api/client";
import type { Agent } from "../api/types";

export default function Agents() {
  const { data, isLoading } = useQuery({ queryKey: ["agents"], queryFn: () => api.get<Agent[]>("/agents") });

  return (
    <div className="space-y-4">
      <div>
        <h2 className="text-2xl font-semibold tracking-tight">Agents</h2>
        <p className="text-sm text-slate-400">Hosts running the Sonar Probe (Phase 2).</p>
      </div>
      <div className="overflow-hidden rounded-xl border border-ink-800 bg-ink-900">
        <table className="w-full text-left text-sm">
          <thead className="bg-ink-800/60 text-xs uppercase tracking-wide text-slate-400">
            <tr>
              <th className="px-4 py-2">Hostname</th>
              <th className="px-4 py-2">OS</th>
              <th className="px-4 py-2">Version</th>
              <th className="px-4 py-2">Last seen</th>
              <th className="px-4 py-2">Status</th>
            </tr>
          </thead>
          <tbody>
            {isLoading && (
              <tr>
                <td colSpan={5} className="px-4 py-6 text-center text-slate-500">
                  Loading…
                </td>
              </tr>
            )}
            {data?.length === 0 && (
              <tr>
                <td colSpan={5} className="px-4 py-6 text-center text-slate-500">
                  No agents enrolled yet. Phase 2 ships the Probe binary and enrollment flow.
                </td>
              </tr>
            )}
            {data?.map((a) => {
              const online = a.lastSeenAt && Date.now() - new Date(a.lastSeenAt).getTime() < 5 * 60_000;
              return (
                <tr key={a.id} className="border-t border-ink-800 hover:bg-ink-800/30">
                  <td className="px-4 py-2">{a.hostname}</td>
                  <td className="px-4 py-2 text-slate-400">
                    {a.os} {a.osVersion}
                  </td>
                  <td className="px-4 py-2 text-slate-400">{a.agentVersion || "—"}</td>
                  <td className="px-4 py-2 text-slate-500">
                    {a.lastSeenAt ? new Date(a.lastSeenAt).toLocaleString() : "never"}
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
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </div>
  );
}

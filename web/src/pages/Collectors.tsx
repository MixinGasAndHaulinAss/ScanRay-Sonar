import { useQuery } from "@tanstack/react-query";
import { api } from "../api/client";
import type { Site } from "../api/types";
import { formatRelative } from "../lib/format";

interface CollectorRow {
  id: string;
  siteId: string;
  name: string;
  hostname: string;
  collectorVersion: string;
  lastSeenAt?: string | null;
  isActive: boolean;
  createdAt: string;
}

export default function Collectors() {
  const sites = useQuery({ queryKey: ["sites"], queryFn: () => api.get<Site[]>("/sites") });
  const collectors = useQuery({
    queryKey: ["collectors"],
    queryFn: () => api.get<CollectorRow[]>("/collectors"),
  });

  const siteName = (id: string) => sites.data?.find((s) => s.id === id)?.name ?? id.slice(0, 8);

  return (
    <div className="space-y-4">
      <div>
        <h2 className="text-2xl font-semibold tracking-tight">Collectors</h2>
        <p className="mt-0.5 text-xs text-slate-500">
          Remote SNMP/discovery agents enrolled per site. Enrollment tokens are issued under each site&apos;s admin flows.
        </p>
      </div>

      {collectors.isLoading && <p className="text-sm text-slate-500">Loading…</p>}
      {collectors.error && (
        <p className="text-sm text-red-400">Could not load collectors.</p>
      )}

      {collectors.data && (
        <div className="overflow-hidden rounded-xl border border-ink-800 bg-ink-900 shadow-sm">
          <table className="w-full text-left text-sm">
            <thead className="bg-ink-800/60 text-xs uppercase tracking-wide text-slate-400">
              <tr>
                <th className="px-4 py-2">Name</th>
                <th className="px-4 py-2">Site</th>
                <th className="px-4 py-2">Hostname</th>
                <th className="px-4 py-2">Version</th>
                <th className="px-4 py-2">Last seen</th>
                <th className="px-4 py-2">Active</th>
              </tr>
            </thead>
            <tbody>
              {collectors.data.map((c) => (
                <tr key={c.id} className="border-t border-ink-800 hover:bg-ink-800/30">
                  <td className="px-4 py-2 font-medium text-slate-100">{c.name}</td>
                  <td className="px-4 py-2 text-slate-400">{siteName(c.siteId)}</td>
                  <td className="px-4 py-2 font-mono text-xs text-slate-400">{c.hostname}</td>
                  <td className="px-4 py-2 text-slate-400">{c.collectorVersion || "—"}</td>
                  <td className="px-4 py-2 text-slate-500">
                    {c.lastSeenAt ? formatRelative(c.lastSeenAt) : "never"}
                  </td>
                  <td className="px-4 py-2">
                    <span
                      className={
                        c.isActive
                          ? "rounded-full bg-emerald-950/50 px-2 py-0.5 text-[10px] uppercase text-emerald-300"
                          : "rounded-full bg-slate-800 px-2 py-0.5 text-[10px] uppercase text-slate-500"
                      }
                    >
                      {c.isActive ? "yes" : "no"}
                    </span>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          {collectors.data.length === 0 && (
            <div className="px-4 py-8 text-center text-sm text-slate-500">No collectors enrolled yet.</div>
          )}
        </div>
      )}
    </div>
  );
}

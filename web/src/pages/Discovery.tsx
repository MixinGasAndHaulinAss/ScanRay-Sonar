import { useQuery } from "@tanstack/react-query";
import { api } from "../api/client";
import type { Site, User } from "../api/types";
import { formatRelative } from "../lib/format";

export default function Discovery() {
  const me = useQuery({ queryKey: ["me"], queryFn: () => api.get<User>("/auth/me") });
  const sites = useQuery({ queryKey: ["sites"], queryFn: () => api.get<Site[]>("/sites") });
  const ok = me.data?.role === "superadmin";

  const devices = useQuery({
    queryKey: ["discovery-devices"],
    queryFn: () => api.get<Record<string, unknown>[]>("/discovery/devices"),
    enabled: !!me.data && ok,
  });

  const networks = useQuery({
    queryKey: ["discovery-networks"],
    queryFn: () => api.get<Record<string, unknown>[]>("/discovery/networks"),
    enabled: !!me.data && ok,
  });

  if (!me.data) return <p className="text-sm text-slate-500">Loading…</p>;
  if (!ok) {
    return (
      <div className="rounded-lg border border-ink-800 bg-ink-900 px-4 py-6 text-sm text-slate-400">
        Discovery aggregates are restricted to super administrators.
      </div>
    );
  }

  const siteLabel = (id: unknown) => {
    const s = typeof id === "string" ? sites.data?.find((x) => x.id === id) : undefined;
    return s?.name ?? String(id ?? "");
  };

  return (
    <div className="space-y-8">
      <div>
        <h2 className="text-2xl font-semibold tracking-tight">Discovery</h2>
        <p className="mt-0.5 text-xs text-slate-500">
          Devices learned by collectors per site, plus configured subnets per organization.
        </p>
      </div>

      <section className="space-y-2">
        <h3 className="text-sm font-semibold text-slate-200">Networks</h3>
        {networks.isLoading && <p className="text-xs text-slate-500">Loading networks…</p>}
        <div className="overflow-hidden rounded-xl border border-ink-800 bg-ink-900">
          <table className="w-full text-left text-sm">
            <thead className="bg-ink-800/60 text-xs uppercase text-slate-400">
              <tr>
                <th className="px-3 py-2">Site</th>
                <th className="px-3 py-2">Subnets</th>
                <th className="px-3 py-2">Interval</th>
                <th className="px-3 py-2">Devices</th>
                <th className="px-3 py-2">Last scan</th>
              </tr>
            </thead>
            <tbody>
              {networks.data?.map((row, i) => (
                <tr key={`${row.siteId}-${i}`} className="border-t border-ink-800">
                  <td className="px-3 py-2">{siteLabel(row.siteId)}</td>
                  <td className="max-w-lg px-3 py-2 font-mono text-[11px] text-slate-400">
                    {JSON.stringify(row.subnets)}
                  </td>
                  <td className="px-3 py-2 text-slate-400">{String(row.scanIntervalSeconds ?? "")}s</td>
                  <td className="px-3 py-2">{String(row.deviceCount ?? "")}</td>
                  <td className="px-3 py-2 text-slate-500">
                    {row.lastScanAt ? formatRelative(String(row.lastScanAt)) : "—"}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          {!networks.data?.length && !networks.isLoading && (
            <div className="px-4 py-6 text-center text-sm text-slate-500">No discovery networks.</div>
          )}
        </div>
      </section>

      <section className="space-y-2">
        <h3 className="text-sm font-semibold text-slate-200">Devices (recent)</h3>
        {devices.isLoading && <p className="text-xs text-slate-500">Loading devices…</p>}
        <div className="overflow-hidden rounded-xl border border-ink-800 bg-ink-900">
          <table className="w-full text-left text-sm">
            <thead className="bg-ink-800/60 text-xs uppercase text-slate-400">
              <tr>
                <th className="px-3 py-2">Site</th>
                <th className="px-3 py-2">IP</th>
                <th className="px-3 py-2">Hostname</th>
                <th className="px-3 py-2">Vendor</th>
                <th className="px-3 py-2">Protocols</th>
                <th className="px-3 py-2">Seen</th>
              </tr>
            </thead>
            <tbody>
              {devices.data?.map((d, i) => (
                <tr key={`${d.ip}-${i}`} className="border-t border-ink-800">
                  <td className="px-3 py-2">{String(d.siteName ?? siteLabel(d.siteId))}</td>
                  <td className="px-3 py-2 font-mono text-xs">{String(d.ip ?? "")}</td>
                  <td className="px-3 py-2">{String(d.hostname ?? "—")}</td>
                  <td className="px-3 py-2">{String(d.vendor ?? "—")}</td>
                  <td className="px-3 py-2 font-mono text-[11px] text-slate-400">
                    {Array.isArray(d.protocols) ? (d.protocols as string[]).join(", ") : "—"}
                  </td>
                  <td className="px-3 py-2 text-slate-500">
                    {d.lastSeenAt ? formatRelative(String(d.lastSeenAt)) : "—"}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          {!devices.data?.length && !devices.isLoading && (
            <div className="px-4 py-6 text-center text-sm text-slate-500">No discovered devices yet.</div>
          )}
        </div>
      </section>
    </div>
  );
}

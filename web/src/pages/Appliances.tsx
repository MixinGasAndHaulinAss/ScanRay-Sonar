import { useQuery } from "@tanstack/react-query";
import { api } from "../api/client";
import type { Appliance } from "../api/types";

export default function Appliances() {
  const { data, isLoading } = useQuery({ queryKey: ["appliances"], queryFn: () => api.get<Appliance[]>("/appliances") });

  return (
    <div className="space-y-4">
      <div>
        <h2 className="text-2xl font-semibold tracking-tight">Appliances</h2>
        <p className="text-sm text-slate-400">
          Switches, routers, and APs polled by the Sonar Poller (Phase 3 — SNMP v1/v2c/v3 + LLDP).
        </p>
      </div>
      <div className="overflow-hidden rounded-xl border border-ink-800 bg-ink-900">
        <table className="w-full text-left text-sm">
          <thead className="bg-ink-800/60 text-xs uppercase tracking-wide text-slate-400">
            <tr>
              <th className="px-4 py-2">Name</th>
              <th className="px-4 py-2">Vendor</th>
              <th className="px-4 py-2">Mgmt IP</th>
              <th className="px-4 py-2">SNMP</th>
              <th className="px-4 py-2">Poll</th>
              <th className="px-4 py-2">Last polled</th>
            </tr>
          </thead>
          <tbody>
            {isLoading && (
              <tr>
                <td colSpan={6} className="px-4 py-6 text-center text-slate-500">
                  Loading…
                </td>
              </tr>
            )}
            {data?.length === 0 && (
              <tr>
                <td colSpan={6} className="px-4 py-6 text-center text-slate-500">
                  No appliances yet. Add switches, APs, or routers — supports SNMP v1, v2c, and v3.
                </td>
              </tr>
            )}
            {data?.map((a) => (
              <tr key={a.id} className="border-t border-ink-800 hover:bg-ink-800/30">
                <td className="px-4 py-2">{a.name}</td>
                <td className="px-4 py-2 text-slate-400">{a.vendor}</td>
                <td className="px-4 py-2 font-mono text-slate-300">{a.mgmtIp}</td>
                <td className="px-4 py-2 text-slate-400">{a.snmpVersion}</td>
                <td className="px-4 py-2 text-slate-400">{a.pollIntervalSeconds}s</td>
                <td className="px-4 py-2 text-slate-500">
                  {a.lastPolledAt ? new Date(a.lastPolledAt).toLocaleString() : "never"}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

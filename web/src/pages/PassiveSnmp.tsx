// PassiveSnmp — read-only view of the per-site passive-SNMP inventory
// plus a change feed. Operators pick a site, the page renders two
// stacked tables: current devices (filterable by status) and the most
// recent ~500 added/retired/changed/reactivated events.

import { useMemo, useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { api } from "../api/client";
import type { Site } from "../api/types";
import { formatRelative } from "../lib/format";

interface PassiveDevice {
  ip: string;
  vendor: string;
  type: string;
  subType: string;
  sysDescr: string;
  sysObjectId: string;
  sysName: string;
  sysLocation: string;
  status: string;
  firstSeenAt: string;
  lastSeenAt: string;
  missCount: number;
}

interface PassiveChange {
  id: number;
  time: string;
  ip: string;
  kind: "added" | "retired" | "changed" | "reactivated";
  old?: Record<string, string>;
  new?: Record<string, string>;
}

const kindBadge: Record<PassiveChange["kind"], string> = {
  added: "bg-emerald-500/20 text-emerald-300",
  retired: "bg-slate-600/30 text-slate-300",
  changed: "bg-amber-500/20 text-amber-300",
  reactivated: "bg-sonar-500/20 text-sonar-200",
};

export default function PassiveSnmp() {
  const sites = useQuery({ queryKey: ["sites"], queryFn: () => api.get<Site[]>("/sites") });
  const [siteId, setSiteId] = useState<string>("");
  const [status, setStatus] = useState<"active" | "retired" | "all">("active");

  const devices = useQuery({
    queryKey: ["passive-snmp", siteId, status],
    queryFn: () =>
      api
        .get<{ devices: PassiveDevice[] }>(
          `/sites/${siteId}/passive-snmp?status=${status}`,
        )
        .then((r) => r.devices),
    enabled: !!siteId,
  });
  const changes = useQuery({
    queryKey: ["passive-snmp-changes", siteId],
    queryFn: () =>
      api
        .get<{ changes: PassiveChange[] }>(`/sites/${siteId}/passive-snmp/changes`)
        .then((r) => r.changes),
    enabled: !!siteId,
  });

  const counts = useMemo(() => {
    const c = { active: 0, retired: 0 };
    devices.data?.forEach((d) =>
      d.status === "retired" ? c.retired++ : c.active++,
    );
    return c;
  }, [devices.data]);

  return (
    <div className="space-y-6">
      <header>
        <h2 className="text-2xl font-semibold tracking-tight">Discovered devices</h2>
        <p className="mt-0.5 text-xs text-slate-500">
          Devices our collector hears upstream tooling polling on UDP/161, classified once
          per capture. Server merges new captures with the existing inventory and emits an
          add/retire/change feed below.
        </p>
      </header>

      <div className="flex flex-wrap items-end gap-3">
        <label className="space-y-1 text-xs">
          <span className="text-slate-400">Site</span>
          <select
            className="rounded-md border border-ink-700 bg-ink-950 px-2 py-1.5 text-sm"
            value={siteId}
            onChange={(e) => setSiteId(e.target.value)}
          >
            <option value="">Pick a site…</option>
            {sites.data?.map((s) => (
              <option key={s.id} value={s.id}>
                {s.name}
              </option>
            ))}
          </select>
        </label>
        {siteId && (
          <div className="flex gap-1 text-xs">
            {(["active", "retired", "all"] as const).map((k) => (
              <button
                key={k}
                onClick={() => setStatus(k)}
                className={
                  "rounded-full border px-3 py-1 " +
                  (status === k
                    ? "border-sonar-500 bg-sonar-500/15 text-sonar-200"
                    : "border-ink-700 text-slate-300 hover:bg-ink-800")
                }
              >
                {k} ({k === "active" ? counts.active : k === "retired" ? counts.retired : counts.active + counts.retired})
              </button>
            ))}
          </div>
        )}
      </div>

      {siteId && (
        <>
          <section className="space-y-2">
            <h3 className="text-sm font-semibold text-slate-200">Inventory</h3>
            <div className="overflow-auto rounded-xl border border-ink-800 bg-ink-900">
              <table className="w-full text-left text-sm">
                <thead className="bg-ink-800/60 text-xs uppercase text-slate-400">
                  <tr>
                    <th className="px-3 py-2">IP</th>
                    <th className="px-3 py-2">Vendor</th>
                    <th className="px-3 py-2">Type</th>
                    <th className="px-3 py-2">Sub-type</th>
                    <th className="px-3 py-2">sysName</th>
                    <th className="px-3 py-2">sysDescr</th>
                    <th className="px-3 py-2">First seen</th>
                    <th className="px-3 py-2">Last seen</th>
                    <th className="px-3 py-2">Status</th>
                  </tr>
                </thead>
                <tbody>
                  {devices.data?.map((d) => (
                    <tr key={d.ip} className="border-t border-ink-800 align-top">
                      <td className="px-3 py-2 font-mono text-xs">{d.ip}</td>
                      <td className="px-3 py-2">{d.vendor || "—"}</td>
                      <td className="px-3 py-2">{d.type || "—"}</td>
                      <td className="px-3 py-2 text-slate-400">{d.subType || ""}</td>
                      <td className="px-3 py-2 text-slate-300">{d.sysName || ""}</td>
                      <td className="px-3 py-2 max-w-[40ch] truncate text-slate-400" title={d.sysDescr}>
                        {d.sysDescr}
                      </td>
                      <td className="px-3 py-2 text-slate-500">
                        {formatRelative(d.firstSeenAt)}
                      </td>
                      <td className="px-3 py-2 text-slate-500">
                        {formatRelative(d.lastSeenAt)}
                      </td>
                      <td className="px-3 py-2">
                        <span
                          className={
                            "rounded-full px-2 py-0.5 text-xs " +
                            (d.status === "active"
                              ? "bg-emerald-500/20 text-emerald-300"
                              : "bg-slate-600/30 text-slate-300")
                          }
                        >
                          {d.status}
                        </span>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
              {!devices.data?.length && !devices.isLoading && (
                <div className="px-4 py-6 text-center text-sm text-slate-500">
                  No devices in passive-SNMP inventory yet.
                </div>
              )}
            </div>
          </section>

          <section className="space-y-2">
            <h3 className="text-sm font-semibold text-slate-200">Change feed</h3>
            <div className="overflow-auto rounded-xl border border-ink-800 bg-ink-900">
              <table className="w-full text-left text-sm">
                <thead className="bg-ink-800/60 text-xs uppercase text-slate-400">
                  <tr>
                    <th className="px-3 py-2">When</th>
                    <th className="px-3 py-2">Kind</th>
                    <th className="px-3 py-2">IP</th>
                    <th className="px-3 py-2">From → To</th>
                  </tr>
                </thead>
                <tbody>
                  {changes.data?.map((c) => (
                    <tr key={c.id} className="border-t border-ink-800 align-top">
                      <td className="px-3 py-2 text-slate-500">{formatRelative(c.time)}</td>
                      <td className="px-3 py-2">
                        <span className={"rounded-full px-2 py-0.5 text-xs " + kindBadge[c.kind]}>
                          {c.kind}
                        </span>
                      </td>
                      <td className="px-3 py-2 font-mono text-xs">{c.ip}</td>
                      <td className="px-3 py-2 text-xs text-slate-400">
                        {c.old?.vendor && (
                          <span>
                            {c.old.vendor || "?"} / {c.old.type || "?"} →{" "}
                          </span>
                        )}
                        {c.new?.vendor && (
                          <span>
                            {c.new.vendor || "?"} / {c.new.type || "?"}
                          </span>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
              {!changes.data?.length && !changes.isLoading && (
                <div className="px-4 py-6 text-center text-sm text-slate-500">
                  No changes recorded yet.
                </div>
              )}
            </div>
          </section>
        </>
      )}
    </div>
  );
}

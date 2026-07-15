// ApplianceDetail — the system tab for a single network appliance.
//
// Renders from one /appliances/{id} fetch + one /appliances/{id}/metrics
// fetch; per-port time-series is fetched lazily when the operator
// expands a row. Same shape and idioms as AgentDetail so the
// dashboard feels consistent across "the box on the rack" and "the
// switch above it".

import { useQuery } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { api } from "../api/client";
import type {
  ApplianceDetail,
  ApplianceEntity,
  ApplianceIfaceSeries,
  ApplianceInterface,
  ApplianceLLDP,
  ApplianceMetricSeries,
  ApplianceSnapshot,
  ApplianceVendorMetricSeries,
  MerakiDashboardSnapshot,
  Site,
} from "../api/types";
import { isMerakiDashboardSnapshot } from "../api/types";
import Sparkline from "../components/Sparkline";
import {
  formatBytes,
  formatDuration,
  formatPct,
  formatRelative,
  pctBarColor,
} from "../lib/format";

export default function ApplianceDetailPage() {
  const { id = "" } = useParams<{ id: string }>();

  const appliance = useQuery({
    queryKey: ["appliance", id],
    queryFn: () => api.get<ApplianceDetail>(`/appliances/${id}`),
    refetchInterval: 30_000,
    enabled: !!id,
  });
  const metrics = useQuery({
    queryKey: ["appliance-metrics", id, "24h"],
    queryFn: () => api.get<ApplianceMetricSeries>(`/appliances/${id}/metrics?range=24h`),
    refetchInterval: 60_000,
    enabled: !!id,
  });
  const sites = useQuery({ queryKey: ["sites"], queryFn: () => api.get<Site[]>("/sites") });

  const snap = appliance.data?.lastSnapshot ?? null;
  const merakiSnap = isMerakiDashboardSnapshot(snap) ? snap : null;
  const snmpSnap = !merakiSnap && snap ? (snap as ApplianceSnapshot) : null;

  const cpuSeries = useMemo(
    () => (metrics.data?.samples ?? []).map((s) => Number(s.cpuPct ?? 0)),
    [metrics.data],
  );
  const memSeries = useMemo(() => {
    if (!metrics.data) return [];
    return metrics.data.samples.map((s) => {
      const used = Number(s.memUsedBytes ?? 0);
      const total = Number(s.memTotalBytes ?? 0);
      return total > 0 ? (used / total) * 100 : 0;
    });
  }, [metrics.data]);

  if (appliance.isLoading) {
    return <div className="text-sm text-slate-400">Loading appliance…</div>;
  }
  if (appliance.isError || !appliance.data) {
    return (
      <div className="space-y-3">
        <Link to="/appliances" className="text-sm text-sonar-400 hover:underline">
          ← Back to appliances
        </Link>
        <div className="rounded-md border border-red-800/60 bg-red-950/40 p-4 text-sm text-red-200">
          Could not load appliance:{" "}
          {(appliance.error as Error)?.message ?? "unknown error"}
        </div>
      </div>
    );
  }

  const a = appliance.data;
  const siteName = sites.data?.find((s) => s.id === a.siteId)?.name ?? a.siteId.slice(0, 8);
  const memPct =
    a.memUsedBytes != null && a.memTotalBytes && a.memTotalBytes > 0
      ? (Number(a.memUsedBytes) / Number(a.memTotalBytes)) * 100
      : null;
  // Meraki Dashboard telemetry often runs on the site sync interval (~15m);
  // SNMP devices use 3× pollInterval.
  const pollWindowSec =
    a.vendor === "meraki"
      ? Math.max(3 * a.pollIntervalSeconds, 45 * 60)
      : Math.max(3 * a.pollIntervalSeconds, 180);
  const polledRecently =
    a.lastPolledAt &&
    Date.now() - new Date(a.lastPolledAt).getTime() < pollWindowSec * 1000;

  return (
    <div className="space-y-6">
      <div className="flex items-end justify-between gap-4">
        <div>
          <Link to="/appliances" className="text-xs text-sonar-400 hover:underline">
            ← All appliances
          </Link>
          <h2 className="mt-1 text-2xl font-semibold tracking-tight">
            {a.name}
            {a.sysName && a.sysName !== a.name && (
              <span className="ml-2 text-sm font-normal text-slate-400">
                ({a.sysName})
              </span>
            )}
          </h2>
          <p className="text-sm text-slate-400">
            {siteName} · {a.vendor}
            {a.model && <> · {a.model}</>}
            {a.vendor === "meraki" ? (
              <> · Dashboard API</>
            ) : (
              <> · SNMP {a.snmpVersion}</>
            )}{" "}
            · <span className="font-mono">{a.mgmtIp}</span>
          </p>
          <p className="mt-1 text-xs">
            <Link
              className="text-sonar-400 hover:underline"
              to={`/checks?siteId=${encodeURIComponent(a.siteId)}&applianceId=${encodeURIComponent(a.id)}&host=${encodeURIComponent(a.mgmtIp)}&typeId=icmp`}
            >
              Add check
            </Link>
          </p>
        </div>
        <div className="text-right text-xs">
          <div>
            <span
              className={
                polledRecently
                  ? "rounded bg-emerald-900/40 px-2 py-0.5 text-emerald-300"
                  : "rounded bg-slate-800 px-2 py-0.5 text-slate-400"
              }
            >
              {polledRecently ? "polling" : "stale"}
            </span>
          </div>
          <div className="mt-1 text-slate-500">
            polled {formatRelative(a.lastPolledAt)}
          </div>
          {a.vendor !== "meraki" && (
            <div className="text-slate-600">interval {a.pollIntervalSeconds}s</div>
          )}
        </div>
      </div>

      {a.lastError && (
        <div className="rounded-xl border border-red-800/60 bg-red-950/40 p-3 text-sm text-red-200">
          <strong className="font-semibold">Last poll failed.</strong>{" "}
          <code className="rounded bg-red-950/60 px-1 py-0.5 font-mono text-xs">
            {a.lastError}
          </code>
        </div>
      )}

      {merakiSnap ? (
        <MerakiHealthBlock detail={a} snap={merakiSnap} />
      ) : snmpSnap == null ? (
        <div className="rounded-xl border border-ink-800 bg-ink-900 p-6 text-sm text-slate-400">
          {a.vendor === "meraki" ? (
            <>
              No Meraki Dashboard health snapshot yet. With Meraki sync enabled,
              the poller refreshes live status on the site sync interval (typically
              every 15 minutes). Use Discovery → Meraki → Poll now to refresh
              inventory; health follows on the next telemetry cycle.
            </>
          ) : (
            <>
              No SNMP snapshot yet. The poller picks up new appliances within ~30
              seconds and polls each on its configured interval. If nothing
              appears within a couple of cycles, double-check the community
              string / v3 user and that the device&apos;s SNMP ACL allows the
              poller host&apos;s IP.
            </>
          )}
        </div>
      ) : (
        <>
          <StatCards
            cpuPct={a.cpuPct ?? snmpSnap.chassis.cpuPct ?? null}
            memPct={memPct}
            uptime={a.uptimeSeconds ?? snmpSnap.system.uptimeSeconds}
            physUp={
              a.physUpCount ??
              snmpSnap.interfaces.filter((i) => i.kind === "physical" && i.operUp).length
            }
            physTotal={
              a.physTotalCount ??
              snmpSnap.interfaces.filter((i) => i.kind === "physical").length
            }
            logicalCount={
              snmpSnap.interfaces.length -
              (a.physTotalCount ??
                snmpSnap.interfaces.filter((i) => i.kind === "physical").length)
            }
            uplinkCount={
              a.uplinkCount ?? snmpSnap.interfaces.filter((i) => i.isUplink).length
            }
            memTotal={Number(a.memTotalBytes ?? snmpSnap.chassis.memTotalBytes ?? 0)}
          />

          <Charts cpu={cpuSeries} mem={memSeries} loading={metrics.isLoading} />

          <InterfacesTable applianceId={a.id} interfaces={snmpSnap.interfaces ?? []} />

          {snmpSnap.entities && snmpSnap.entities.length > 0 && (
            <EntitiesTable entities={snmpSnap.entities} />
          )}

          {snmpSnap.lldp && snmpSnap.lldp.length > 0 && <LLDPTable neighbors={snmpSnap.lldp} />}

          {snmpSnap.vendor?.oidMetrics && snmpSnap.vendor.oidMetrics.length > 0 && (
            <OIDMetricsTable metrics={snmpSnap.vendor.oidMetrics} />
          )}

          <SystemMeta detail={a} />

          {snmpSnap.collectionWarnings && snmpSnap.collectionWarnings.length > 0 && (
            <div className="rounded-xl border border-amber-800/40 bg-amber-950/20 p-3 text-xs text-amber-200">
              <div className="mb-1 font-semibold">Collection warnings</div>
              <ul className="list-inside list-disc space-y-0.5">
                {snmpSnap.collectionWarnings.map((w) => (
                  <li key={w}>{w}</li>
                ))}
              </ul>
            </div>
          )}
        </>
      )}
    </div>
  );
}

function MerakiHealthBlock({
  detail,
  snap,
}: {
  detail: ApplianceDetail;
  snap: MerakiDashboardSnapshot;
}) {
  const [expandedPort, setExpandedPort] = useState<number | null>(null);
  const status = (snap.status ?? "unknown").toLowerCase();
  const statusClass =
    status === "online"
      ? "bg-emerald-900/40 text-emerald-300"
      : status === "alerting"
        ? "bg-amber-900/40 text-amber-200"
        : "bg-red-900/40 text-red-200";
  const lossByUplink = new Map(
    (snap.lossLatency ?? []).map((l) => [l.uplink.toLowerCase(), l]),
  );
  const physUp = detail.physUpCount ?? snap.physUp ?? null;
  const physTotal = detail.physTotalCount ?? snap.physTotal ?? null;
  const uplinkCount = detail.uplinkCount ?? snap.uplinkCount ?? snap.uplinks?.length ?? null;
  const product = (snap.productType ?? "").toLowerCase();
  const isSwitch = product === "switch";
  const isAppliance = product === "appliance";
  const isWireless = product === "wireless";
  const isSensor = product === "sensor";
  const hasPortTraffic = (snap.ports ?? []).some(
    (p) => p.rxPackets != null || p.txPackets != null,
  );
  const hasPortBps = (snap.ports ?? []).some((p) => p.inBps != null || p.outBps != null);
  const hasPortExtras = (snap.ports ?? []).some(
    (p) => p.name || p.vlan != null || p.clientCount != null || p.neighbor,
  );
  const hasCellular = (snap.uplinks ?? []).some(
    (u) => u.provider || u.iccid || u.rsrp || u.interface?.toLowerCase() === "cellular",
  );
  const memUsed = detail.memUsedBytes ?? snap.memUsedBytes ?? null;
  const memTotal = detail.memTotalBytes ?? snap.memTotalBytes ?? null;
  const memPct =
    memUsed != null && memTotal != null && Number(memTotal) > 0
      ? (Number(memUsed) / Number(memTotal)) * 100
      : null;

  const vendorKeys = useMemo(() => {
    const keys = [
      "meraki.status.online",
      "meraki.switch.ports.up",
      "meraki.switch.ports.total",
      "meraki.switch.ports.error_count",
      "meraki.switch.ports.rx_packets",
      "meraki.switch.ports.tx_packets",
      "meraki.wireless.clients",
      "meraki.wireless.loss_downstream_pct",
      "meraki.wireless.loss_upstream_pct",
      "meraki.appliance.perf_score",
      "meraki.vpn.peers_reachable",
      "meraki.vpn.peers_total",
    ];
    for (const u of snap.uplinks ?? []) {
      const iface = u.interface.toLowerCase();
      keys.push(`meraki.uplink.${iface}.loss_pct`, `meraki.uplink.${iface}.latency_ms`);
    }
    for (const r of snap.sensorReadings ?? []) {
      keys.push(`meraki.sensor.${r.metric.toLowerCase()}`);
    }
    return [...new Set(keys)].join(",");
  }, [snap.uplinks, snap.sensorReadings]);

  const vendorMetrics = useQuery({
    queryKey: ["appliance-vendor-metrics", detail.id, "24h", vendorKeys],
    queryFn: () =>
      api.get<ApplianceVendorMetricSeries>(
        `/appliances/${detail.id}/vendor-metrics?range=24h&keys=${encodeURIComponent(vendorKeys)}`,
      ),
    refetchInterval: 60_000,
  });

  const chassisMetrics = useQuery({
    queryKey: ["appliance-metrics", detail.id, "24h", "meraki"],
    queryFn: () =>
      api.get<ApplianceMetricSeries>(`/appliances/${detail.id}/metrics?range=24h`),
    refetchInterval: 60_000,
    enabled: isSwitch,
  });

  const memSeries = useMemo(() => {
    if (!chassisMetrics.data) return [];
    return chassisMetrics.data.samples.map((s) => {
      const used = Number(s.memUsedBytes ?? 0);
      const total = Number(s.memTotalBytes ?? 0);
      return total > 0 ? (used / total) * 100 : 0;
    });
  }, [chassisMetrics.data]);

  const seriesByKey = useMemo(() => {
    const m = new Map<string, number[]>();
    for (const s of vendorMetrics.data?.samples ?? []) {
      if (s.valueDouble == null) continue;
      const arr = m.get(s.metricKey) ?? [];
      arr.push(Number(s.valueDouble));
      m.set(s.metricKey, arr);
    }
    return m;
  }, [vendorMetrics.data]);

  return (
    <div className="space-y-4">
      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-4">
        <div className="rounded-xl border border-ink-800 bg-ink-900 p-4">
          <div className="text-xs uppercase tracking-wide text-slate-500">Status</div>
          <div className="mt-2">
            <span className={`rounded px-2 py-0.5 text-sm capitalize ${statusClass}`}>
              {snap.status ?? "unknown"}
            </span>
          </div>
          {snap.lastReportedAt && (
            <div className="mt-2 text-xs text-slate-500">
              reported {formatRelative(snap.lastReportedAt)}
            </div>
          )}
        </div>
        <div className="rounded-xl border border-ink-800 bg-ink-900 p-4">
          <div className="text-xs uppercase tracking-wide text-slate-500">Product</div>
          <div className="mt-2 text-lg text-slate-200">
            {snap.model || snap.productType || detail.model || "—"}
          </div>
          {snap.productType && (
            <div className="mt-1 text-xs text-slate-500">{snap.productType}</div>
          )}
        </div>
        <div className="rounded-xl border border-ink-800 bg-ink-900 p-4">
          <div className="text-xs uppercase tracking-wide text-slate-500">
            {isSwitch ? "Ports" : isWireless ? "Clients" : isAppliance ? "Perf" : "Uplinks"}
          </div>
          <div className="mt-2 text-lg text-slate-200">
            {isSwitch && physTotal != null ? (
              <>
                <span className="text-emerald-300">{physUp ?? 0}</span>
                <span className="text-slate-500"> / {physTotal}</span>
              </>
            ) : isWireless && snap.clientCount != null ? (
              snap.clientCount
            ) : isAppliance && snap.perfScore != null ? (
              `${snap.perfScore.toFixed(0)}`
            ) : (
              uplinkCount ?? "—"
            )}
          </div>
        </div>
        <div className="rounded-xl border border-ink-800 bg-ink-900 p-4">
          <div className="text-xs uppercase tracking-wide text-slate-500">
            {isSwitch && memPct != null ? "Memory" : "Network"}
          </div>
          {isSwitch && memPct != null ? (
            <>
              <div className="mt-2 text-lg text-slate-200">{formatPct(memPct)}</div>
              <div className="mt-1 text-xs text-slate-500">
                {formatBytes(Number(memUsed))} / {formatBytes(Number(memTotal))}
              </div>
            </>
          ) : (
            <>
              <div className="mt-2 font-mono text-sm text-slate-200">
                {snap.lanIp || detail.mgmtIp || "—"}
              </div>
              {snap.publicIp && (
                <div className="mt-1 font-mono text-xs text-slate-500">pub {snap.publicIp}</div>
              )}
            </>
          )}
        </div>
      </div>

      {isSwitch && (memSeries.length > 0 || chassisMetrics.isLoading) && (
        <section className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          <MerakiChartCard
            title="Memory %"
            values={memSeries}
            now={memSeries.length ? memSeries[memSeries.length - 1] : memPct ?? undefined}
            suffix="%"
          />
        </section>
      )}

      <section className="rounded-xl border border-ink-800 bg-ink-900 p-4 text-sm">
        <div className="mb-2 text-xs font-semibold uppercase tracking-wide text-slate-400">
          Device addressing
        </div>
        <div className="grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
          <Meta label="LAN IP" value={snap.lanIp || detail.mgmtIp || "—"} />
          <Meta label="Public IP" value={snap.publicIp || "—"} />
          <Meta label="Gateway" value={snap.gateway || "—"} />
          <Meta label="IP type" value={snap.ipType || "—"} />
          <Meta label="Primary DNS" value={snap.primaryDns || "—"} />
          <Meta label="Secondary DNS" value={snap.secondaryDns || "—"} />
          <Meta label="MAC" value={snap.mac || "—"} />
          <Meta
            label="Tags"
            value={snap.tags && snap.tags.length ? snap.tags.join(", ") : "—"}
          />
          {snap.highAvailability && (
            <Meta
              label="HA"
              value={`${snap.highAvailability.enabled ? "enabled" : "disabled"}${
                snap.highAvailability.role ? ` · ${snap.highAvailability.role}` : ""
              }`}
            />
          )}
        </div>
        {snap.powerSupplies && snap.powerSupplies.length > 0 && (
          <div className="mt-3 border-t border-ink-800 pt-3">
            <div className="mb-1 text-xs text-slate-500">Power supplies</div>
            <ul className="space-y-1 text-xs text-slate-300">
              {snap.powerSupplies.map((p) => (
                <li key={`${p.slot}-${p.serial}`}>
                  slot {p.slot}: {p.model || "—"} · {p.status || "—"}
                  {p.poeMaximum != null
                    ? ` · PoE ${p.poeMaximum}${p.poeUnit ? ` ${p.poeUnit}` : ""}`
                    : ""}
                </li>
              ))}
            </ul>
          </div>
        )}
        {snap.firmware && (
          <div className="mt-3 border-t border-ink-800 pt-3 grid gap-2 sm:grid-cols-2 lg:grid-cols-3">
            <Meta label="Firmware" value={snap.firmware.current || "—"} />
            <Meta label="Next upgrade" value={snap.firmware.nextUpgrade || "—"} />
            <Meta
              label="Upgrade at"
              value={
                snap.firmware.nextAt ? formatRelative(snap.firmware.nextAt) : "—"
              }
            />
          </div>
        )}
      </section>

      {(seriesByKey.size > 0 || vendorMetrics.isLoading) && (
        <section className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {(snap.uplinks ?? []).flatMap((u) => {
            const iface = u.interface.toLowerCase();
            const loss = seriesByKey.get(`meraki.uplink.${iface}.loss_pct`) ?? [];
            const lat = seriesByKey.get(`meraki.uplink.${iface}.latency_ms`) ?? [];
            const cards = [];
            if (loss.length > 0) {
              cards.push(
                <MerakiChartCard
                  key={`${iface}-loss`}
                  title={`${u.interface} loss %`}
                  values={loss}
                  now={loss[loss.length - 1]}
                  suffix="%"
                />,
              );
            }
            if (lat.length > 0) {
              cards.push(
                <MerakiChartCard
                  key={`${iface}-lat`}
                  title={`${u.interface} latency`}
                  values={lat}
                  now={lat[lat.length - 1]}
                  suffix=" ms"
                />,
              );
            }
            return cards;
          })}
          {(seriesByKey.get("meraki.switch.ports.up") ?? []).length > 0 && (
            <MerakiChartCard
              title="Ports up"
              values={seriesByKey.get("meraki.switch.ports.up") ?? []}
              now={(seriesByKey.get("meraki.switch.ports.up") ?? [])[
                (seriesByKey.get("meraki.switch.ports.up") ?? []).length - 1
              ]}
            />
          )}
          {(seriesByKey.get("meraki.wireless.clients") ?? []).length > 0 && (
            <MerakiChartCard
              title="Wireless clients"
              values={seriesByKey.get("meraki.wireless.clients") ?? []}
              now={(seriesByKey.get("meraki.wireless.clients") ?? [])[
                (seriesByKey.get("meraki.wireless.clients") ?? []).length - 1
              ]}
            />
          )}
          {(seriesByKey.get("meraki.appliance.perf_score") ?? []).length > 0 && (
            <MerakiChartCard
              title="Appliance perf"
              values={seriesByKey.get("meraki.appliance.perf_score") ?? []}
              now={(seriesByKey.get("meraki.appliance.perf_score") ?? [])[
                (seriesByKey.get("meraki.appliance.perf_score") ?? []).length - 1
              ]}
            />
          )}
        </section>
      )}

      {snap.uplinks && snap.uplinks.length > 0 && (
        <section className="overflow-hidden rounded-xl border border-ink-800 bg-ink-900">
          <div className="border-b border-ink-800 px-4 py-2 text-xs font-semibold uppercase tracking-wide text-slate-400">
            WAN uplinks (telemetry — not management IP)
          </div>
          <div className="overflow-x-auto">
            <table className="w-full text-left text-sm">
              <thead className="bg-ink-800/40 text-xs uppercase text-slate-500">
                <tr>
                  <th className="px-4 py-2">Interface</th>
                  <th className="px-4 py-2">Status</th>
                  <th className="px-4 py-2">IP</th>
                  <th className="px-4 py-2">Gateway</th>
                  <th className="px-4 py-2">Public IP</th>
                  <th className="px-4 py-2">DNS</th>
                  <th className="px-4 py-2">Assign</th>
                  <th className="px-4 py-2">Loss</th>
                  <th className="px-4 py-2">Latency</th>
                  {hasCellular && <th className="px-4 py-2">Cellular</th>}
                </tr>
              </thead>
              <tbody>
                {snap.uplinks.map((u) => {
                  const ll = lossByUplink.get(u.interface.toLowerCase());
                  return (
                    <tr key={u.interface} className="border-t border-ink-800">
                      <td className="px-4 py-2 font-mono text-slate-300">{u.interface}</td>
                      <td className="px-4 py-2 capitalize text-slate-300">{u.status}</td>
                      <td className="px-4 py-2 font-mono text-slate-400">{u.ip || "—"}</td>
                      <td className="px-4 py-2 font-mono text-slate-400">
                        {u.gateway || "—"}
                      </td>
                      <td className="px-4 py-2 font-mono text-slate-400">
                        {u.publicIp || "—"}
                      </td>
                      <td className="px-4 py-2 font-mono text-xs text-slate-400">
                        {u.primaryDns || "—"}
                        {u.secondaryDns ? ` / ${u.secondaryDns}` : ""}
                      </td>
                      <td className="px-4 py-2 text-slate-400">{u.ipAssignedBy || "—"}</td>
                      <td className="px-4 py-2 text-slate-300">
                        {ll?.lossPercent != null ? `${ll.lossPercent.toFixed(2)}%` : "—"}
                      </td>
                      <td className="px-4 py-2 text-slate-300">
                        {ll?.latencyMs != null ? `${ll.latencyMs.toFixed(0)} ms` : "—"}
                      </td>
                      {hasCellular && (
                        <td className="px-4 py-2 text-xs text-slate-400">
                          {[u.provider, u.signalType, u.rsrp && `rsrp ${u.rsrp}`, u.iccid]
                            .filter(Boolean)
                            .join(" · ") || "—"}
                        </td>
                      )}
                    </tr>
                  );
                })}
              </tbody>
            </table>
          </div>
        </section>
      )}

      {snap.vpn && (
        <section className="rounded-xl border border-ink-800 bg-ink-900 p-4">
          <div className="mb-2 text-xs font-semibold uppercase tracking-wide text-slate-400">
            Site-to-site VPN
          </div>
          <div className="mb-3 text-sm text-slate-300">
            mode {snap.vpn.mode || "—"} · peers{" "}
            <span className="text-emerald-300">{snap.vpn.reachablePeerCount}</span>
            <span className="text-slate-500"> / {snap.vpn.totalPeerCount}</span>
          </div>
          <div className="grid gap-2 sm:grid-cols-2">
            {(snap.vpn.merakiPeers ?? []).slice(0, 12).map((p) => (
              <div key={p.name} className="text-xs text-slate-400">
                {p.name}: <span className="text-slate-300">{p.reachability || "—"}</span>
              </div>
            ))}
            {(snap.vpn.thirdPartyPeers ?? []).slice(0, 8).map((p) => (
              <div key={p.name} className="text-xs text-slate-400">
                {p.name}
                {p.publicIp ? ` (${p.publicIp})` : ""}:{" "}
                <span className="text-slate-300">{p.reachability || "—"}</span>
              </div>
            ))}
          </div>
        </section>
      )}

      {isWireless && snap.wirelessLoss && (
        <section className="rounded-xl border border-ink-800 bg-ink-900 p-4 text-sm text-slate-300">
          Wireless path loss — down{" "}
          {snap.wirelessLoss.downstreamLossPct != null
            ? `${snap.wirelessLoss.downstreamLossPct.toFixed(2)}%`
            : "—"}
          , up{" "}
          {snap.wirelessLoss.upstreamLossPct != null
            ? `${snap.wirelessLoss.upstreamLossPct.toFixed(2)}%`
            : "—"}
        </section>
      )}

      {snap.ports && snap.ports.length > 0 && (
        <section className="overflow-hidden rounded-xl border border-ink-800 bg-ink-900">
          <div className="border-b border-ink-800 px-4 py-2 text-xs font-semibold uppercase tracking-wide text-slate-400">
            Switch ports
            {snap.portErrorCount != null && snap.portErrorCount > 0
              ? ` · ${snap.portErrorCount} with errors`
              : ""}
          </div>
          <div className="max-h-[28rem] overflow-auto">
            <table className="w-full text-left text-sm">
              <thead className="sticky top-0 bg-ink-800/90 text-xs uppercase text-slate-500">
                <tr>
                  <th className="px-4 py-2">Port</th>
                  {hasPortExtras && <th className="px-4 py-2">Name</th>}
                  <th className="px-4 py-2">Status</th>
                  <th className="px-4 py-2">Speed</th>
                  {hasPortExtras && <th className="px-4 py-2">VLAN</th>}
                  {hasPortExtras && <th className="px-4 py-2">Clients</th>}
                  {hasPortBps && <th className="px-4 py-2 text-right">In</th>}
                  {hasPortBps && <th className="px-4 py-2 text-right">Out</th>}
                  {hasPortExtras && <th className="px-4 py-2">Neighbor</th>}
                  <th className="px-4 py-2">Uplink</th>
                  <th className="px-4 py-2">PoE</th>
                  {hasPortTraffic && <th className="px-4 py-2">Rx pkts</th>}
                  {hasPortTraffic && <th className="px-4 py-2">Tx pkts</th>}
                  <th className="px-4 py-2">Errors</th>
                  {hasPortBps && <th className="px-4 py-2 text-right">Graph</th>}
                </tr>
              </thead>
              <tbody>
                {snap.ports.map((p) => {
                  const ifIndex = p.ifIndex && p.ifIndex > 0 ? p.ifIndex : null;
                  const expanded = ifIndex != null && expandedPort === ifIndex;
                  const colSpan =
                    6 +
                    (hasPortExtras ? 4 : 0) +
                    (hasPortBps ? 3 : 0) +
                    (hasPortTraffic ? 2 : 0);
                  return (
                    <MerakiPortRows
                      key={p.portId}
                      applianceId={detail.id}
                      port={p}
                      ifIndex={ifIndex}
                      expanded={expanded}
                      colSpan={colSpan}
                      hasPortExtras={hasPortExtras}
                      hasPortBps={hasPortBps}
                      hasPortTraffic={hasPortTraffic}
                      onToggle={() =>
                        ifIndex != null &&
                        setExpandedPort(expanded ? null : ifIndex)
                      }
                    />
                  );
                })}
              </tbody>
            </table>
          </div>
        </section>
      )}

      {snap.neighbors && snap.neighbors.length > 0 && (
        <section className="overflow-hidden rounded-xl border border-ink-800 bg-ink-900">
          <div className="border-b border-ink-800 px-4 py-2 text-xs font-semibold uppercase tracking-wide text-slate-400">
            Neighbors (LLDP/CDP)
          </div>
          <div className="overflow-x-auto">
            <table className="w-full text-left text-sm">
              <thead className="bg-ink-800/40 text-xs uppercase text-slate-500">
                <tr>
                  <th className="px-4 py-2">Port</th>
                  <th className="px-4 py-2">Protocol</th>
                  <th className="px-4 py-2">Neighbor</th>
                </tr>
              </thead>
              <tbody>
                {snap.neighbors.map((n) => (
                  <tr
                    key={`${n.portId}-${n.protocol}-${n.summary}`}
                    className="border-t border-ink-800"
                  >
                    <td className="px-4 py-2 font-mono text-slate-300">{n.portId}</td>
                    <td className="px-4 py-2 uppercase text-slate-400">{n.protocol}</td>
                    <td className="px-4 py-2 text-slate-300">{n.summary}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </section>
      )}

      {isSensor && snap.sensorReadings && snap.sensorReadings.length > 0 && (
        <section className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
          {snap.sensorReadings.map((r) => (
            <div
              key={`${r.metric}-${r.ts}`}
              className="rounded-xl border border-ink-800 bg-ink-900 p-4"
            >
              <div className="text-xs uppercase tracking-wide text-slate-500">{r.metric}</div>
              <div className="mt-2 text-lg text-slate-200">
                {r.value != null
                  ? `${r.value}${r.unit ? ` ${r.unit}` : ""}`
                  : r.bool != null
                    ? r.bool
                      ? "yes"
                      : "no"
                    : "—"}
              </div>
            </div>
          ))}
        </section>
      )}

      {snap.alerts && snap.alerts.length > 0 && (
        <section className="rounded-xl border border-amber-800/40 bg-amber-950/20 p-4">
          <div className="mb-2 text-xs font-semibold uppercase tracking-wide text-amber-200">
            Recent Meraki alerts
          </div>
          <ul className="space-y-1 text-sm text-amber-100/90">
            {snap.alerts.map((a) => (
              <li key={a.id || `${a.title}-${a.startedAt}`}>
                <span className="uppercase text-amber-300/80">{a.severity || "alert"}</span>
                {": "}
                {a.title || a.type || "—"}
                {a.startedAt ? (
                  <span className="text-amber-200/60"> · {formatRelative(a.startedAt)}</span>
                ) : null}
              </li>
            ))}
          </ul>
        </section>
      )}

      <SystemMeta detail={detail} />
      <p className="text-xs text-slate-500">
        Live health from Meraki Dashboard API (not SNMP). Management IP stays on the
        LAN/appliance address. Charts use vendor samples from the last 24h.
      </p>
    </div>
  );
}

function MerakiPortRows({
  applianceId,
  port: p,
  ifIndex,
  expanded,
  colSpan,
  hasPortExtras,
  hasPortBps,
  hasPortTraffic,
  onToggle,
}: {
  applianceId: string;
  port: NonNullable<MerakiDashboardSnapshot["ports"]>[number];
  ifIndex: number | null;
  expanded: boolean;
  colSpan: number;
  hasPortExtras: boolean;
  hasPortBps: boolean;
  hasPortTraffic: boolean;
  onToggle: () => void;
}) {
  return (
    <>
      <tr className="border-t border-ink-800">
        <td className="px-4 py-2 font-mono text-slate-300">
          {p.portId}
          {p.type ? (
            <span className="ml-1 text-[10px] uppercase text-slate-600">{p.type}</span>
          ) : null}
        </td>
        {hasPortExtras && (
          <td className="px-4 py-2 text-slate-400">{p.name || "—"}</td>
        )}
        <td className="px-4 py-2 text-slate-300">{p.status}</td>
        <td className="px-4 py-2 text-slate-400">
          {p.speed || "—"}
          {p.duplex ? ` ${p.duplex}` : ""}
        </td>
        {hasPortExtras && (
          <td className="px-4 py-2 text-slate-400">{p.vlan != null ? p.vlan : "—"}</td>
        )}
        {hasPortExtras && (
          <td className="px-4 py-2 text-slate-400">
            {p.clientCount != null ? p.clientCount : "—"}
          </td>
        )}
        {hasPortBps && (
          <td className="px-4 py-2 text-right text-emerald-300">
            {p.inBps == null ? "—" : formatBitRate(Number(p.inBps))}
          </td>
        )}
        {hasPortBps && (
          <td className="px-4 py-2 text-right text-sonar-300">
            {p.outBps == null ? "—" : formatBitRate(Number(p.outBps))}
          </td>
        )}
        {hasPortExtras && (
          <td
            className="max-w-[14rem] truncate px-4 py-2 text-xs text-slate-400"
            title={p.neighbor || undefined}
          >
            {p.neighbor || "—"}
          </td>
        )}
        <td className="px-4 py-2 text-slate-400">{p.isUplink ? "yes" : "—"}</td>
        <td className="px-4 py-2 text-slate-400">
          {p.poeAllocated == null ? "—" : p.poeAllocated ? "alloc" : "—"}
        </td>
        {hasPortTraffic && (
          <td className="px-4 py-2 font-mono text-xs text-slate-400">
            {p.rxPackets != null ? p.rxPackets.toLocaleString() : "—"}
          </td>
        )}
        {hasPortTraffic && (
          <td className="px-4 py-2 font-mono text-xs text-slate-400">
            {p.txPackets != null ? p.txPackets.toLocaleString() : "—"}
          </td>
        )}
        <td className="px-4 py-2 text-xs text-amber-200">
          {[...(p.errors ?? []), ...(p.warnings ?? [])].join(", ") || "—"}
        </td>
        {hasPortBps && (
          <td className="px-4 py-2 text-right">
            {ifIndex != null && (p.inBps != null || p.outBps != null) ? (
              <button
                type="button"
                onClick={onToggle}
                className="rounded border border-ink-700 px-2 py-0.5 text-xs text-slate-300 hover:bg-ink-800"
              >
                {expanded ? "hide" : "graph"}
              </button>
            ) : (
              <span className="text-slate-600">—</span>
            )}
          </td>
        )}
      </tr>
      {expanded && ifIndex != null && (
        <tr className="bg-ink-950/50">
          <td colSpan={colSpan} className="px-3 py-3">
            <IfaceSparkline applianceId={applianceId} ifIndex={ifIndex} />
          </td>
        </tr>
      )}
    </>
  );
}

function MerakiChartCard({
  title,
  values,
  now,
  suffix = "",
}: {
  title: string;
  values: number[];
  now?: number;
  suffix?: string;
}) {
  return (
    <div className="rounded-xl border border-ink-800 bg-ink-900 p-3">
      <div className="mb-1 flex items-baseline justify-between gap-2">
        <div className="text-xs uppercase tracking-wide text-slate-500">{title}</div>
        <div className="text-sm text-slate-200">
          {now != null ? `${Number(now).toFixed(now < 10 ? 2 : 0)}${suffix}` : "—"}
        </div>
      </div>
      <Sparkline values={values} height={40} min={0} />
      <div className="mt-1 text-[10px] text-slate-600">24h</div>
    </div>
  );
}

// ---- Stat cards ----------------------------------------------------------

interface StatCardsProps {
  cpuPct: number | null;
  memPct: number | null;
  uptime: number;
  physUp: number;
  physTotal: number;
  logicalCount: number;
  uplinkCount: number;
  memTotal: number;
}

function StatCards(p: StatCardsProps) {
  // Physical ports are the only count that should drive the "X / Y up" UX —
  // an access switch's ifTable is dominated by SVIs, port-channels, and
  // loopbacks, so the raw number lies about how many cables you can plug in.
  const portsPct = p.physTotal > 0 ? (p.physUp / p.physTotal) * 100 : null;
  const sub =
    p.physTotal === 0
      ? "no physical ports"
      : p.uplinkCount > 0
        ? `${p.uplinkCount} uplink${p.uplinkCount === 1 ? "" : "s"} · ${p.logicalCount} logical`
        : `${p.logicalCount} logical interfaces`;
  return (
    <div className="grid grid-cols-2 gap-3 md:grid-cols-4">
      <Stat
        label="CPU"
        value={formatPct(p.cpuPct)}
        bar={p.cpuPct ?? 0}
        sub={p.cpuPct == null ? "no chassis MIB" : "5s avg"}
      />
      <Stat
        label="Memory"
        value={formatPct(p.memPct)}
        bar={p.memPct ?? 0}
        sub={p.memTotal ? `${formatBytes(p.memTotal)} total` : "—"}
      />
      <Stat
        label="Physical ports"
        value={p.physTotal === 0 ? "—" : `${p.physUp} / ${p.physTotal}`}
        bar={portsPct ?? 0}
        sub={sub}
      />
      <Stat
        label="Uptime"
        value={formatDuration(p.uptime)}
        sub="since last reboot"
      />
    </div>
  );
}

interface StatProps {
  label: string;
  value: string;
  bar?: number;
  sub: string;
}

function Stat({ label, value, bar, sub }: StatProps) {
  return (
    <div className="rounded-xl border border-ink-800 bg-ink-900 p-4">
      <div className="text-xs uppercase tracking-wide text-slate-500">{label}</div>
      <div className="mt-1 text-2xl font-semibold tracking-tight text-slate-100">
        {value}
      </div>
      {bar != null && (
        <div className="mt-2 h-1.5 w-full overflow-hidden rounded-full bg-ink-800">
          <div
            className={`h-full ${pctBarColor(bar)}`}
            style={{ width: `${Math.min(100, Math.max(0, bar))}%` }}
          />
        </div>
      )}
      <div className="mt-2 text-xs text-slate-500">{sub}</div>
    </div>
  );
}

// ---- Sparkline charts ----------------------------------------------------

function Charts({
  cpu,
  mem,
  loading,
}: {
  cpu: number[];
  mem: number[];
  loading: boolean;
}) {
  return (
    <div className="grid grid-cols-1 gap-4 md:grid-cols-2">
      <ChartCard title="CPU (24h)" data={cpu} suffix="%" loading={loading} />
      <ChartCard title="Memory (24h)" data={mem} suffix="%" loading={loading} />
    </div>
  );
}
function ChartCard({
  title,
  data,
  suffix,
  loading,
}: {
  title: string;
  data: number[];
  suffix: string;
  loading: boolean;
}) {
  const last = data.length > 0 ? data[data.length - 1] : null;
  return (
    <div className="rounded-xl border border-ink-800 bg-ink-900 p-4">
      <div className="flex items-baseline justify-between">
        <div className="text-sm text-slate-300">{title}</div>
        <div className="text-xs text-slate-500">
          {loading
            ? "loading…"
            : data.length === 0
              ? "no samples"
              : `now ${last?.toFixed(1)}${suffix}`}
        </div>
      </div>
      <Sparkline values={data} height={40} min={0} max={100} />
    </div>
  );
}

// ---- Interfaces table (the headline view for a switch) ------------------

type KindFilter = "physical" | "uplinks" | "all" | "logical";

function InterfacesTable({
  applianceId,
  interfaces,
}: {
  applianceId: string;
  interfaces: ApplianceInterface[];
}) {
  const [filter, setFilter] = useState("");
  const [hideDown, setHideDown] = useState(false);
  // Default to "physical" because that's what an operator means when they
  // say "ports". Uplinks are always pinned to the top regardless of this
  // filter (so they're visible even when the operator narrows to physical).
  const [kindFilter, setKindFilter] = useState<KindFilter>("physical");
  const [sortBy, setSortBy] = useState<"index" | "name" | "in" | "out">("index");
  const [expanded, setExpanded] = useState<number | null>(null);

  const counts = useMemo(() => {
    let phys = 0;
    let logical = 0;
    let uplinks = 0;
    for (const ifc of interfaces) {
      if (ifc.kind === "physical") phys++;
      else logical++;
      if (ifc.isUplink) uplinks++;
    }
    return { phys, logical, uplinks, total: interfaces.length };
  }, [interfaces]);

  const rows = useMemo(() => {
    let r = interfaces.filter((ifc) => {
      if (hideDown && !ifc.operUp) return false;
      switch (kindFilter) {
        case "physical":
          // Show physical ports, but never hide an uplink — port-channels
          // and 10G ports are usually classified non-physical and they're
          // exactly what an operator looking at "ports" wants to see.
          if (ifc.kind !== "physical" && !ifc.isUplink) return false;
          break;
        case "uplinks":
          if (!ifc.isUplink) return false;
          break;
        case "logical":
          if (ifc.kind === "physical") return false;
          break;
        case "all":
          break;
      }
      if (!filter) return true;
      const f = filter.toLowerCase();
      return (
        ifc.name?.toLowerCase().includes(f) ||
        ifc.descr?.toLowerCase().includes(f) ||
        (ifc.alias ?? "").toLowerCase().includes(f)
      );
    });
    r = [...r];
    switch (sortBy) {
      case "name":
        r.sort((a, b) => a.name.localeCompare(b.name, undefined, { numeric: true }));
        break;
      case "in":
        r.sort((a, b) => Number(b.inBps ?? 0) - Number(a.inBps ?? 0));
        break;
      case "out":
        r.sort((a, b) => Number(b.outBps ?? 0) - Number(a.outBps ?? 0));
        break;
      default:
        r.sort((a, b) => a.ifIndex - b.ifIndex);
    }
    // Uplinks always pin to the top so the most operationally important
    // ports stay above the fold no matter how the user sorts/filters.
    r.sort((a, b) => Number(b.isUplink ?? false) - Number(a.isUplink ?? false));
    return r;
  }, [interfaces, filter, hideDown, kindFilter, sortBy]);

  return (
    <div className="rounded-xl border border-ink-800 bg-ink-900">
      <div className="flex flex-wrap items-center gap-3 border-b border-ink-800 p-3">
        <div className="text-sm font-semibold">
          Interfaces{" "}
          <span className="font-normal text-slate-500">
            ({counts.phys} physical · {counts.logical} logical · {counts.uplinks} uplinks)
          </span>
        </div>
        <input
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          placeholder="filter by name / descr / alias…"
          className="ml-auto w-64 rounded-md border border-ink-700 bg-ink-950 px-2 py-1 text-xs"
        />
        <select
          value={kindFilter}
          onChange={(e) => setKindFilter(e.target.value as KindFilter)}
          className="rounded-md border border-ink-700 bg-ink-950 px-2 py-1 text-xs"
          title="Limit to physical ports (the default), uplinks only, all, or only logical (SVIs/loopbacks/etc.)"
        >
          <option value="physical">show: physical + uplinks</option>
          <option value="uplinks">show: uplinks only</option>
          <option value="logical">show: logical only</option>
          <option value="all">show: all ({counts.total})</option>
        </select>
        <label className="flex items-center gap-1 text-xs text-slate-400">
          <input
            type="checkbox"
            checked={hideDown}
            onChange={(e) => setHideDown(e.target.checked)}
          />
          hide down
        </label>
        <select
          value={sortBy}
          onChange={(e) => setSortBy(e.target.value as typeof sortBy)}
          className="rounded-md border border-ink-700 bg-ink-950 px-2 py-1 text-xs"
        >
          <option value="index">sort: index</option>
          <option value="name">sort: name</option>
          <option value="in">sort: in bps</option>
          <option value="out">sort: out bps</option>
        </select>
      </div>
      <div className="overflow-x-auto">
        <table className="w-full text-left text-sm">
          <thead className="bg-ink-800/40 text-xs uppercase tracking-wide text-slate-400">
            <tr>
              <th className="px-3 py-2">#</th>
              <th className="px-3 py-2">Name</th>
              <th className="px-3 py-2">Kind</th>
              <th className="px-3 py-2">Description / alias</th>
              <th className="px-3 py-2">Status</th>
              <th className="px-3 py-2 text-right">Speed</th>
              <th className="px-3 py-2 text-right">Last change</th>
              <th className="px-3 py-2 text-right">In</th>
              <th className="px-3 py-2 text-right">Out</th>
              <th className="px-3 py-2 text-right">Errors</th>
              <th className="px-3 py-2 text-right">Discards</th>
              <th className="px-3 py-2"></th>
            </tr>
          </thead>
          <tbody>
            {rows.length === 0 && (
              <tr>
                <td colSpan={12} className="px-3 py-6 text-center text-slate-500">
                  No interfaces match.
                </td>
              </tr>
            )}
            {rows.map((ifc) => (
              <Row
                key={ifc.ifIndex}
                applianceId={applianceId}
                ifc={ifc}
                expanded={expanded === ifc.ifIndex}
                onToggle={() =>
                  setExpanded(expanded === ifc.ifIndex ? null : ifc.ifIndex)
                }
              />
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function Row({
  applianceId,
  ifc,
  expanded,
  onToggle,
}: {
  applianceId: string;
  ifc: ApplianceInterface;
  expanded: boolean;
  onToggle: () => void;
}) {
  const errors = (ifc.inErrors ?? 0) + (ifc.outErrors ?? 0);
  const discards = (ifc.inDiscards ?? 0) + (ifc.outDiscards ?? 0);
  // Uplinks get a distinct row tint + a left-edge accent bar so they
  // stand out even in a long table.
  const rowClass = ifc.isUplink
    ? "border-t border-ink-800 bg-amber-950/10 hover:bg-amber-950/20"
    : "border-t border-ink-800 hover:bg-ink-800/30";
  return (
    <>
      <tr className={rowClass}>
        <td className="px-3 py-2 text-slate-500">
          {ifc.isUplink && (
            <span
              className="mr-1 inline-block h-3 w-1 rounded-sm bg-amber-400 align-middle"
              title="Uplink"
            />
          )}
          {ifc.ifIndex}
        </td>
        <td className="px-3 py-2 font-mono text-slate-200">
          {ifc.name}
          {ifc.isUplink && (
            <span
              className="ml-1.5 rounded bg-amber-500/20 px-1 py-0.5 text-[10px] font-semibold uppercase tracking-wide text-amber-200"
              title="Heuristic: high speed, alias contains uplink/trunk, port-channel, or LLDP neighbor present"
            >
              uplink
            </span>
          )}
        </td>
        <td className="px-3 py-2">
          <KindBadge kind={ifc.kind} />
        </td>
        <td className="px-3 py-2 text-slate-400">
          <div>{ifc.descr || "—"}</div>
          {ifc.alias && (
            <div className="text-xs text-slate-500">"{ifc.alias}"</div>
          )}
        </td>
        <td className="px-3 py-2">
          <StatusBadge admin={ifc.adminUp} oper={ifc.operUp} />
        </td>
        <td className="px-3 py-2 text-right text-slate-400">
          {ifc.speedBps ? formatBitRate(ifc.speedBps) : "—"}
        </td>
        <td
          className="px-3 py-2 text-right text-slate-400"
          title={
            ifc.lastChangeSeconds != null
              ? `${ifc.operUp ? "Up" : "Down"} for ${formatDuration(ifc.lastChangeSeconds)} (since last ifLastChange)`
              : "Device did not report ifLastChange for this port"
          }
        >
          {ifc.lastChangeSeconds == null ? (
            <span className="text-slate-600">—</span>
          ) : (
            <span className={ifc.operUp ? "text-emerald-300/80" : "text-red-300/80"}>
              {formatDuration(ifc.lastChangeSeconds)}
            </span>
          )}
        </td>
        <td className="px-3 py-2 text-right text-emerald-300">
          {ifc.inBps == null ? "—" : formatBitRate(Number(ifc.inBps))}
        </td>
        <td className="px-3 py-2 text-right text-sonar-300">
          {ifc.outBps == null ? "—" : formatBitRate(Number(ifc.outBps))}
        </td>
        <td className={`px-3 py-2 text-right ${errors > 0 ? "text-amber-300" : "text-slate-500"}`}>
          {errors}
        </td>
        <td className={`px-3 py-2 text-right ${discards > 0 ? "text-amber-300" : "text-slate-500"}`}>
          {discards}
        </td>
        <td className="px-3 py-2 text-right">
          <button
            type="button"
            onClick={onToggle}
            className="rounded border border-ink-700 px-2 py-0.5 text-xs text-slate-300 hover:bg-ink-800"
          >
            {expanded ? "hide" : "graph"}
          </button>
        </td>
      </tr>
      {expanded && (
        <tr className="bg-ink-950/50">
          <td colSpan={12} className="px-3 py-3">
            <IfaceSparkline applianceId={applianceId} ifIndex={ifc.ifIndex} />
          </td>
        </tr>
      )}
    </>
  );
}

function KindBadge({ kind }: { kind?: ApplianceInterface["kind"] }) {
  const k = kind ?? "other";
  const styles: Record<string, string> = {
    physical: "bg-slate-800 text-slate-300",
    vlan: "bg-indigo-900/40 text-indigo-200",
    loopback: "bg-slate-800 text-slate-400",
    tunnel: "bg-violet-900/40 text-violet-200",
    lag: "bg-amber-900/40 text-amber-200",
    mgmt: "bg-emerald-900/40 text-emerald-200",
    stack: "bg-cyan-900/40 text-cyan-200",
    other: "bg-slate-800 text-slate-500",
  };
  const cls = styles[k] ?? styles.other;
  return (
    <span className={`rounded px-1.5 py-0.5 text-[10px] font-medium uppercase tracking-wide ${cls}`}>
      {k}
    </span>
  );
}

function IfaceSparkline({
  applianceId,
  ifIndex,
}: {
  applianceId: string;
  ifIndex: number;
}) {
  const q = useQuery({
    queryKey: ["appliance-iface", applianceId, ifIndex, "24h"],
    queryFn: () =>
      api.get<ApplianceIfaceSeries>(
        `/appliances/${applianceId}/interfaces/${ifIndex}/metrics?range=24h`,
      ),
    refetchInterval: 60_000,
  });
  if (q.isLoading) return <div className="text-xs text-slate-500">Loading…</div>;
  if (q.isError) {
    return (
      <div className="text-xs text-red-300">
        Failed to load history: {(q.error as Error).message}
      </div>
    );
  }
  const samples = q.data?.samples ?? [];
  const inSeries = samples.map((s) => Number(s.inBps ?? 0));
  const outSeries = samples.map((s) => Number(s.outBps ?? 0));
  if (samples.length === 0) {
    return (
      <div className="text-xs text-slate-500">
        No samples yet — sparklines populate after the second poll cycle.
      </div>
    );
  }
  return (
    <div className="grid grid-cols-1 gap-3 md:grid-cols-2">
      <div>
        <div className="mb-1 text-xs text-emerald-300">in bps (24h)</div>
        <Sparkline
          values={inSeries}
          height={32}
          min={0}
          strokeClass="stroke-emerald-400"
          fillClass="fill-emerald-500/15"
        />
      </div>
      <div>
        <div className="mb-1 text-xs text-sonar-300">out bps (24h)</div>
        <Sparkline values={outSeries} height={32} min={0} />
      </div>
    </div>
  );
}

function StatusBadge({ admin, oper }: { admin: boolean; oper: boolean }) {
  if (oper) {
    return (
      <span className="rounded bg-emerald-900/40 px-1.5 py-0.5 text-[11px] text-emerald-300">
        up
      </span>
    );
  }
  if (!admin) {
    return (
      <span className="rounded bg-slate-800 px-1.5 py-0.5 text-[11px] text-slate-400">
        admin-down
      </span>
    );
  }
  return (
    <span className="rounded bg-red-900/40 px-1.5 py-0.5 text-[11px] text-red-300">
      down
    </span>
  );
}

// ---- OID pack metrics ----------------------------------------------------

function OIDMetricsTable({
  metrics,
}: {
  metrics: NonNullable<NonNullable<ApplianceSnapshot["vendor"]>["oidMetrics"]>;
}) {
  const [q, setQ] = useState("");
  const filtered = useMemo(() => {
    const needle = q.trim().toLowerCase();
    if (!needle) return metrics.slice(0, 200);
    return metrics
      .filter(
        (m) =>
          m.key.toLowerCase().includes(needle) ||
          m.packId.toLowerCase().includes(needle) ||
          (m.label || "").toLowerCase().includes(needle),
      )
      .slice(0, 200);
  }, [metrics, q]);

  return (
    <div className="rounded-xl border border-ink-800 bg-ink-900 p-4">
      <div className="mb-3 flex flex-wrap items-center justify-between gap-2">
        <div className="text-sm font-semibold text-slate-200">
          OID metrics{" "}
          <span className="font-normal text-slate-500">
            ({metrics.length} collected
            {metrics.length > 200 ? ", showing filtered/capped list" : ""})
          </span>
        </div>
        <input
          value={q}
          onChange={(e) => setQ(e.target.value)}
          placeholder="Filter key / pack…"
          className="rounded border border-ink-700 bg-ink-950 px-2 py-1 text-xs text-slate-200"
        />
      </div>
      <div className="max-h-96 overflow-auto">
        <table className="w-full text-left text-xs">
          <thead className="sticky top-0 bg-ink-900 text-slate-500">
            <tr>
              <th className="px-2 py-1">Pack</th>
              <th className="px-2 py-1">Key</th>
              <th className="px-2 py-1">Value</th>
              <th className="px-2 py-1">Unit</th>
            </tr>
          </thead>
          <tbody>
            {filtered.map((m) => (
              <tr key={m.key} className="border-t border-ink-800/80">
                <td className="px-2 py-1 font-mono text-slate-500">{m.packId}</td>
                <td className="px-2 py-1 font-mono text-slate-300" title={m.label || m.key}>
                  {m.key}
                </td>
                <td className="px-2 py-1 text-slate-200">
                  {m.text ? (
                    <span>
                      {m.text}{" "}
                      <span className="text-slate-500">({m.value})</span>
                    </span>
                  ) : (
                    m.value
                  )}
                </td>
                <td className="px-2 py-1 text-slate-500">{m.unit || "—"}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

// ---- Entities (chassis hardware inventory) ------------------------------

function EntitiesTable({ entities }: { entities: ApplianceEntity[] }) {
  return (
    <div className="rounded-xl border border-ink-800 bg-ink-900">
      <div className="border-b border-ink-800 p-3 text-sm font-semibold">
        Hardware inventory{" "}
        <span className="font-normal text-slate-500">
          ({entities.length} entities)
        </span>
      </div>
      <div className="overflow-x-auto">
        <table className="w-full text-left text-sm">
          <thead className="bg-ink-800/40 text-xs uppercase tracking-wide text-slate-400">
            <tr>
              <th className="px-3 py-2">#</th>
              <th className="px-3 py-2">Class</th>
              <th className="px-3 py-2">Description</th>
              <th className="px-3 py-2">Model</th>
              <th className="px-3 py-2">Serial</th>
              <th className="px-3 py-2">HW</th>
              <th className="px-3 py-2">FW</th>
              <th className="px-3 py-2">SW</th>
            </tr>
          </thead>
          <tbody>
            {entities.map((e) => (
              <tr key={e.index} className="border-t border-ink-800">
                <td className="px-3 py-2 text-slate-500">{e.index}</td>
                <td className="px-3 py-2 text-slate-400">{entityClass(e.class)}</td>
                <td className="px-3 py-2">{e.description}</td>
                <td className="px-3 py-2 font-mono text-slate-300">{e.modelName || "—"}</td>
                <td className="px-3 py-2 font-mono text-slate-400">{e.serial || "—"}</td>
                <td className="px-3 py-2 text-slate-500">{e.hardwareRev || "—"}</td>
                <td className="px-3 py-2 text-slate-500">{e.firmwareRev || "—"}</td>
                <td className="px-3 py-2 text-slate-500">{e.softwareRev || "—"}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

function entityClass(c: number): string {
  switch (c) {
    case 3:
      return "chassis";
    case 4:
      return "backplane";
    case 5:
      return "container";
    case 6:
      return "powerSupply";
    case 7:
      return "fan";
    case 8:
      return "sensor";
    case 9:
      return "module";
    case 10:
      return "port";
    default:
      return `class ${c}`;
  }
}

// ---- LLDP neighbors ------------------------------------------------------

function LLDPTable({ neighbors }: { neighbors: ApplianceLLDP[] }) {
  return (
    <div className="rounded-xl border border-ink-800 bg-ink-900">
      <div className="border-b border-ink-800 p-3 text-sm font-semibold">
        LLDP neighbors{" "}
        <span className="font-normal text-slate-500">
          ({neighbors.length})
        </span>
      </div>
      <div className="overflow-x-auto">
        <table className="w-full text-left text-sm">
          <thead className="bg-ink-800/40 text-xs uppercase tracking-wide text-slate-400">
            <tr>
              <th className="px-3 py-2">Local if</th>
              <th className="px-3 py-2">Remote system</th>
              <th className="px-3 py-2">Remote port</th>
              <th className="px-3 py-2">Chassis ID</th>
            </tr>
          </thead>
          <tbody>
            {neighbors.map((n, i) => (
              <tr key={`${n.localIfIndex}-${i}`} className="border-t border-ink-800">
                <td className="px-3 py-2 text-slate-500">{n.localIfIndex}</td>
                <td className="px-3 py-2">
                  <div className="text-slate-200">{n.remoteSysName || "—"}</div>
                  {n.remoteSysDescr && (
                    <div className="text-xs text-slate-500">{n.remoteSysDescr}</div>
                  )}
                </td>
                <td className="px-3 py-2 text-slate-300">
                  {n.remotePortDescr || n.remotePortId || "—"}
                </td>
                <td className="px-3 py-2 font-mono text-xs text-slate-500">
                  {n.remoteChassisId || "—"}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    </div>
  );
}

// ---- System metadata footer ---------------------------------------------

function SystemMeta({ detail }: { detail: ApplianceDetail }) {
  const snap = detail.lastSnapshot;
  if (!snap) return null;
  if (isMerakiDashboardSnapshot(snap)) {
    return (
      <div className="rounded-xl border border-ink-800 bg-ink-900 p-4 text-xs text-slate-400">
        <div className="mb-2 text-sm font-semibold text-slate-200">System</div>
        <div className="grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-3">
          <Meta label="name" value={snap.name || detail.sysName || detail.name || "—"} />
          <Meta label="productType" value={snap.productType || "—"} />
          <Meta label="model" value={snap.model || "—"} />
          <Meta label="status" value={snap.status || "—"} />
          <Meta label="firmware" value={snap.firmware?.current || "—"} />
          <Meta
            label="upgrade"
            value={
              snap.firmware?.nextUpgrade
                ? snap.firmware.nextUpgrade
                : snap.firmware?.status || "none"
            }
          />
          <Meta label="source" value={snap.source} />
          <Meta
            label="lastReported"
            value={snap.lastReportedAt ? formatRelative(snap.lastReportedAt) : "—"}
          />
          <Meta
            label="captured"
            value={snap.capturedAt ? formatRelative(snap.capturedAt) : "—"}
          />
        </div>
      </div>
    );
  }
  const snmp = snap as ApplianceSnapshot;
  return (
    <div className="rounded-xl border border-ink-800 bg-ink-900 p-4 text-xs text-slate-400">
      <div className="mb-2 text-sm font-semibold text-slate-200">System</div>
      <div className="grid grid-cols-1 gap-2 sm:grid-cols-2 lg:grid-cols-3">
        <Meta label="sysName" value={snmp.system.name} />
        <Meta label="sysDescr" value={snmp.system.description} />
        <Meta label="sysObjectID" value={snmp.system.objectId || "—"} />
        <Meta label="sysContact" value={snmp.system.contact || "—"} />
        <Meta label="sysLocation" value={snmp.system.location || "—"} />
        <Meta label="captured" value={`${snmp.collectMs} ms`} />
      </div>
    </div>
  );
}

function Meta({ label, value }: { label: string; value: string }) {
  return (
    <div>
      <div className="text-[10px] uppercase tracking-wide text-slate-500">
        {label}
      </div>
      <div className="break-all font-mono text-slate-300">{value}</div>
    </div>
  );
}

// ---- helpers -------------------------------------------------------------

// formatBitRate renders bps as "1.2 Gbps", "340 Mbps", "12 Kbps".
// Nothing in the codebase needed this before, so it lives here rather
// than format.ts; bytes-on-disk vs bits-on-the-wire are easy to confuse
// and we want them visibly separate.
function formatBitRate(bps: number): string {
  if (!bps || bps < 0) return "0 bps";
  if (bps < 1_000) return `${bps} bps`;
  if (bps < 1_000_000) return `${(bps / 1_000).toFixed(1)} Kbps`;
  if (bps < 1_000_000_000) return `${(bps / 1_000_000).toFixed(1)} Mbps`;
  if (bps < 1_000_000_000_000) return `${(bps / 1_000_000_000).toFixed(2)} Gbps`;
  return `${(bps / 1_000_000_000_000).toFixed(2)} Tbps`;
}

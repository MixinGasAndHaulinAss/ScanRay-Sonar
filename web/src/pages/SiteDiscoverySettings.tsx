import { useEffect, useMemo, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ApiError, api } from "../api/client";
import type { Site } from "../api/types";

interface SiteDiscoverySettings {
  siteId: string;
  subnets: string[];
  scanIntervalSeconds: number;
  subnetsPerPeriod: number;
  deviceOfflineDeleteDays: number;
  unidentifiedDeleteDays: number;
  configBackupIntervalSeconds: number;
  icmpTimeoutMs: number;
  cliFeatures: Record<string, unknown>;
  filterRules: { include?: string[]; exclude?: string[] };
}

const DEFAULTS: SiteDiscoverySettings = {
  siteId: "",
  subnets: [],
  scanIntervalSeconds: 3600,
  subnetsPerPeriod: 4,
  deviceOfflineDeleteDays: 30,
  unidentifiedDeleteDays: 7,
  configBackupIntervalSeconds: 86400,
  icmpTimeoutMs: 2000,
  cliFeatures: {},
  filterRules: { include: [], exclude: [] },
};

export default function SiteDiscoverySettingsPage() {
  const { siteId = "" } = useParams<{ siteId: string }>();
  const qc = useQueryClient();
  const sites = useQuery({ queryKey: ["sites"], queryFn: () => api.get<Site[]>("/sites") });
  const settings = useQuery({
    queryKey: ["site-discovery-settings", siteId],
    queryFn: () => api.get<SiteDiscoverySettings>(`/sites/${siteId}/discovery-settings`),
    enabled: !!siteId,
  });

  const site = useMemo(() => sites.data?.find((s) => s.id === siteId), [sites.data, siteId]);

  const [subnetsText, setSubnetsText] = useState("");
  const [scanInt, setScanInt] = useState(DEFAULTS.scanIntervalSeconds);
  const [spp, setSpp] = useState(DEFAULTS.subnetsPerPeriod);
  const [dod, setDod] = useState(DEFAULTS.deviceOfflineDeleteDays);
  const [udd, setUdd] = useState(DEFAULTS.unidentifiedDeleteDays);
  const [cbInt, setCbInt] = useState(DEFAULTS.configBackupIntervalSeconds);
  const [icmp, setIcmp] = useState(DEFAULTS.icmpTimeoutMs);
  const [includeText, setIncludeText] = useState("");
  const [excludeText, setExcludeText] = useState("");
  const [err, setErr] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);

  useEffect(() => {
    const d = settings.data;
    if (!d) return;
    setSubnetsText((d.subnets ?? []).join("\n"));
    setScanInt(d.scanIntervalSeconds);
    setSpp(d.subnetsPerPeriod);
    setDod(d.deviceOfflineDeleteDays);
    setUdd(d.unidentifiedDeleteDays);
    setCbInt(d.configBackupIntervalSeconds);
    setIcmp(d.icmpTimeoutMs);
    setIncludeText((d.filterRules?.include ?? []).join("\n"));
    setExcludeText((d.filterRules?.exclude ?? []).join("\n"));
  }, [settings.data]);

  const save = useMutation({
    mutationFn: () =>
      api.put<unknown>(`/sites/${siteId}/discovery-settings`, {
        subnets: parseLines(subnetsText),
        scanIntervalSeconds: scanInt,
        subnetsPerPeriod: spp,
        deviceOfflineDeleteDays: dod,
        unidentifiedDeleteDays: udd,
        configBackupIntervalSeconds: cbInt,
        icmpTimeoutMs: icmp,
        filterRules: {
          include: parseLines(includeText),
          exclude: parseLines(excludeText),
        },
      }),
    onSuccess: () => {
      setErr(null);
      setSaved(true);
      qc.invalidateQueries({ queryKey: ["site-discovery-settings", siteId] });
      setTimeout(() => setSaved(false), 2000);
    },
    onError: (e: unknown) => setErr(e instanceof ApiError ? e.message : "Save failed"),
  });

  if (!siteId) {
    return <div className="p-6 text-ink-300">Pick a site from <Link className="text-sonar-300 underline" to="/sites">Sites</Link>.</div>;
  }

  return (
    <div className="space-y-6 p-6">
      <header className="flex items-center justify-between">
        <div>
          <h1 className="text-2xl font-semibold">Discovery settings</h1>
          <p className="text-sm text-ink-400">{site?.name ?? siteId}</p>
        </div>
        <div className="flex gap-2 text-xs">
          <Link className="rounded border border-ink-700 px-2 py-1" to={`/sites/${siteId}/map`}>Network map</Link>
          <Link className="rounded border border-ink-700 px-2 py-1" to="/discovery">Org discovery</Link>
        </div>
      </header>

      <section className="space-y-3 rounded border border-ink-800 bg-ink-900/40 p-4">
        <h2 className="text-sm font-semibold text-ink-200">Subnets to sweep</h2>
        <p className="text-xs text-ink-400">One CIDR per line, e.g. <code>10.4.20.0/24</code>. Empty = no discovery.</p>
        <textarea
          value={subnetsText}
          onChange={(e) => setSubnetsText(e.target.value)}
          className="h-32 w-full rounded border border-ink-700 bg-ink-950 px-2 py-1 font-mono text-xs"
        />
      </section>

      <section className="grid grid-cols-2 gap-4 rounded border border-ink-800 bg-ink-900/40 p-4 md:grid-cols-3">
        <NumField label="Scan interval (s)" value={scanInt} onChange={setScanInt} min={60} />
        <NumField label="Subnets per period" value={spp} onChange={setSpp} min={1} />
        <NumField label="ICMP timeout (ms)" value={icmp} onChange={setIcmp} min={1} />
        <NumField label="Offline-delete (d)" value={dod} onChange={setDod} min={1} />
        <NumField label="Unidentified-delete (d)" value={udd} onChange={setUdd} min={1} />
        <NumField label="Config backup interval (s)" value={cbInt} onChange={setCbInt} min={300} />
      </section>

      <section className="grid gap-4 rounded border border-ink-800 bg-ink-900/40 p-4 md:grid-cols-2">
        <div className="space-y-2">
          <h2 className="text-sm font-semibold text-ink-200">Include filter</h2>
          <p className="text-xs text-ink-400">CIDR / regex / OUI / vendor / sysObjectID. One per line. Empty = match all.</p>
          <textarea
            value={includeText}
            onChange={(e) => setIncludeText(e.target.value)}
            className="h-28 w-full rounded border border-ink-700 bg-ink-950 px-2 py-1 font-mono text-xs"
          />
        </div>
        <div className="space-y-2">
          <h2 className="text-sm font-semibold text-ink-200">Exclude filter</h2>
          <p className="text-xs text-ink-400">Same syntax. Excludes win over includes.</p>
          <textarea
            value={excludeText}
            onChange={(e) => setExcludeText(e.target.value)}
            className="h-28 w-full rounded border border-ink-700 bg-ink-950 px-2 py-1 font-mono text-xs"
          />
        </div>
      </section>

      {err && <div className="rounded border border-red-800 bg-red-950/40 p-2 text-sm text-red-200">{err}</div>}
      {saved && <div className="rounded border border-emerald-800 bg-emerald-950/40 p-2 text-sm text-emerald-200">Saved.</div>}

      <div>
        <button
          onClick={() => save.mutate()}
          disabled={save.isPending}
          className="rounded bg-sonar-500 px-3 py-1.5 text-sm font-medium text-ink-950 hover:bg-sonar-400 disabled:opacity-50"
        >
          {save.isPending ? "Saving…" : "Save settings"}
        </button>
      </div>
    </div>
  );
}

function parseLines(s: string): string[] {
  return s
    .split(/\r?\n/)
    .map((x) => x.trim())
    .filter((x) => x.length > 0);
}

function NumField({ label, value, onChange, min }: { label: string; value: number; onChange: (n: number) => void; min: number }) {
  return (
    <label className="space-y-1 text-xs">
      <div className="text-ink-400">{label}</div>
      <input
        type="number"
        min={min}
        value={value}
        onChange={(e) => onChange(parseInt(e.target.value, 10) || min)}
        className="w-full rounded border border-ink-700 bg-ink-950 px-2 py-1 font-mono text-sm"
      />
    </label>
  );
}

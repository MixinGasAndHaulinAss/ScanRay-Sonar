// SiteDiscoveryAdmin — per-site discovery configuration. Two top-level
// tabs:
//
//   Manage Credentials  Manage SNMP / SSH / Telnet / WMI / WinAgent /
//                       VMware / generic API credentials. Each kind has
//                       its own form; everything serializes to a JSON
//                       blob that the collector parses (see
//                       internal/collector/discovery_poller.go
//                       parseCredSecret).
//
//   Discovery Settings  CIDR list, scan cadence, retention and filter
//                       rules — same form that lived here before. Now
//                       extracted into a tab.
//
// We deliberately never echo existing secrets back to the UI; Edit
// only lets the operator rotate the secret (paste a new one) or
// rename the credential. This avoids re-introducing plaintext into
// the wire on every edit.

import { useEffect, useMemo, useState } from "react";
import { Link, useParams } from "react-router-dom";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ApiError, api } from "../api/client";
import type { Site } from "../api/types";

type Tab = "credentials" | "settings";

export default function SiteDiscoveryAdmin() {
  const { siteId = "" } = useParams<{ siteId: string }>();
  const sites = useQuery({ queryKey: ["sites"], queryFn: () => api.get<Site[]>("/sites") });
  const site = useMemo(() => sites.data?.find((s) => s.id === siteId), [sites.data, siteId]);
  const [tab, setTab] = useState<Tab>("credentials");

  if (!siteId) {
    return (
      <div className="p-6 text-slate-300">
        Pick a site from{" "}
        <Link className="text-sonar-300 underline" to="/sites">
          Sites
        </Link>
        .
      </div>
    );
  }

  return (
    <div className="space-y-6">
      <header className="flex flex-wrap items-end justify-between gap-3">
        <div>
          <h1 className="text-2xl font-semibold tracking-tight">Discovery</h1>
          <p className="mt-0.5 text-xs text-slate-500">
            {site?.name ?? siteId} · per-site credential vault and scan configuration. Anything
            you save here is delivered to the collector(s) bound to this site over an
            authenticated WebSocket.
          </p>
        </div>
        <div className="flex gap-2 text-xs">
          <Link
            className="rounded-md border border-ink-700 px-2 py-1 text-slate-300 hover:bg-ink-800"
            to={`/sites/${siteId}/map`}
          >
            Network map
          </Link>
          <Link
            className="rounded-md border border-ink-700 px-2 py-1 text-slate-300 hover:bg-ink-800"
            to="/discovery"
          >
            Org discovery results
          </Link>
        </div>
      </header>

      <div className="border-b border-ink-800">
        <nav className="-mb-px flex gap-2">
          <TabButton active={tab === "credentials"} onClick={() => setTab("credentials")}>
            Manage credentials
          </TabButton>
          <TabButton active={tab === "settings"} onClick={() => setTab("settings")}>
            Discovery settings
          </TabButton>
        </nav>
      </div>

      {tab === "credentials" ? (
        <SiteCredentialsAdmin siteId={siteId} />
      ) : (
        <DiscoverySettingsForm siteId={siteId} />
      )}
    </div>
  );
}

function TabButton({
  active,
  children,
  onClick,
}: {
  active: boolean;
  children: React.ReactNode;
  onClick: () => void;
}) {
  return (
    <button
      onClick={onClick}
      className={
        "border-b-2 px-4 py-2 text-sm font-medium transition-colors " +
        (active
          ? "border-sonar-400 text-sonar-200"
          : "border-transparent text-slate-400 hover:border-ink-700 hover:text-slate-200")
      }
    >
      {children}
    </button>
  );
}

// --------------------------------------------------------------------
// Manage Credentials
// --------------------------------------------------------------------

type CredKind = "snmp" | "ssh" | "telnet" | "wmi" | "winagent" | "vmware" | "generic";

interface KindMeta {
  id: CredKind;
  label: string;
  protocol: string;
  desc: string;
}

const KINDS: KindMeta[] = [
  {
    id: "snmp",
    label: "SNMP",
    protocol: "SNMPv2c / v3",
    desc: "Community string or USM user used to poll switches, routers, firewalls and printers.",
  },
  {
    id: "ssh",
    label: "SSH login",
    protocol: "SSH/22",
    desc: "Username + password (or private key) used to issue `show version` and pull configs.",
  },
  {
    id: "telnet",
    label: "Telnet login",
    protocol: "Telnet/23",
    desc: "Legacy fallback when SSH is unavailable. Same vendor command map as SSH.",
  },
  {
    id: "wmi",
    label: "WMI",
    protocol: "WMI/RPC",
    desc: "Windows credentials for direct WMI queries. Prefer WinAgent for nonprivileged hosts.",
  },
  {
    id: "winagent",
    label: "WinAgent",
    protocol: "HTTPS/8443",
    desc: "Shared bearer token used to fetch inventory from the Sonar Windows sidecar.",
  },
  {
    id: "vmware",
    label: "VMware",
    protocol: "vSphere SOAP",
    desc: "vCenter / ESXi credentials. govmomi backend not yet wired — credentials are stored.",
  },
  {
    id: "generic",
    label: "Device API",
    protocol: "Custom",
    desc: "Free-form JSON for vendor APIs the built-in protocols don't cover yet.",
  },
];

interface CredRow {
  id: string;
  kind: CredKind;
  name: string;
  createdAt: string;
}

function SiteCredentialsAdmin({ siteId }: { siteId: string }) {
  const qc = useQueryClient();
  const [activeKind, setActiveKind] = useState<CredKind>("snmp");
  const [search, setSearch] = useState("");
  const [editing, setEditing] = useState<CredRow | null>(null);
  const [creating, setCreating] = useState(false);

  const creds = useQuery({
    queryKey: ["site-credentials", siteId],
    queryFn: () => api.get<CredRow[]>(`/sites/${siteId}/credentials`),
  });

  const del = useMutation({
    mutationFn: (id: string) => api.del<void>(`/sites/${siteId}/credentials/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["site-credentials", siteId] }),
  });

  const filtered = (creds.data ?? [])
    .filter((c) => c.kind === activeKind)
    .filter((c) => (search ? c.name.toLowerCase().includes(search.toLowerCase()) : true));

  const meta = KINDS.find((k) => k.id === activeKind)!;

  return (
    <div className="grid gap-4 lg:grid-cols-[16rem_minmax(0,1fr)]">
      <aside className="space-y-1 rounded-xl border border-ink-800 bg-ink-900/40 p-2">
        <h2 className="px-2 pt-1 pb-2 text-[10px] font-semibold uppercase tracking-wide text-slate-500">
          Credential type
        </h2>
        {KINDS.map((k) => {
          const count = creds.data?.filter((c) => c.kind === k.id).length ?? 0;
          return (
            <button
              key={k.id}
              onClick={() => setActiveKind(k.id)}
              className={
                "flex w-full items-center justify-between rounded-md px-3 py-2 text-left text-sm transition-colors " +
                (k.id === activeKind
                  ? "bg-sonar-500/15 text-sonar-200"
                  : "text-slate-300 hover:bg-ink-800/60")
              }
            >
              <span>{k.label}</span>
              <span
                className={
                  "rounded-full px-2 py-0.5 text-[10px] " +
                  (k.id === activeKind
                    ? "bg-sonar-500/20 text-sonar-100"
                    : "bg-ink-800 text-slate-500")
                }
              >
                {count}
              </span>
            </button>
          );
        })}
      </aside>

      <section className="space-y-3">
        <div className="flex flex-wrap items-center justify-between gap-3">
          <div>
            <h2 className="text-base font-semibold text-slate-100">{meta.label} credentials</h2>
            <p className="text-xs text-slate-500">{meta.desc}</p>
          </div>
          <button
            onClick={() => setCreating(true)}
            className="rounded-md bg-sonar-600 px-3 py-1.5 text-sm font-medium hover:bg-sonar-500"
          >
            Add {meta.label.toLowerCase()}
          </button>
        </div>

        <input
          placeholder={`Search ${meta.label} credentials…`}
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          className="w-full rounded-md border border-ink-700 bg-ink-950 px-3 py-2 text-sm"
        />

        <div className="overflow-hidden rounded-xl border border-ink-800 bg-ink-900">
          <table className="w-full text-left text-sm">
            <thead className="bg-ink-800/60 text-xs uppercase tracking-wide text-slate-400">
              <tr>
                <th className="px-4 py-2">Description</th>
                <th className="px-4 py-2">Protocol</th>
                <th className="px-4 py-2">Created</th>
                <th className="px-4 py-2 text-right">Actions</th>
              </tr>
            </thead>
            <tbody>
              {filtered.length === 0 && (
                <tr>
                  <td colSpan={4} className="px-4 py-6 text-center text-slate-500">
                    No {meta.label.toLowerCase()} credentials yet.
                  </td>
                </tr>
              )}
              {filtered.map((c) => (
                <tr key={c.id} className="border-t border-ink-800 hover:bg-ink-800/30">
                  <td className="px-4 py-2 font-medium text-slate-100">{c.name}</td>
                  <td className="px-4 py-2 text-slate-400">{meta.protocol}</td>
                  <td className="px-4 py-2 text-slate-500">
                    {new Date(c.createdAt).toLocaleDateString()}
                  </td>
                  <td className="px-4 py-2 text-right">
                    <div className="inline-flex gap-1">
                      <button
                        onClick={() => setEditing(c)}
                        className="rounded-md border border-ink-700 px-2 py-1 text-xs text-slate-200 hover:bg-ink-800"
                      >
                        Edit
                      </button>
                      <button
                        onClick={() => {
                          if (confirm(`Delete credential "${c.name}"?`)) del.mutate(c.id);
                        }}
                        className="rounded-md border border-ink-700 px-2 py-1 text-xs text-red-300 hover:bg-red-900/30"
                      >
                        Delete
                      </button>
                    </div>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>

        <p className="text-[11px] text-slate-500">
          Secrets are sealed with the central master key and never returned to the browser. The
          collector unseals them server-side at job dispatch time. Edit a credential to rotate
          its secret.
        </p>
      </section>

      {(creating || editing) && (
        <CredentialModal
          siteId={siteId}
          kind={editing?.kind ?? activeKind}
          editing={editing}
          onClose={() => {
            setCreating(false);
            setEditing(null);
          }}
        />
      )}
    </div>
  );
}

function CredentialModal({
  siteId,
  kind,
  editing,
  onClose,
}: {
  siteId: string;
  kind: CredKind;
  editing: CredRow | null;
  onClose: () => void;
}) {
  const qc = useQueryClient();
  const meta = KINDS.find((k) => k.id === kind)!;

  const [name, setName] = useState(editing?.name ?? "");
  const [fields, setFields] = useState<Record<string, string>>({});
  const [err, setErr] = useState<string | null>(null);

  const save = useMutation({
    mutationFn: () => {
      const secret = serializeSecret(kind, fields);
      if (editing) {
        const body: Record<string, string> = {};
        if (name && name !== editing.name) body.name = name;
        if (secret) body.secret = secret;
        if (!Object.keys(body).length) {
          throw new ApiError(400, "noop", "Nothing to update");
        }
        return api.patch<unknown>(`/sites/${siteId}/credentials/${editing.id}`, body);
      }
      if (!secret) throw new ApiError(400, "missing", "Secret fields are required");
      if (!name) throw new ApiError(400, "missing", "Name is required");
      return api.post<unknown>(`/sites/${siteId}/credentials`, {
        kind,
        name,
        secret,
      });
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["site-credentials", siteId] });
      onClose();
    },
    onError: (e: unknown) =>
      setErr(e instanceof ApiError ? e.message : "Save failed"),
  });

  return (
    <div className="fixed inset-0 z-30 grid place-items-center bg-black/60 px-4 backdrop-blur-sm">
      <form
        className="w-full max-w-lg space-y-3 rounded-xl border border-ink-800 bg-ink-900 p-5 shadow-2xl"
        onSubmit={(e) => {
          e.preventDefault();
          save.mutate();
        }}
      >
        <h3 className="text-lg font-semibold">
          {editing ? `Edit "${editing.name}"` : `New ${meta.label.toLowerCase()} credential`}
        </h3>
        <p className="text-xs text-slate-400">{meta.desc}</p>

        <label className="block text-xs text-slate-400">
          Description / name
          <input
            required={!editing}
            className="mt-1 w-full rounded-md border border-ink-700 bg-ink-950 px-3 py-2 text-sm"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="e.g. cisco-readonly"
          />
        </label>

        <CredentialFields kind={kind} fields={fields} setFields={setFields} editing={!!editing} />

        {err && <div className="text-xs text-red-300">{err}</div>}
        <div className="flex justify-end gap-2 pt-2">
          <button
            type="button"
            className="rounded-md border border-ink-700 px-3 py-1.5 text-sm hover:bg-ink-800"
            onClick={onClose}
          >
            Cancel
          </button>
          <button
            type="submit"
            disabled={save.isPending}
            className="rounded-md bg-sonar-600 px-3 py-1.5 text-sm font-medium hover:bg-sonar-500 disabled:opacity-50"
          >
            {save.isPending ? "Saving…" : editing ? "Save" : "Create"}
          </button>
        </div>
      </form>
    </div>
  );
}

function CredentialFields({
  kind,
  fields,
  setFields,
  editing,
}: {
  kind: CredKind;
  fields: Record<string, string>;
  setFields: (f: Record<string, string>) => void;
  editing: boolean;
}) {
  const set = (k: string, v: string) => setFields({ ...fields, [k]: v });
  const help = editing
    ? "Leave blank to keep the existing secret. Fill any field to rotate it."
    : "All fields are sealed server-side and never displayed again.";

  if (kind === "snmp") {
    const ver = fields.version || "v2c";
    return (
      <>
        <div className="grid grid-cols-2 gap-3">
          <label className="block text-xs text-slate-400">
            Version
            <select
              value={ver}
              onChange={(e) => set("version", e.target.value)}
              className="mt-1 w-full rounded-md border border-ink-700 bg-ink-950 px-3 py-2 text-sm"
            >
              <option value="v2c">v2c</option>
              <option value="v3">v3</option>
            </select>
          </label>
          {ver === "v2c" && (
            <Field label="Community" k="community" fields={fields} set={set} placeholder="public" />
          )}
        </div>
        {ver === "v3" && (
          <div className="space-y-2">
            <Field label="Username" k="username" fields={fields} set={set} />
            <div className="grid grid-cols-2 gap-3">
              <Field
                label="Auth protocol"
                k="authProto"
                fields={fields}
                set={set}
                placeholder="SHA"
              />
              <Field label="Auth password" k="authPass" fields={fields} set={set} type="password" />
              <Field
                label="Priv protocol"
                k="privProto"
                fields={fields}
                set={set}
                placeholder="AES"
              />
              <Field label="Priv password" k="privPass" fields={fields} set={set} type="password" />
            </div>
          </div>
        )}
        <p className="text-[11px] text-slate-500">{help}</p>
      </>
    );
  }

  if (kind === "ssh") {
    return (
      <>
        <Field label="Username" k="username" fields={fields} set={set} placeholder="admin" />
        <Field label="Password" k="password" fields={fields} set={set} type="password" />
        <label className="block text-xs text-slate-400">
          Private key (PEM, optional)
          <textarea
            rows={4}
            value={fields.key ?? ""}
            onChange={(e) => set("key", e.target.value)}
            className="mt-1 w-full rounded-md border border-ink-700 bg-ink-950 px-3 py-2 font-mono text-[11px]"
            placeholder="-----BEGIN OPENSSH PRIVATE KEY-----"
          />
        </label>
        <p className="text-[11px] text-slate-500">{help}</p>
      </>
    );
  }

  if (kind === "telnet") {
    return (
      <>
        <Field label="Username" k="username" fields={fields} set={set} />
        <Field label="Password" k="password" fields={fields} set={set} type="password" />
        <p className="text-[11px] text-slate-500">{help}</p>
      </>
    );
  }

  if (kind === "wmi") {
    return (
      <>
        <Field label="Domain\\username" k="username" fields={fields} set={set} />
        <Field label="Password" k="password" fields={fields} set={set} type="password" />
        <p className="text-[11px] text-slate-500">{help}</p>
      </>
    );
  }

  if (kind === "winagent") {
    return (
      <>
        <Field
          label="Shared bearer token"
          k="password"
          fields={fields}
          set={set}
          type="password"
          placeholder="The same token configured on the Windows sidecar"
        />
        <p className="text-[11px] text-slate-500">{help}</p>
      </>
    );
  }

  if (kind === "vmware") {
    return (
      <>
        <Field
          label="vCenter / ESXi host"
          k="host"
          fields={fields}
          set={set}
          placeholder="vcenter.example.com"
        />
        <Field
          label="Username"
          k="username"
          fields={fields}
          set={set}
          placeholder="user@vsphere.local"
        />
        <Field label="Password" k="password" fields={fields} set={set} type="password" />
        <p className="text-[11px] text-amber-400">
          govmomi backend is not yet wired in this build — credentials will be stored and surfaced
          to the collector but discovery via vCenter is queued behind Phase 2.
        </p>
      </>
    );
  }

  // generic
  return (
    <label className="block text-xs text-slate-400">
      Secret (free-form JSON or string)
      <textarea
        rows={5}
        value={fields.raw ?? ""}
        onChange={(e) => set("raw", e.target.value)}
        className="mt-1 w-full rounded-md border border-ink-700 bg-ink-950 px-3 py-2 font-mono text-[11px]"
        placeholder='{"apiKey":"…"}'
      />
      <span className="mt-1 block text-[11px] text-slate-500">{help}</span>
    </label>
  );
}

function Field({
  label,
  k,
  fields,
  set,
  type = "text",
  placeholder,
}: {
  label: string;
  k: string;
  fields: Record<string, string>;
  set: (k: string, v: string) => void;
  type?: string;
  placeholder?: string;
}) {
  return (
    <label className="block text-xs text-slate-400">
      {label}
      <input
        type={type}
        value={fields[k] ?? ""}
        onChange={(e) => set(k, e.target.value)}
        placeholder={placeholder}
        autoComplete="new-password"
        className="mt-1 w-full rounded-md border border-ink-700 bg-ink-950 px-3 py-2 text-sm"
      />
    </label>
  );
}

// serializeSecret produces the JSON string the collector expects for
// each credential kind. Empty result means "no fields filled in" so the
// caller can refuse to submit.
function serializeSecret(kind: CredKind, fields: Record<string, string>): string {
  const trim = (k: string) => (fields[k] ?? "").trim();
  switch (kind) {
    case "snmp": {
      const ver = trim("version") || "v2c";
      if (ver === "v2c") {
        const community = trim("community");
        if (!community) return "";
        return JSON.stringify({ version: "v2c", community });
      }
      const obj: Record<string, string> = { version: "v3" };
      for (const k of ["username", "authProto", "authPass", "privProto", "privPass"]) {
        if (trim(k)) obj[k] = trim(k);
      }
      return Object.keys(obj).length > 1 ? JSON.stringify(obj) : "";
    }
    case "ssh": {
      const obj: Record<string, string> = {};
      for (const k of ["username", "password", "key"]) if (trim(k)) obj[k] = trim(k);
      return Object.keys(obj).length ? JSON.stringify(obj) : "";
    }
    case "telnet":
    case "wmi": {
      const obj: Record<string, string> = {};
      for (const k of ["username", "password"]) if (trim(k)) obj[k] = trim(k);
      return Object.keys(obj).length ? JSON.stringify(obj) : "";
    }
    case "winagent": {
      const pw = trim("password");
      return pw ? JSON.stringify({ password: pw }) : "";
    }
    case "vmware": {
      const obj: Record<string, string> = {};
      for (const k of ["host", "username", "password"]) if (trim(k)) obj[k] = trim(k);
      return Object.keys(obj).length ? JSON.stringify(obj) : "";
    }
    case "generic":
      return trim("raw");
  }
}

// --------------------------------------------------------------------
// Discovery Settings (existing form, extracted into the second tab)
// --------------------------------------------------------------------

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

function DiscoverySettingsForm({ siteId }: { siteId: string }) {
  const qc = useQueryClient();
  const settings = useQuery({
    queryKey: ["site-discovery-settings", siteId],
    queryFn: () => api.get<SiteDiscoverySettings>(`/sites/${siteId}/discovery-settings`),
  });

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

  return (
    <div className="space-y-4">
      <section className="space-y-3 rounded-xl border border-ink-800 bg-ink-900/40 p-4">
        <h2 className="text-sm font-semibold text-slate-200">Subnets to sweep</h2>
        <p className="text-xs text-slate-400">
          One CIDR per line, e.g. <code className="font-mono">10.4.20.0/24</code>. Empty disables
          discovery for this site.
        </p>
        <textarea
          value={subnetsText}
          onChange={(e) => setSubnetsText(e.target.value)}
          className="h-32 w-full rounded-md border border-ink-700 bg-ink-950 px-2 py-1 font-mono text-xs"
        />
      </section>

      <section className="grid grid-cols-2 gap-4 rounded-xl border border-ink-800 bg-ink-900/40 p-4 md:grid-cols-3">
        <NumField label="Scan interval (s)" value={scanInt} onChange={setScanInt} min={60} />
        <NumField label="Subnets per period" value={spp} onChange={setSpp} min={1} />
        <NumField label="ICMP timeout (ms)" value={icmp} onChange={setIcmp} min={1} />
        <NumField label="Offline-delete (d)" value={dod} onChange={setDod} min={1} />
        <NumField label="Unidentified-delete (d)" value={udd} onChange={setUdd} min={1} />
        <NumField
          label="Config backup interval (s)"
          value={cbInt}
          onChange={setCbInt}
          min={300}
        />
      </section>

      <section className="grid gap-4 rounded-xl border border-ink-800 bg-ink-900/40 p-4 md:grid-cols-2">
        <div className="space-y-2">
          <h2 className="text-sm font-semibold text-slate-200">Include filter</h2>
          <p className="text-xs text-slate-400">
            CIDR / regex / OUI / vendor / sysObjectID. One per line. Empty = match all.
          </p>
          <textarea
            value={includeText}
            onChange={(e) => setIncludeText(e.target.value)}
            className="h-28 w-full rounded-md border border-ink-700 bg-ink-950 px-2 py-1 font-mono text-xs"
          />
        </div>
        <div className="space-y-2">
          <h2 className="text-sm font-semibold text-slate-200">Exclude filter</h2>
          <p className="text-xs text-slate-400">Same syntax. Excludes win over includes.</p>
          <textarea
            value={excludeText}
            onChange={(e) => setExcludeText(e.target.value)}
            className="h-28 w-full rounded-md border border-ink-700 bg-ink-950 px-2 py-1 font-mono text-xs"
          />
        </div>
      </section>

      {err && (
        <div className="rounded-md border border-red-800 bg-red-950/40 p-2 text-sm text-red-200">
          {err}
        </div>
      )}
      {saved && (
        <div className="rounded-md border border-emerald-800 bg-emerald-950/40 p-2 text-sm text-emerald-200">
          Saved.
        </div>
      )}

      <div>
        <button
          onClick={() => save.mutate()}
          disabled={save.isPending}
          className="rounded-md bg-sonar-500 px-3 py-1.5 text-sm font-medium text-ink-950 hover:bg-sonar-400 disabled:opacity-50"
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

function NumField({
  label,
  value,
  onChange,
  min,
}: {
  label: string;
  value: number;
  onChange: (n: number) => void;
  min: number;
}) {
  return (
    <label className="space-y-1 text-xs">
      <div className="text-slate-400">{label}</div>
      <input
        type="number"
        min={min}
        value={value}
        onChange={(e) => onChange(parseInt(e.target.value, 10) || min)}
        className="w-full rounded-md border border-ink-700 bg-ink-950 px-2 py-1 font-mono text-sm"
      />
    </label>
  );
}

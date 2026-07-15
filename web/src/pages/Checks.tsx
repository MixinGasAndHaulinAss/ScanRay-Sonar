import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useEffect, useMemo, useState } from "react";
import { Link, useSearchParams } from "react-router-dom";
import { ApiError, api } from "../api/client";
import type { Site } from "../api/types";

interface CheckType {
  id: string;
  title: string;
  mechanism: string;
  runner: string;
  params: { name: string; type: string; required?: boolean; default?: unknown; label?: string }[];
}

interface CheckRow {
  id: string;
  siteId: string;
  name: string;
  typeId: string;
  params: Record<string, unknown>;
  intervalSeconds: number;
  enabled: boolean;
  preferredRunner: string;
  assignedAgentId?: string;
  credentialId?: string;
  applianceId?: string;
  lastRunAt?: string;
  lastOk?: boolean;
  lastError?: string;
}

interface CredRow {
  id: string;
  kind: string;
  name: string;
}

const CENTRAL_TYPES = new Set(["sql_query", "smtp", "imap", "ldap_bind"]);

const CRED_KIND: Record<string, string> = {
  sql_query: "sql",
  smtp: "smtp",
  imap: "imap",
  ldap_bind: "ldap",
};

export default function Checks() {
  const qc = useQueryClient();
  const [sp] = useSearchParams();
  const sites = useQuery({ queryKey: ["sites"], queryFn: () => api.get<Site[]>("/sites") });
  const types = useQuery({ queryKey: ["check-types"], queryFn: () => api.get<CheckType[]>("/check-types") });
  const list = useQuery({ queryKey: ["checks"], queryFn: () => api.get<CheckRow[]>("/checks") });

  const [name, setName] = useState("");
  const [siteId, setSiteId] = useState(sp.get("siteId") || "");
  const [typeId, setTypeId] = useState(sp.get("typeId") || "icmp");
  const [host, setHost] = useState(sp.get("host") || "");
  const [url, setUrl] = useState(sp.get("url") || "");
  const [port, setPort] = useState(sp.get("port") || "443");
  const [query, setQuery] = useState("SELECT 1");
  const [driver, setDriver] = useState("postgres");
  const [tlsMode, setTlsMode] = useState("starttls");
  const [useTLS, setUseTLS] = useState(false);
  const [credentialId, setCredentialId] = useState("");
  const [runner, setRunner] = useState(sp.get("runner") || "auto");
  const [agentId, setAgentId] = useState(sp.get("agentId") || "");
  const [applianceId] = useState(sp.get("applianceId") || "");
  const [err, setErr] = useState<string | null>(null);

  const centralOnly = CENTRAL_TYPES.has(typeId);
  const credKind = CRED_KIND[typeId];

  useEffect(() => {
    if (centralOnly) setRunner("central");
  }, [centralOnly]);

  const creds = useQuery({
    queryKey: ["site-credentials", siteId],
    queryFn: () => api.get<CredRow[]>(`/sites/${siteId}/credentials`),
    enabled: !!siteId && !!credKind,
  });

  const selectedType = useMemo(
    () => types.data?.find((t) => t.id === typeId),
    [types.data, typeId],
  );

  const filteredCreds = useMemo(
    () => (creds.data || []).filter((c) => c.kind === credKind),
    [creds.data, credKind],
  );

  const create = useMutation({
    mutationFn: () => {
      const params: Record<string, unknown> = {};
      if (typeId === "http") {
        params.url = url || (host ? `https://${host}` : "");
      } else {
        params.host = host;
      }
      if (typeId === "tcp" || typeId === "tls") {
        params.port = Number(port) || (typeId === "tls" ? 443 : 0);
      }
      if (typeId === "tls" && host) params.sni = host;
      if (typeId === "sql_query") {
        params.port = Number(port) || 5432;
        params.driver = driver;
        params.query = query;
        params.credentialId = credentialId;
      }
      if (typeId === "smtp") {
        params.port = Number(port) || 587;
        params.tlsMode = tlsMode;
        params.credentialId = credentialId;
      }
      if (typeId === "imap") {
        params.port = Number(port) || 993;
        params.tlsMode = tlsMode === "starttls" ? "tls" : tlsMode;
        params.credentialId = credentialId;
      }
      if (typeId === "ldap_bind") {
        params.port = Number(port) || (useTLS ? 636 : 389);
        params.useTLS = useTLS;
        params.credentialId = credentialId;
      }
      return api.post<{ id: string }>("/checks", {
        siteId,
        name: name || `${typeId} ${host || url}`,
        typeId,
        params,
        preferredRunner: centralOnly ? "central" : runner,
        assignedAgentId: centralOnly ? undefined : agentId || undefined,
        applianceId: applianceId || undefined,
        credentialId: credentialId || undefined,
        intervalSeconds: 60,
      });
    },
    onSuccess: async () => {
      setErr(null);
      setName("");
      await qc.invalidateQueries({ queryKey: ["checks"] });
    },
    onError: (e: unknown) => setErr(e instanceof ApiError ? e.message : "Create failed"),
  });

  const del = useMutation({
    mutationFn: (id: string) => api.del(`/checks/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["checks"] }),
  });

  const toggle = useMutation({
    mutationFn: ({ id, enabled }: { id: string; enabled: boolean }) =>
      api.patch(`/checks/${id}`, { enabled }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["checks"] }),
  });

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-2xl font-semibold tracking-tight">Checks</h2>
        <p className="mt-0.5 text-xs text-slate-500">
          Synthetic monitors (ICMP/TCP/HTTP/DNS/TLS) plus vault-backed SQL / SMTP / IMAP / LDAP. Phase-1 types are
          agent-first; credentialed types always run on the central poller. Secrets live in the site credential vault —
          never in check params.
        </p>
      </div>

      <form
        className="space-y-3 rounded-xl border border-ink-800 bg-ink-900 p-4"
        onSubmit={(e) => {
          e.preventDefault();
          if (!siteId || !typeId) return;
          if (centralOnly && !credentialId) {
            setErr("Select a vault credential for this check type");
            return;
          }
          create.mutate();
        }}
      >
        <div className="flex flex-wrap gap-3">
          <label className="text-xs text-slate-400">
            Site
            <select
              className="mt-1 block rounded-md border border-ink-700 bg-ink-950 px-2 py-1.5 text-sm text-slate-100"
              value={siteId}
              onChange={(e) => {
                setSiteId(e.target.value);
                setCredentialId("");
              }}
              required
            >
              <option value="">Select…</option>
              {sites.data?.map((s) => (
                <option key={s.id} value={s.id}>
                  {s.name}
                </option>
              ))}
            </select>
          </label>
          <label className="text-xs text-slate-400">
            Type
            <select
              className="mt-1 block rounded-md border border-ink-700 bg-ink-950 px-2 py-1.5 text-sm text-slate-100"
              value={typeId}
              onChange={(e) => {
                setTypeId(e.target.value);
                setCredentialId("");
                const t = e.target.value;
                if (t === "sql_query") setPort("5432");
                else if (t === "smtp") {
                  setPort("587");
                  setTlsMode("starttls");
                } else if (t === "imap") {
                  setPort("993");
                  setTlsMode("tls");
                } else if (t === "ldap_bind") setPort("389");
                else if (t === "tls") setPort("443");
              }}
            >
              {(types.data || []).map((t) => (
                <option key={t.id} value={t.id}>
                  {t.title}
                </option>
              ))}
            </select>
          </label>
          <label className="text-xs text-slate-400">
            Runner
            <select
              className="mt-1 block rounded-md border border-ink-700 bg-ink-950 px-2 py-1.5 text-sm text-slate-100"
              value={centralOnly ? "central" : runner}
              disabled={centralOnly}
              onChange={(e) => setRunner(e.target.value)}
            >
              <option value="auto">auto (agent first)</option>
              <option value="agent">agent</option>
              <option value="central">central</option>
            </select>
          </label>
          <label className="text-xs text-slate-400">
            Name
            <input
              className="mt-1 block rounded-md border border-ink-700 bg-ink-950 px-2 py-1.5 text-sm text-slate-100"
              value={name}
              onChange={(e) => setName(e.target.value)}
              placeholder="optional"
            />
          </label>
        </div>
        <div className="flex flex-wrap gap-3">
          {typeId === "http" ? (
            <label className="text-xs text-slate-400">
              URL
              <input
                className="mt-1 block w-72 rounded-md border border-ink-700 bg-ink-950 px-2 py-1.5 text-sm text-slate-100"
                value={url}
                onChange={(e) => setUrl(e.target.value)}
                placeholder="https://example.com/health"
                required
              />
            </label>
          ) : (
            <label className="text-xs text-slate-400">
              Host
              <input
                className="mt-1 block w-56 rounded-md border border-ink-700 bg-ink-950 px-2 py-1.5 text-sm text-slate-100"
                value={host}
                onChange={(e) => setHost(e.target.value)}
                required
              />
            </label>
          )}
          {(typeId === "tcp" ||
            typeId === "tls" ||
            typeId === "sql_query" ||
            typeId === "smtp" ||
            typeId === "imap" ||
            typeId === "ldap_bind") && (
            <label className="text-xs text-slate-400">
              Port
              <input
                className="mt-1 block w-24 rounded-md border border-ink-700 bg-ink-950 px-2 py-1.5 text-sm text-slate-100"
                value={port}
                onChange={(e) => setPort(e.target.value)}
              />
            </label>
          )}
          {typeId === "sql_query" && (
            <>
              <label className="text-xs text-slate-400">
                Driver
                <select
                  className="mt-1 block rounded-md border border-ink-700 bg-ink-950 px-2 py-1.5 text-sm text-slate-100"
                  value={driver}
                  onChange={(e) => setDriver(e.target.value)}
                >
                  <option value="postgres">postgres</option>
                  <option value="sqlserver">sqlserver</option>
                  <option value="mysql">mysql</option>
                </select>
              </label>
              <label className="text-xs text-slate-400">
                Query
                <input
                  className="mt-1 block w-72 rounded-md border border-ink-700 bg-ink-950 px-2 py-1.5 font-mono text-sm text-slate-100"
                  value={query}
                  onChange={(e) => setQuery(e.target.value)}
                />
              </label>
            </>
          )}
          {(typeId === "smtp" || typeId === "imap") && (
            <label className="text-xs text-slate-400">
              TLS mode
              <select
                className="mt-1 block rounded-md border border-ink-700 bg-ink-950 px-2 py-1.5 text-sm text-slate-100"
                value={tlsMode}
                onChange={(e) => setTlsMode(e.target.value)}
              >
                <option value="starttls">starttls</option>
                <option value="tls">tls</option>
                <option value="plain">plain</option>
              </select>
            </label>
          )}
          {typeId === "ldap_bind" && (
            <label className="flex items-end gap-2 text-xs text-slate-400">
              <input type="checkbox" checked={useTLS} onChange={(e) => setUseTLS(e.target.checked)} />
              Use LDAPS
            </label>
          )}
          {centralOnly && (
            <label className="text-xs text-slate-400">
              Vault credential ({credKind})
              <select
                className="mt-1 block w-64 rounded-md border border-ink-700 bg-ink-950 px-2 py-1.5 text-sm text-slate-100"
                value={credentialId}
                onChange={(e) => setCredentialId(e.target.value)}
                required
              >
                <option value="">Select…</option>
                {filteredCreds.map((c) => (
                  <option key={c.id} value={c.id}>
                    {c.name}
                  </option>
                ))}
              </select>
              <span className="mt-1 block text-[10px] text-slate-500">
                Manage under Sites → Discovery → credentials (
                <Link className="text-sonar-400 hover:underline" to={siteId ? `/sites/${siteId}` : "/sites"}>
                  open site
                </Link>
                ).
              </span>
            </label>
          )}
          {!centralOnly && runner === "agent" && (
            <label className="text-xs text-slate-400">
              Agent ID
              <input
                className="mt-1 block w-72 rounded-md border border-ink-700 bg-ink-950 px-2 py-1.5 font-mono text-sm text-slate-100"
                value={agentId}
                onChange={(e) => setAgentId(e.target.value)}
                placeholder="optional UUID"
              />
            </label>
          )}
        </div>
        {selectedType && (
          <p className="text-[11px] text-slate-500">
            {selectedType.title} — pack runner {selectedType.runner}
            {centralOnly ? " (forced central; secrets never sent to probes)" : ""}
          </p>
        )}
        {err && <p className="text-xs text-rose-400">{err}</p>}
        <button
          type="submit"
          disabled={create.isPending}
          className="rounded-md bg-sonar-600 px-3 py-1.5 text-sm font-medium text-white hover:bg-sonar-500 disabled:opacity-50"
        >
          Add check
        </button>
      </form>

      <div className="overflow-hidden rounded-xl border border-ink-800">
        <table className="min-w-full text-left text-sm">
          <thead className="bg-ink-900 text-[11px] uppercase tracking-wide text-slate-500">
            <tr>
              <th className="px-3 py-2">Name</th>
              <th className="px-3 py-2">Type</th>
              <th className="px-3 py-2">Runner</th>
              <th className="px-3 py-2">Last</th>
              <th className="px-3 py-2">Status</th>
              <th className="px-3 py-2" />
            </tr>
          </thead>
          <tbody className="divide-y divide-ink-800">
            {(list.data || []).map((c) => (
              <tr key={c.id} className="bg-ink-950/40">
                <td className="px-3 py-2">
                  <div className="font-medium text-slate-100">{c.name}</div>
                  <div className="font-mono text-[10px] text-slate-500">{c.id}</div>
                </td>
                <td className="px-3 py-2 text-slate-300">{c.typeId}</td>
                <td className="px-3 py-2 text-slate-400">{c.preferredRunner}</td>
                <td className="px-3 py-2 text-xs text-slate-500">
                  {c.lastRunAt ? new Date(c.lastRunAt).toLocaleString() : "—"}
                </td>
                <td className="px-3 py-2">
                  {c.lastOk == null ? (
                    <span className="text-slate-500">pending</span>
                  ) : c.lastOk ? (
                    <span className="text-emerald-400">ok</span>
                  ) : (
                    <span className="text-rose-400" title={c.lastError || ""}>
                      fail
                    </span>
                  )}
                </td>
                <td className="space-x-2 px-3 py-2 text-right">
                  <button
                    type="button"
                    className="text-xs text-slate-400 hover:text-slate-200"
                    onClick={() => toggle.mutate({ id: c.id, enabled: !c.enabled })}
                  >
                    {c.enabled ? "Disable" : "Enable"}
                  </button>
                  <button
                    type="button"
                    className="text-xs text-rose-400 hover:text-rose-300"
                    onClick={() => {
                      if (window.confirm("Delete this check?")) del.mutate(c.id);
                    }}
                  >
                    Delete
                  </button>
                </td>
              </tr>
            ))}
            {!list.data?.length && (
              <tr>
                <td colSpan={6} className="px-3 py-8 text-center text-sm text-slate-500">
                  No checks yet.{" "}
                  <Link className="text-sonar-400 hover:underline" to="/documentation">
                    Docs
                  </Link>
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>
    </div>
  );
}

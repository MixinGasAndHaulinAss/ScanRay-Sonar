import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { ApiError, api } from "../api/client";
import type { Appliance, Site } from "../api/types";

type Vendor = Appliance["vendor"];
type SNMPVersion = Appliance["snmpVersion"];

const VENDORS: Vendor[] = ["meraki", "cisco", "aruba", "ubiquiti", "mikrotik", "generic"];

interface FormState {
  siteId: string;
  name: string;
  vendor: Vendor;
  model: string;
  serial: string;
  mgmtIp: string;
  snmpVersion: SNMPVersion;
  community: string;
  v3User: string;
  v3AuthProto: string;
  v3AuthPass: string;
  v3PrivProto: string;
  v3PrivPass: string;
  pollIntervalSeconds: number;
}

const EMPTY_FORM: FormState = {
  siteId: "",
  name: "",
  vendor: "generic",
  model: "",
  serial: "",
  mgmtIp: "",
  snmpVersion: "v2c",
  community: "",
  v3User: "",
  v3AuthProto: "SHA",
  v3AuthPass: "",
  v3PrivProto: "AES",
  v3PrivPass: "",
  pollIntervalSeconds: 60,
};

export default function Appliances() {
  const qc = useQueryClient();
  const appliances = useQuery({
    queryKey: ["appliances"],
    queryFn: () => api.get<Appliance[]>("/appliances"),
  });
  const sites = useQuery({ queryKey: ["sites"], queryFn: () => api.get<Site[]>("/sites") });

  const [open, setOpen] = useState(false);
  const [form, setForm] = useState<FormState>(EMPTY_FORM);
  const [err, setErr] = useState<string | null>(null);

  const create = useMutation({
    mutationFn: (b: FormState) => api.post<Appliance>("/appliances", b),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["appliances"] });
      setOpen(false);
      setForm(EMPTY_FORM);
      setErr(null);
    },
    onError: (e) => setErr(e instanceof ApiError ? e.message : "Failed to add appliance"),
  });

  const del = useMutation({
    mutationFn: (id: string) => api.del<void>(`/appliances/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["appliances"] }),
  });

  const siteName = (id: string) => sites.data?.find((s) => s.id === id)?.name ?? id.slice(0, 8);

  return (
    <div className="space-y-4">
      <div className="flex items-end justify-between gap-4">
        <div>
          <h2 className="text-2xl font-semibold tracking-tight">Appliances</h2>
          <p className="text-sm text-slate-400">
            Switches, routers, firewalls, and APs polled by the Sonar Poller. SNMP v1/v2c/v3
            credentials are encrypted at rest.
          </p>
        </div>
        <button
          className="rounded-md bg-sonar-600 px-3 py-1.5 text-sm font-medium hover:bg-sonar-500"
          onClick={() => {
            setForm({ ...EMPTY_FORM, siteId: sites.data?.[0]?.id ?? "" });
            setErr(null);
            setOpen(true);
          }}
        >
          New appliance
        </button>
      </div>
      <div className="overflow-hidden rounded-xl border border-ink-800 bg-ink-900">
        <table className="w-full text-left text-sm">
          <thead className="bg-ink-800/60 text-xs uppercase tracking-wide text-slate-400">
            <tr>
              <th className="px-4 py-2">Name</th>
              <th className="px-4 py-2">Site</th>
              <th className="px-4 py-2">Vendor</th>
              <th className="px-4 py-2">Mgmt IP</th>
              <th className="px-4 py-2">SNMP</th>
              <th className="px-4 py-2">Poll</th>
              <th className="px-4 py-2">Last polled</th>
              <th className="px-4 py-2 text-right">Actions</th>
            </tr>
          </thead>
          <tbody>
            {appliances.isLoading && (
              <tr>
                <td colSpan={8} className="px-4 py-6 text-center text-slate-500">
                  Loading…
                </td>
              </tr>
            )}
            {appliances.data?.length === 0 && (
              <tr>
                <td colSpan={8} className="px-4 py-6 text-center text-slate-500">
                  No appliances yet. Add switches, APs, or routers — supports SNMP v1, v2c, and v3.
                </td>
              </tr>
            )}
            {appliances.data?.map((a) => (
              <tr key={a.id} className="border-t border-ink-800 hover:bg-ink-800/30">
                <td className="px-4 py-2">{a.name}</td>
                <td className="px-4 py-2 text-slate-400">{siteName(a.siteId)}</td>
                <td className="px-4 py-2 text-slate-400">{a.vendor}</td>
                <td className="px-4 py-2 font-mono text-slate-300">{a.mgmtIp}</td>
                <td className="px-4 py-2 text-slate-400">{a.snmpVersion}</td>
                <td className="px-4 py-2 text-slate-400">{a.pollIntervalSeconds}s</td>
                <td className="px-4 py-2 text-slate-500">
                  {a.lastPolledAt ? new Date(a.lastPolledAt).toLocaleString() : "never"}
                </td>
                <td className="px-4 py-2 text-right">
                  <button
                    onClick={() => {
                      if (confirm(`Delete appliance "${a.name}"?`)) del.mutate(a.id);
                    }}
                    className="rounded-md border border-ink-700 px-2 py-1 text-xs text-red-300 hover:bg-red-900/30"
                  >
                    Delete
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {open && (
        <div className="fixed inset-0 z-20 grid place-items-center bg-black/60 px-4">
          <form
            className="w-full max-w-2xl space-y-3 rounded-xl border border-ink-800 bg-ink-900 p-5"
            onSubmit={(e) => {
              e.preventDefault();
              create.mutate(form);
            }}
          >
            <h3 className="text-lg font-semibold">New appliance</h3>

            <div className="grid grid-cols-2 gap-3">
              <label className="text-xs text-slate-400">
                Site
                <select
                  required
                  value={form.siteId}
                  onChange={(e) => setForm({ ...form, siteId: e.target.value })}
                  className="mt-1 w-full rounded-md border border-ink-700 bg-ink-950 px-3 py-2 text-sm text-slate-100"
                >
                  <option value="">— select site —</option>
                  {sites.data?.map((s) => (
                    <option key={s.id} value={s.id}>
                      {s.name}
                    </option>
                  ))}
                </select>
              </label>
              <label className="text-xs text-slate-400">
                Vendor
                <select
                  value={form.vendor}
                  onChange={(e) => setForm({ ...form, vendor: e.target.value as Vendor })}
                  className="mt-1 w-full rounded-md border border-ink-700 bg-ink-950 px-3 py-2 text-sm text-slate-100"
                >
                  {VENDORS.map((v) => (
                    <option key={v} value={v}>
                      {v}
                    </option>
                  ))}
                </select>
              </label>
              <label className="text-xs text-slate-400">
                Name
                <input
                  required
                  className="mt-1 w-full rounded-md border border-ink-700 bg-ink-950 px-3 py-2 text-sm"
                  value={form.name}
                  onChange={(e) => setForm({ ...form, name: e.target.value })}
                  placeholder="core-sw-01"
                />
              </label>
              <label className="text-xs text-slate-400">
                Mgmt IP
                <input
                  required
                  className="mt-1 w-full rounded-md border border-ink-700 bg-ink-950 px-3 py-2 font-mono text-sm"
                  value={form.mgmtIp}
                  onChange={(e) => setForm({ ...form, mgmtIp: e.target.value })}
                  placeholder="10.0.0.10"
                />
              </label>
              <label className="text-xs text-slate-400">
                Model
                <input
                  className="mt-1 w-full rounded-md border border-ink-700 bg-ink-950 px-3 py-2 text-sm"
                  value={form.model}
                  onChange={(e) => setForm({ ...form, model: e.target.value })}
                  placeholder="C9300-48P"
                />
              </label>
              <label className="text-xs text-slate-400">
                Serial
                <input
                  className="mt-1 w-full rounded-md border border-ink-700 bg-ink-950 px-3 py-2 text-sm"
                  value={form.serial}
                  onChange={(e) => setForm({ ...form, serial: e.target.value })}
                />
              </label>
              <label className="text-xs text-slate-400">
                SNMP version
                <select
                  value={form.snmpVersion}
                  onChange={(e) =>
                    setForm({ ...form, snmpVersion: e.target.value as SNMPVersion })
                  }
                  className="mt-1 w-full rounded-md border border-ink-700 bg-ink-950 px-3 py-2 text-sm"
                >
                  <option value="v1">v1</option>
                  <option value="v2c">v2c</option>
                  <option value="v3">v3</option>
                </select>
              </label>
              <label className="text-xs text-slate-400">
                Poll interval (seconds)
                <input
                  type="number"
                  min={15}
                  className="mt-1 w-full rounded-md border border-ink-700 bg-ink-950 px-3 py-2 text-sm"
                  value={form.pollIntervalSeconds}
                  onChange={(e) =>
                    setForm({ ...form, pollIntervalSeconds: Number(e.target.value) })
                  }
                />
              </label>
            </div>

            {(form.snmpVersion === "v1" || form.snmpVersion === "v2c") && (
              <label className="block text-xs text-slate-400">
                Community string
                <input
                  required
                  type="password"
                  className="mt-1 w-full rounded-md border border-ink-700 bg-ink-950 px-3 py-2 font-mono text-sm"
                  value={form.community}
                  onChange={(e) => setForm({ ...form, community: e.target.value })}
                  autoComplete="new-password"
                  placeholder="public"
                />
              </label>
            )}

            {form.snmpVersion === "v3" && (
              <div className="space-y-3 rounded-md border border-ink-800 bg-ink-950/50 p-3">
                <div className="text-xs uppercase tracking-wide text-slate-500">
                  SNMP v3 credentials
                </div>
                <div className="grid grid-cols-2 gap-3">
                  <label className="text-xs text-slate-400">
                    User
                    <input
                      required
                      className="mt-1 w-full rounded-md border border-ink-700 bg-ink-950 px-3 py-2 text-sm"
                      value={form.v3User}
                      onChange={(e) => setForm({ ...form, v3User: e.target.value })}
                    />
                  </label>
                  <label className="text-xs text-slate-400">
                    Auth protocol
                    <select
                      value={form.v3AuthProto}
                      onChange={(e) => setForm({ ...form, v3AuthProto: e.target.value })}
                      className="mt-1 w-full rounded-md border border-ink-700 bg-ink-950 px-3 py-2 text-sm"
                    >
                      <option>SHA</option>
                      <option>SHA256</option>
                      <option>SHA512</option>
                      <option>MD5</option>
                    </select>
                  </label>
                  <label className="col-span-2 text-xs text-slate-400">
                    Auth passphrase
                    <input
                      required
                      type="password"
                      autoComplete="new-password"
                      className="mt-1 w-full rounded-md border border-ink-700 bg-ink-950 px-3 py-2 font-mono text-sm"
                      value={form.v3AuthPass}
                      onChange={(e) => setForm({ ...form, v3AuthPass: e.target.value })}
                    />
                  </label>
                  <label className="text-xs text-slate-400">
                    Priv protocol
                    <select
                      value={form.v3PrivProto}
                      onChange={(e) => setForm({ ...form, v3PrivProto: e.target.value })}
                      className="mt-1 w-full rounded-md border border-ink-700 bg-ink-950 px-3 py-2 text-sm"
                    >
                      <option value="">(none)</option>
                      <option>AES</option>
                      <option>AES256</option>
                      <option>DES</option>
                    </select>
                  </label>
                  <label className="text-xs text-slate-400">
                    Priv passphrase
                    <input
                      type="password"
                      autoComplete="new-password"
                      className="mt-1 w-full rounded-md border border-ink-700 bg-ink-950 px-3 py-2 font-mono text-sm"
                      value={form.v3PrivPass}
                      onChange={(e) => setForm({ ...form, v3PrivPass: e.target.value })}
                    />
                  </label>
                </div>
              </div>
            )}

            {err && <div className="text-xs text-red-300">{err}</div>}
            <div className="flex justify-end gap-2 pt-2">
              <button
                type="button"
                className="rounded-md border border-ink-700 px-3 py-1.5 text-sm"
                onClick={() => setOpen(false)}
              >
                Cancel
              </button>
              <button
                type="submit"
                disabled={create.isPending}
                className="rounded-md bg-sonar-600 px-3 py-1.5 text-sm hover:bg-sonar-500 disabled:opacity-50"
              >
                {create.isPending ? "Saving…" : "Add appliance"}
              </button>
            </div>
          </form>
        </div>
      )}
    </div>
  );
}

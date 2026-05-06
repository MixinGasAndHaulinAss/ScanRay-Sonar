import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { ApiError, api } from "../api/client";
import type { Site } from "../api/types";

interface ApiKeyRow {
  id: string;
  name: string;
  scopes: string[];
  expiresAt?: string | null;
  createdAt: string;
}

function ScopeEditor({ keyId }: { keyId: string }) {
  const qc = useQueryClient();
  const sites = useQuery({ queryKey: ["sites"], queryFn: () => api.get<Site[]>("/sites") });
  const [picked, setPicked] = useState<Set<string>>(new Set());

  const saveSites = useMutation({
    mutationFn: (siteIds: string[]) => api.put(`/api-keys/${keyId}/sites`, { siteIds }),
    onSuccess: async () => {
      await qc.invalidateQueries({ queryKey: ["api-keys"] });
    },
  });

  const toggle = (id: string, on: boolean) => {
    setPicked((prev) => {
      const next = new Set(prev);
      if (on) next.add(id);
      else next.delete(id);
      return next;
    });
  };

  return (
    <div className="space-y-2">
      <div className="flex flex-wrap gap-2">
        {sites.data?.map((s) => (
          <label key={s.id} className="inline-flex items-center gap-1 text-[11px] text-slate-400">
            <input
              type="checkbox"
              className="accent-sonar-500"
              checked={picked.has(s.id)}
              onChange={(e) => toggle(s.id, e.target.checked)}
            />
            {s.name}
          </label>
        ))}
      </div>
      <div className="flex flex-wrap gap-2">
        <button
          type="button"
          disabled={saveSites.isPending}
          onClick={() => saveSites.mutate(Array.from(picked))}
          className="rounded-md border border-ink-700 px-2 py-1 text-[11px] text-slate-200 hover:bg-ink-800"
        >
          Apply site binding
        </button>
        <button
          type="button"
          disabled={saveSites.isPending}
          onClick={() => {
            setPicked(new Set());
            saveSites.mutate([]);
          }}
          className="rounded-md border border-ink-700 px-2 py-1 text-[11px] text-slate-400 hover:bg-ink-800"
        >
          Allow all sites
        </button>
      </div>
      <p className="text-[10px] text-slate-500">
        Checking sites then Apply restricts the key to those tenants. Allow all sites clears rows in{" "}
        <code className="font-mono">api_key_sites</code> so the key is not tenant-filtered. Existing bindings are not shown here yet — adjust by selecting sites and Apply.
      </p>
    </div>
  );
}

export default function ApiKeys() {
  const qc = useQueryClient();
  const keys = useQuery({
    queryKey: ["api-keys"],
    queryFn: () => api.get<ApiKeyRow[]>("/api-keys"),
  });

  const [name, setName] = useState("");
  const [err, setErr] = useState<string | null>(null);

  const createKey = useMutation({
    mutationFn: () => api.post<{ id: string; token: string }>("/api-keys", { name, scopes: [] }),
    onSuccess: async (data) => {
      window.alert(`Copy your token now — it will not be shown again:\n\n${data.token}`);
      setName("");
      setErr(null);
      await qc.invalidateQueries({ queryKey: ["api-keys"] });
    },
    onError: (e: unknown) =>
      setErr(e instanceof ApiError ? e.message : "Create failed"),
  });

  const revoke = useMutation({
    mutationFn: (id: string) => api.del(`/api-keys/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["api-keys"] }),
  });

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-2xl font-semibold tracking-tight">API keys</h2>
        <p className="mt-0.5 text-xs text-slate-500">
          Bearer tokens for automation. Optionally bind keys to specific sites for multi-tenant integrations.
        </p>
      </div>

      <form
        className="flex flex-wrap items-end gap-2 rounded-xl border border-ink-800 bg-ink-900 p-4"
        onSubmit={(e) => {
          e.preventDefault();
          if (!name.trim()) return;
          createKey.mutate();
        }}
      >
        <label className="block text-xs text-slate-400">
          New key name
          <input
            className="mt-1 w-56 rounded-md border border-ink-700 bg-ink-950 px-3 py-2 text-sm"
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="ci-readonly"
          />
        </label>
        <button
          type="submit"
          disabled={createKey.isPending || !name.trim()}
          className="rounded-full border border-sonar-700 bg-sonar-950/40 px-4 py-2 text-xs text-sonar-200 hover:bg-sonar-900/40 disabled:opacity-40"
        >
          Create
        </button>
        {err && <span className="text-xs text-red-400">{err}</span>}
      </form>

      <div className="overflow-hidden rounded-xl border border-ink-800 bg-ink-900">
        <table className="w-full text-left text-sm">
          <thead className="bg-ink-800/60 text-xs uppercase text-slate-400">
            <tr>
              <th className="px-3 py-2">Name</th>
              <th className="px-3 py-2">Scopes</th>
              <th className="px-3 py-2">Site scope</th>
              <th className="px-3 py-2 text-right">Actions</th>
            </tr>
          </thead>
          <tbody>
            {keys.data?.map((k) => (
              <tr key={k.id} className="border-t border-ink-800 align-top">
                <td className="px-3 py-2 font-medium">{k.name}</td>
                <td className="px-3 py-2 font-mono text-[11px] text-slate-400">
                  {k.scopes?.length ? k.scopes.join(", ") : "(none)"}
                </td>
                <td className="px-3 py-2">
                  <ScopeEditor keyId={k.id} />
                </td>
                <td className="px-3 py-2 text-right align-top">
                  <button
                    type="button"
                    className="text-xs text-red-400 hover:underline"
                    onClick={() => {
                      if (confirm(`Revoke API key "${k.name}"?`)) revoke.mutate(k.id);
                    }}
                  >
                    Revoke
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        {!keys.data?.length && !keys.isLoading && (
          <div className="px-4 py-8 text-center text-sm text-slate-500">No API keys yet.</div>
        )}
      </div>
    </div>
  );
}

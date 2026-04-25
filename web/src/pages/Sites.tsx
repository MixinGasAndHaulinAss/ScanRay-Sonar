import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { ApiError, api } from "../api/client";
import type { Site } from "../api/types";

export default function Sites() {
  const qc = useQueryClient();
  const sites = useQuery({ queryKey: ["sites"], queryFn: () => api.get<Site[]>("/sites") });
  const [open, setOpen] = useState(false);
  const [form, setForm] = useState({ slug: "", name: "", timezone: "UTC", description: "" });
  const [err, setErr] = useState<string | null>(null);

  const create = useMutation({
    mutationFn: (b: typeof form) => api.post<Site>("/sites", b),
    onSuccess: () => {
      setOpen(false);
      setForm({ slug: "", name: "", timezone: "UTC", description: "" });
      qc.invalidateQueries({ queryKey: ["sites"] });
    },
    onError: (e) => setErr(e instanceof ApiError ? e.message : "Failed"),
  });

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h2 className="text-2xl font-semibold tracking-tight">Sites</h2>
        <button
          className="rounded-md bg-sonar-600 px-3 py-1.5 text-sm font-medium hover:bg-sonar-500"
          onClick={() => setOpen(true)}
        >
          New site
        </button>
      </div>

      <div className="overflow-hidden rounded-xl border border-ink-800 bg-ink-900">
        <table className="w-full text-left text-sm">
          <thead className="bg-ink-800/60 text-xs uppercase tracking-wide text-slate-400">
            <tr>
              <th className="px-4 py-2">Name</th>
              <th className="px-4 py-2">Slug</th>
              <th className="px-4 py-2">Timezone</th>
              <th className="px-4 py-2">Created</th>
            </tr>
          </thead>
          <tbody>
            {sites.data?.map((s) => (
              <tr key={s.id} className="border-t border-ink-800 hover:bg-ink-800/30">
                <td className="px-4 py-2">{s.name}</td>
                <td className="px-4 py-2 text-slate-400">{s.slug}</td>
                <td className="px-4 py-2 text-slate-400">{s.timezone}</td>
                <td className="px-4 py-2 text-slate-500">{new Date(s.createdAt).toLocaleString()}</td>
              </tr>
            ))}
            {sites.data?.length === 0 && (
              <tr>
                <td colSpan={4} className="px-4 py-6 text-center text-slate-500">
                  No sites yet. Create one to get started.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>

      {open && (
        <div className="fixed inset-0 z-20 grid place-items-center bg-black/60 px-4">
          <form
            className="w-full max-w-md space-y-3 rounded-xl border border-ink-800 bg-ink-900 p-5"
            onSubmit={(e) => {
              e.preventDefault();
              create.mutate(form);
            }}
          >
            <h3 className="text-lg font-semibold">New site</h3>
            <input
              required
              placeholder="Slug (e.g. hq)"
              pattern="[a-z0-9-]+"
              className="w-full rounded-md border border-ink-700 bg-ink-950 px-3 py-2 text-sm"
              value={form.slug}
              onChange={(e) => setForm({ ...form, slug: e.target.value })}
            />
            <input
              required
              placeholder="Name"
              className="w-full rounded-md border border-ink-700 bg-ink-950 px-3 py-2 text-sm"
              value={form.name}
              onChange={(e) => setForm({ ...form, name: e.target.value })}
            />
            <input
              placeholder="Timezone (UTC)"
              className="w-full rounded-md border border-ink-700 bg-ink-950 px-3 py-2 text-sm"
              value={form.timezone}
              onChange={(e) => setForm({ ...form, timezone: e.target.value })}
            />
            <textarea
              placeholder="Description (optional)"
              rows={2}
              className="w-full rounded-md border border-ink-700 bg-ink-950 px-3 py-2 text-sm"
              value={form.description}
              onChange={(e) => setForm({ ...form, description: e.target.value })}
            />
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
                {create.isPending ? "Creating…" : "Create"}
              </button>
            </div>
          </form>
        </div>
      )}
    </div>
  );
}

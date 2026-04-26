import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { ApiError, api } from "../api/client";
import type { Appliance, Site } from "../api/types";
import { formatRelative } from "../lib/format";

interface FormState {
  slug: string;
  name: string;
  timezone: string;
  description: string;
}

const EMPTY: FormState = { slug: "", name: "", timezone: "UTC", description: "" };

export default function Sites() {
  const qc = useQueryClient();
  const sites = useQuery({ queryKey: ["sites"], queryFn: () => api.get<Site[]>("/sites") });
  const appliances = useQuery({
    queryKey: ["appliances"],
    queryFn: () => api.get<Appliance[]>("/appliances"),
  });

  const [open, setOpen] = useState(false);
  const [editing, setEditing] = useState<Site | null>(null);
  const [form, setForm] = useState<FormState>(EMPTY);
  const [err, setErr] = useState<string | null>(null);

  // Per-site appliance counts let operators see "this site has 8 switches"
  // without leaving the page, which is the most common reason they
  // hesitate before deleting.
  const counts = new Map<string, number>();
  for (const a of appliances.data ?? []) {
    counts.set(a.siteId, (counts.get(a.siteId) ?? 0) + 1);
  }

  function startCreate() {
    setEditing(null);
    setForm(EMPTY);
    setErr(null);
    setOpen(true);
  }
  function startEdit(s: Site) {
    setEditing(s);
    setForm({
      slug: s.slug,
      name: s.name,
      timezone: s.timezone,
      description: s.description ?? "",
    });
    setErr(null);
    setOpen(true);
  }

  const create = useMutation({
    mutationFn: (b: FormState) => api.post<Site>("/sites", b),
    onSuccess: () => {
      setOpen(false);
      setForm(EMPTY);
      qc.invalidateQueries({ queryKey: ["sites"] });
    },
    onError: (e) => setErr(e instanceof ApiError ? e.message : "Failed"),
  });

  const update = useMutation({
    mutationFn: ({ id, body }: { id: string; body: FormState }) =>
      api.patch<Site>(`/sites/${id}`, body),
    onSuccess: () => {
      setOpen(false);
      setEditing(null);
      qc.invalidateQueries({ queryKey: ["sites"] });
    },
    onError: (e) => setErr(e instanceof ApiError ? e.message : "Failed"),
  });

  const del = useMutation({
    mutationFn: (id: string) => api.del<void>(`/sites/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["sites"] }),
    onError: (e) => alert(e instanceof ApiError ? e.message : "Delete failed"),
  });

  return (
    <div className="space-y-4">
      <div className="flex items-end justify-between">
        <div>
          <h2 className="text-2xl font-semibold tracking-tight">Sites</h2>
          <p className="mt-1 text-sm text-slate-400">
            Logical containers for agents and appliances. Every device belongs to
            exactly one site.
          </p>
        </div>
        <button
          className="rounded-full bg-sonar-600 px-4 py-1.5 text-sm font-medium shadow-sm hover:bg-sonar-500"
          onClick={startCreate}
        >
          New site
        </button>
      </div>

      <div className="overflow-hidden rounded-xl border border-ink-800 bg-ink-900 shadow-sm">
        <table className="w-full text-left text-sm">
          <thead className="bg-ink-800/60 text-xs uppercase tracking-wide text-slate-400">
            <tr>
              <th className="px-4 py-2">Name</th>
              <th className="px-4 py-2">Slug</th>
              <th className="px-4 py-2">Timezone</th>
              <th className="px-4 py-2 text-right">Appliances</th>
              <th className="px-4 py-2">Created</th>
              <th className="px-4 py-2 text-right">Actions</th>
            </tr>
          </thead>
          <tbody>
            {sites.data?.map((s) => {
              const childCount = counts.get(s.id) ?? 0;
              return (
                <tr key={s.id} className="border-t border-ink-800 hover:bg-ink-800/30">
                  <td className="px-4 py-2">
                    <div className="text-slate-100">{s.name}</div>
                    {s.description && (
                      <div className="text-xs text-slate-500">{s.description}</div>
                    )}
                  </td>
                  <td className="px-4 py-2 font-mono text-slate-400">{s.slug}</td>
                  <td className="px-4 py-2 text-slate-400">{s.timezone}</td>
                  <td className="px-4 py-2 text-right text-slate-300">{childCount}</td>
                  <td className="px-4 py-2 text-slate-500">{formatRelative(s.createdAt)}</td>
                  <td className="px-4 py-2 text-right">
                    <div className="inline-flex items-center gap-2">
                      <button
                        onClick={() => startEdit(s)}
                        className="rounded-md border border-ink-700 px-2 py-1 text-xs text-slate-200 hover:bg-ink-800"
                      >
                        Edit
                      </button>
                      <button
                        onClick={() => {
                          if (childCount > 0) {
                            alert(
                              `Site "${s.name}" still has ${childCount} appliance${
                                childCount === 1 ? "" : "s"
                              }. Reassign or remove them first.`,
                            );
                            return;
                          }
                          if (confirm(`Delete site "${s.name}"?`)) del.mutate(s.id);
                        }}
                        className="rounded-md border border-ink-700 px-2 py-1 text-xs text-red-300 hover:bg-red-900/30"
                      >
                        Delete
                      </button>
                    </div>
                  </td>
                </tr>
              );
            })}
            {sites.data?.length === 0 && (
              <tr>
                <td colSpan={6} className="px-4 py-6 text-center text-slate-500">
                  No sites yet. Create one to get started.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>

      {open && (
        <div className="fixed inset-0 z-20 grid place-items-center bg-black/60 px-4 backdrop-blur-sm">
          <form
            className="w-full max-w-md space-y-3 rounded-xl border border-ink-800 bg-ink-900 p-5 shadow-2xl"
            onSubmit={(e) => {
              e.preventDefault();
              if (editing) {
                update.mutate({ id: editing.id, body: form });
              } else {
                create.mutate(form);
              }
            }}
          >
            <h3 className="text-lg font-semibold">
              {editing ? `Edit "${editing.name}"` : "New site"}
            </h3>
            <label className="block text-xs text-slate-400">
              Slug
              <input
                required
                placeholder="hq"
                pattern="[a-z0-9-]+"
                className="mt-1 w-full rounded-md border border-ink-700 bg-ink-950 px-3 py-2 font-mono text-sm"
                value={form.slug}
                onChange={(e) => setForm({ ...form, slug: e.target.value })}
              />
            </label>
            <label className="block text-xs text-slate-400">
              Name
              <input
                required
                className="mt-1 w-full rounded-md border border-ink-700 bg-ink-950 px-3 py-2 text-sm"
                value={form.name}
                onChange={(e) => setForm({ ...form, name: e.target.value })}
              />
            </label>
            <label className="block text-xs text-slate-400">
              Timezone
              <input
                placeholder="UTC"
                className="mt-1 w-full rounded-md border border-ink-700 bg-ink-950 px-3 py-2 text-sm"
                value={form.timezone}
                onChange={(e) => setForm({ ...form, timezone: e.target.value })}
              />
            </label>
            <label className="block text-xs text-slate-400">
              Description
              <textarea
                rows={2}
                className="mt-1 w-full rounded-md border border-ink-700 bg-ink-950 px-3 py-2 text-sm"
                value={form.description}
                onChange={(e) => setForm({ ...form, description: e.target.value })}
              />
            </label>
            {err && <div className="text-xs text-red-300">{err}</div>}
            <div className="flex justify-end gap-2 pt-2">
              <button
                type="button"
                className="rounded-md border border-ink-700 px-3 py-1.5 text-sm hover:bg-ink-800"
                onClick={() => setOpen(false)}
              >
                Cancel
              </button>
              <button
                type="submit"
                disabled={create.isPending || update.isPending}
                className="rounded-md bg-sonar-600 px-3 py-1.5 text-sm font-medium hover:bg-sonar-500 disabled:opacity-50"
              >
                {(create.isPending || update.isPending)
                  ? editing ? "Saving…" : "Creating…"
                  : editing ? "Save changes" : "Create"}
              </button>
            </div>
          </form>
        </div>
      )}
    </div>
  );
}

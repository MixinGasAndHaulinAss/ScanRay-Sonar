import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { ApiError, api, tokens } from "../api/client";
import type { Site, User } from "../api/types";
import { formatRelative } from "../lib/format";

interface DocRow {
  id: string;
  siteId: string;
  title: string;
  mimeType: string;
  sha256: string;
  sizeBytes: number;
  createdAt: string;
}

export default function SiteDocuments() {
  const qc = useQueryClient();
  const me = useQuery({ queryKey: ["me"], queryFn: () => api.get<User>("/auth/me") });
  const sites = useQuery({ queryKey: ["sites"], queryFn: () => api.get<Site[]>("/sites") });
  const canUpload = me.data?.role === "siteadmin" || me.data?.role === "superadmin";

  const [siteFilter, setSiteFilter] = useState("");
  const q = siteFilter ? `?siteId=${encodeURIComponent(siteFilter)}` : "";
  const docs = useQuery({
    queryKey: ["documents", siteFilter],
    queryFn: () => api.get<DocRow[]>(`/documents${q}`),
  });

  const [upSite, setUpSite] = useState("");
  const [upTitle, setUpTitle] = useState("");
  const [upMime, setUpMime] = useState("application/octet-stream");
  const [upB64, setUpB64] = useState("");
  const [upErr, setUpErr] = useState<string | null>(null);

  const upload = useMutation({
    mutationFn: () =>
      api.post(`/sites/${upSite}/documents`, {
        title: upTitle,
        mimeType: upMime,
        contentBase64: upB64.replace(/\s/g, ""),
      }),
    onSuccess: async () => {
      setUpTitle("");
      setUpB64("");
      setUpErr(null);
      await qc.invalidateQueries({ queryKey: ["documents"] });
    },
    onError: (e: unknown) =>
      setUpErr(e instanceof ApiError ? e.message : "Upload failed"),
  });

  async function downloadDoc(d: DocRow) {
    const t = tokens.get();
    const res = await fetch(`/api/v1/documents/${d.id}/download`, {
      headers: t?.accessToken ? { Authorization: `Bearer ${t.accessToken}` } : {},
    });
    if (!res.ok) {
      alert(`Download failed: ${res.status}`);
      return;
    }
    const blob = await res.blob();
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = d.title || "document";
    a.click();
    URL.revokeObjectURL(url);
  }

  return (
    <div className="space-y-6">
      <div>
        <h2 className="text-2xl font-semibold tracking-tight">Documents</h2>
        <p className="mt-0.5 text-xs text-slate-500">
          Site-scoped files stored encrypted at rest. Upload requires site administrator.
        </p>
      </div>

      <div className="flex flex-wrap items-center gap-3">
        <label className="text-xs text-slate-400">
          Filter site
          <select
            className="ml-2 rounded-md border border-ink-700 bg-ink-950 px-3 py-2 text-sm"
            value={siteFilter}
            onChange={(e) => setSiteFilter(e.target.value)}
          >
            <option value="">All permitted sites</option>
            {sites.data?.map((s) => (
              <option key={s.id} value={s.id}>
                {s.name}
              </option>
            ))}
          </select>
        </label>
      </div>

      {canUpload && (
        <form
          className="space-y-2 rounded-xl border border-ink-800 bg-ink-900 p-4"
          onSubmit={(e) => {
            e.preventDefault();
            if (!upSite || !upTitle || !upB64.trim()) return;
            upload.mutate();
          }}
        >
          <h3 className="text-sm font-semibold text-slate-200">Upload</h3>
          {upErr && <p className="text-xs text-red-400">{upErr}</p>}
          <div className="flex flex-wrap gap-2">
            <select
              required
              className="rounded-md border border-ink-700 bg-ink-950 px-3 py-2 text-sm"
              value={upSite}
              onChange={(e) => setUpSite(e.target.value)}
            >
              <option value="">Site…</option>
              {sites.data?.map((s) => (
                <option key={s.id} value={s.id}>
                  {s.name}
                </option>
              ))}
            </select>
            <input
              required
              placeholder="Title"
              className="rounded-md border border-ink-700 bg-ink-950 px-3 py-2 text-sm"
              value={upTitle}
              onChange={(e) => setUpTitle(e.target.value)}
            />
            <input
              placeholder="MIME type"
              className="rounded-md border border-ink-700 bg-ink-950 px-3 py-2 font-mono text-xs"
              value={upMime}
              onChange={(e) => setUpMime(e.target.value)}
            />
          </div>
          <textarea
            required
            placeholder="Base64 file contents"
            className="min-h-[100px] w-full rounded-md border border-ink-700 bg-ink-950 px-3 py-2 font-mono text-[11px]"
            value={upB64}
            onChange={(e) => setUpB64(e.target.value)}
          />
          <button
            type="submit"
            disabled={upload.isPending}
            className="rounded-full border border-sonar-700 bg-sonar-950/40 px-4 py-1.5 text-xs text-sonar-200 hover:bg-sonar-900/40 disabled:opacity-40"
          >
            Upload
          </button>
        </form>
      )}

      <div className="overflow-hidden rounded-xl border border-ink-800 bg-ink-900">
        <table className="w-full text-left text-sm">
          <thead className="bg-ink-800/60 text-xs uppercase text-slate-400">
            <tr>
              <th className="px-3 py-2">Title</th>
              <th className="px-3 py-2">Site</th>
              <th className="px-3 py-2">Size</th>
              <th className="px-3 py-2">Created</th>
              <th className="px-3 py-2 text-right">Download</th>
            </tr>
          </thead>
          <tbody>
            {docs.data?.map((d) => (
              <tr key={d.id} className="border-t border-ink-800">
                <td className="px-3 py-2">{d.title}</td>
                <td className="px-3 py-2 text-slate-400">
                  {sites.data?.find((s) => s.id === d.siteId)?.name ?? d.siteId.slice(0, 8)}
                </td>
                <td className="px-3 py-2 font-mono text-xs text-slate-400">{d.sizeBytes}</td>
                <td className="px-3 py-2 text-slate-500">{formatRelative(d.createdAt)}</td>
                <td className="px-3 py-2 text-right">
                  <button
                    type="button"
                    className="text-xs text-sonar-300 hover:underline"
                    onClick={() => downloadDoc(d)}
                  >
                    Download
                  </button>
                </td>
              </tr>
            ))}
          </tbody>
        </table>
        {!docs.data?.length && !docs.isLoading && (
          <div className="px-4 py-8 text-center text-sm text-slate-500">No documents.</div>
        )}
      </div>
    </div>
  );
}

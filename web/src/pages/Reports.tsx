// Reports — list of report templates and previously generated reports.
// Operators pick a template + site, click Generate, and the central
// Sonar API renders the Markdown server-side and persists it. Past
// reports are listed below; clicking one opens a modal-style preview
// that renders Markdown via react-markdown. Download serves
// `text/markdown` with a Content-Disposition filename so the browser
// can save it (and "Print → Save as PDF" gives an operator a PDF
// export without us shipping a renderer).

import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import ReactMarkdown from "react-markdown";
import remarkGfm from "remark-gfm";
import { api } from "../api/client";
import type { Site } from "../api/types";

interface ReportTemplate {
  slug: string;
  title: string;
  vendorScope: string;
  description: string;
}

interface ReportRow {
  id: number;
  templateSlug: string;
  siteId?: string;
  generatedAt: string;
  generatedBy: string;
  format: string;
  sizeBytes: number;
  metadata?: Record<string, unknown>;
}

interface ReportDetail extends ReportRow {
  content: string;
}

export default function Reports() {
  const qc = useQueryClient();
  const sites = useQuery({ queryKey: ["sites"], queryFn: () => api.get<Site[]>("/sites") });
  const templates = useQuery({
    queryKey: ["report-templates"],
    queryFn: () =>
      api
        .get<{ templates: ReportTemplate[] }>("/report-templates")
        .then((r) => r.templates),
  });
  const reports = useQuery({
    queryKey: ["reports"],
    queryFn: () =>
      api.get<{ reports: ReportRow[] }>("/reports").then((r) => r.reports),
  });

  const [siteId, setSiteId] = useState<string>("");
  const [slug, setSlug] = useState<string>("");
  const [openId, setOpenId] = useState<number | null>(null);

  const generate = useMutation({
    mutationFn: () =>
      api.post<{ id: number }>("/reports", { templateSlug: slug, siteId }),
    onSuccess: (r) => {
      qc.invalidateQueries({ queryKey: ["reports"] });
      setOpenId(r.id);
    },
  });

  const detail = useQuery({
    queryKey: ["report", openId],
    queryFn: () => api.get<ReportDetail>(`/reports/${openId}`),
    enabled: openId != null,
  });

  const slugTitle = useMemo(() => {
    const m = new Map<string, string>();
    templates.data?.forEach((t) => m.set(t.slug, t.title));
    return m;
  }, [templates.data]);

  const sitesById = useMemo(() => {
    const m = new Map<string, Site>();
    sites.data?.forEach((s) => m.set(s.id, s));
    return m;
  }, [sites.data]);

  return (
    <div className="space-y-8">
      <header>
        <h2 className="text-2xl font-semibold tracking-tight">Reports</h2>
        <p className="mt-0.5 text-xs text-slate-500">
          Generate Markdown summaries from the data Sonar already collects — UPS health,
          Synology fleet, switch inventory, site overviews. Save as PDF via your browser's
          print dialog.
        </p>
      </header>

      <section className="rounded-xl border border-ink-800 bg-ink-900 p-4">
        <h3 className="text-sm font-semibold text-slate-200">Generate a report</h3>
        <div className="mt-3 grid gap-3 md:grid-cols-3">
          <label className="space-y-1 text-xs">
            <span className="text-slate-400">Template</span>
            <select
              className="w-full rounded-md border border-ink-700 bg-ink-950 px-2 py-1.5 text-sm"
              value={slug}
              onChange={(e) => setSlug(e.target.value)}
            >
              <option value="">Pick a template…</option>
              {templates.data?.map((t) => (
                <option key={t.slug} value={t.slug}>
                  {t.title}
                </option>
              ))}
            </select>
          </label>
          <label className="space-y-1 text-xs">
            <span className="text-slate-400">Site</span>
            <select
              className="w-full rounded-md border border-ink-700 bg-ink-950 px-2 py-1.5 text-sm"
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
          <div className="flex items-end">
            <button
              className="rounded-md bg-sonar-500 px-4 py-1.5 text-sm font-medium text-white hover:bg-sonar-600 disabled:cursor-not-allowed disabled:opacity-50"
              disabled={!slug || !siteId || generate.isPending}
              onClick={() => generate.mutate()}
            >
              {generate.isPending ? "Generating…" : "Generate"}
            </button>
          </div>
        </div>
        {generate.error && (
          <p className="mt-2 text-xs text-red-400">{(generate.error as Error).message}</p>
        )}
      </section>

      <section className="space-y-2">
        <h3 className="text-sm font-semibold text-slate-200">Recent reports</h3>
        <div className="overflow-hidden rounded-xl border border-ink-800 bg-ink-900">
          <table className="w-full text-left text-sm">
            <thead className="bg-ink-800/60 text-xs uppercase text-slate-400">
              <tr>
                <th className="px-3 py-2">Generated</th>
                <th className="px-3 py-2">Template</th>
                <th className="px-3 py-2">Site</th>
                <th className="px-3 py-2">By</th>
                <th className="px-3 py-2">Size</th>
                <th className="px-3 py-2"></th>
              </tr>
            </thead>
            <tbody>
              {reports.data?.map((r) => (
                <tr key={r.id} className="border-t border-ink-800">
                  <td className="px-3 py-2 font-mono text-[11px] text-slate-400">
                    {new Date(r.generatedAt).toLocaleString()}
                  </td>
                  <td className="px-3 py-2">{slugTitle.get(r.templateSlug) ?? r.templateSlug}</td>
                  <td className="px-3 py-2">
                    {r.siteId ? sitesById.get(r.siteId)?.name ?? r.siteId : "—"}
                  </td>
                  <td className="px-3 py-2 text-slate-500">{r.generatedBy}</td>
                  <td className="px-3 py-2 text-slate-500">
                    {(r.sizeBytes / 1024).toFixed(1)} KiB
                  </td>
                  <td className="px-3 py-2 text-right">
                    <button
                      className="rounded-md border border-ink-700 px-2 py-0.5 text-xs hover:bg-ink-800"
                      onClick={() => setOpenId(r.id)}
                    >
                      View
                    </button>
                    <a
                      className="ml-2 rounded-md border border-ink-700 px-2 py-0.5 text-xs hover:bg-ink-800"
                      href={`/api/v1/reports/${r.id}/download`}
                      target="_blank"
                      rel="noreferrer"
                    >
                      Download
                    </a>
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          {!reports.data?.length && !reports.isLoading && (
            <div className="px-4 py-6 text-center text-sm text-slate-500">
              No reports yet.
            </div>
          )}
        </div>
      </section>

      {openId != null && (
        <ReportPreview
          report={detail.data}
          loading={detail.isLoading}
          onClose={() => setOpenId(null)}
        />
      )}
    </div>
  );
}

function ReportPreview({
  report,
  loading,
  onClose,
}: {
  report: ReportDetail | undefined;
  loading: boolean;
  onClose: () => void;
}) {
  return (
    <div className="fixed inset-0 z-30 flex items-center justify-center bg-black/60 p-4">
      <div className="flex h-[85vh] w-full max-w-4xl flex-col rounded-xl border border-ink-800 bg-ink-900">
        <div className="flex items-center justify-between border-b border-ink-800 px-4 py-2">
          <h4 className="text-sm font-semibold text-slate-200">
            {report ? `Report #${report.id} — ${report.templateSlug}` : "Loading…"}
          </h4>
          <div className="flex gap-2">
            {report && (
              <a
                className="rounded-md border border-ink-700 px-2 py-0.5 text-xs hover:bg-ink-800"
                href={`/api/v1/reports/${report.id}/download`}
                target="_blank"
                rel="noreferrer"
              >
                Download
              </a>
            )}
            <button
              className="rounded-md border border-ink-700 px-2 py-0.5 text-xs hover:bg-ink-800"
              onClick={() => window.print()}
            >
              Print / Save PDF
            </button>
            <button
              className="rounded-md border border-ink-700 px-2 py-0.5 text-xs hover:bg-ink-800"
              onClick={onClose}
            >
              Close
            </button>
          </div>
        </div>
        <div className="prose prose-invert max-w-none flex-1 overflow-auto px-6 py-4 text-sm prose-headings:text-slate-100 prose-table:text-slate-200">
          {loading && <p className="text-slate-500">Loading…</p>}
          {report && (
            <ReactMarkdown remarkPlugins={[remarkGfm]}>{report.content}</ReactMarkdown>
          )}
        </div>
      </div>
    </div>
  );
}

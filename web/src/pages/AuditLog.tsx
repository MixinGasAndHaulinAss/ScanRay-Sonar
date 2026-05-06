import { useQuery } from "@tanstack/react-query";
import { api } from "../api/client";
import type { User } from "../api/types";
import { formatRelative } from "../lib/format";

export default function AuditLog() {
  const me = useQuery({ queryKey: ["me"], queryFn: () => api.get<User>("/auth/me") });
  const ok = me.data?.role === "superadmin";

  const rows = useQuery({
    queryKey: ["audit-log"],
    queryFn: () => api.get<Record<string, unknown>[]>("/audit-log?limit=500"),
    enabled: !!me.data && ok,
  });

  if (!me.data) return <p className="text-sm text-slate-500">Loading…</p>;
  if (!ok) {
    return (
      <div className="rounded-lg border border-ink-800 bg-ink-900 px-4 py-6 text-sm text-slate-400">
        Audit log is restricted to super administrators.
      </div>
    );
  }

  return (
    <div className="space-y-4">
      <div>
        <h2 className="text-2xl font-semibold tracking-tight">Audit log</h2>
        <p className="mt-0.5 text-xs text-slate-500">
          Security-sensitive actions (credentials, documents, API keys, settings).
        </p>
      </div>

      <div className="overflow-hidden rounded-xl border border-ink-800 bg-ink-900">
        <table className="w-full text-left text-sm">
          <thead className="bg-ink-800/60 text-xs uppercase text-slate-400">
            <tr>
              <th className="px-3 py-2">When</th>
              <th className="px-3 py-2">Actor</th>
              <th className="px-3 py-2">Action</th>
              <th className="px-3 py-2">Target</th>
              <th className="px-3 py-2">IP</th>
            </tr>
          </thead>
          <tbody>
            {rows.data?.map((r) => (
              <tr key={String(r.id)} className="border-t border-ink-800 align-top">
                <td className="whitespace-nowrap px-3 py-2 text-slate-500">
                  {r.occurredAt ? formatRelative(String(r.occurredAt)) : "—"}
                </td>
                <td className="px-3 py-2 font-mono text-[11px] text-slate-400">
                  {String(r.actorKind ?? "")}
                  {r.actorId ? `:${String(r.actorId).slice(0, 8)}…` : ""}
                </td>
                <td className="px-3 py-2">{String(r.action ?? "")}</td>
                <td className="px-3 py-2 font-mono text-[11px] text-slate-400">
                  {r.targetKind ? `${String(r.targetKind)} ` : ""}
                  {r.targetId ? String(r.targetId) : ""}
                </td>
                <td className="px-3 py-2 font-mono text-[11px] text-slate-500">{String(r.ip ?? "—")}</td>
              </tr>
            ))}
          </tbody>
        </table>
        {!rows.data?.length && !rows.isLoading && (
          <div className="px-4 py-8 text-center text-sm text-slate-500">No audit rows.</div>
        )}
      </div>
    </div>
  );
}

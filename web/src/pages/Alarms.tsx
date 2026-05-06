import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { ApiError, api } from "../api/client";
import type { Site, User } from "../api/types";
import { formatRelative } from "../lib/format";

type Tab = "alarms" | "rules" | "channels";

export default function Alarms() {
  const qc = useQueryClient();
  const me = useQuery({ queryKey: ["me"], queryFn: () => api.get<User>("/auth/me") });
  const sites = useQuery({ queryKey: ["sites"], queryFn: () => api.get<Site[]>("/sites") });
  const canEdit = me.data?.role === "siteadmin" || me.data?.role === "superadmin";

  const [tab, setTab] = useState<Tab>("alarms");

  const alarms = useQuery({
    queryKey: ["alarms"],
    queryFn: () => api.get<Record<string, unknown>[]>("/alarms"),
  });

  const rules = useQuery({
    queryKey: ["alarm-rules"],
    queryFn: () => api.get<Record<string, unknown>[]>("/alarm-rules"),
  });

  const channels = useQuery({
    queryKey: ["notification-channels"],
    queryFn: () => api.get<Record<string, unknown>[]>("/notification-channels"),
  });

  const [ruleName, setRuleName] = useState("");
  const [ruleExpr, setRuleExpr] = useState("");
  const [ruleSev, setRuleSev] = useState("warning");
  const [ruleSite, setRuleSite] = useState("");
  const [ruleErr, setRuleErr] = useState<string | null>(null);

  const createRule = useMutation({
    mutationFn: () =>
      api.post("/alarm-rules", {
        name: ruleName,
        expression: ruleExpr,
        severity: ruleSev,
        siteId: ruleSite || null,
        channelIds: [],
      }),
    onSuccess: async () => {
      setRuleName("");
      setRuleExpr("");
      setRuleErr(null);
      await qc.invalidateQueries({ queryKey: ["alarm-rules"] });
    },
    onError: (e: unknown) =>
      setRuleErr(e instanceof ApiError ? e.message : "Create failed"),
  });

  const delRule = useMutation({
    mutationFn: (id: string) => api.del(`/alarm-rules/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["alarm-rules"] }),
  });

  const [chKind, setChKind] = useState("webhook");
  const [chName, setChName] = useState("");
  const [chUrl, setChUrl] = useState("");
  const [chSecret, setChSecret] = useState("");
  const [chErr, setChErr] = useState<string | null>(null);

  const createChannel = useMutation({
    mutationFn: () =>
      api.post("/notification-channels", {
        kind: chKind,
        name: chName,
        config: chUrl ? { url: chUrl } : {},
        signingSecret: chSecret || undefined,
      }),
    onSuccess: async () => {
      setChName("");
      setChUrl("");
      setChSecret("");
      setChErr(null);
      await qc.invalidateQueries({ queryKey: ["notification-channels"] });
    },
    onError: (e: unknown) =>
      setChErr(e instanceof ApiError ? e.message : "Create failed"),
  });

  const delChannel = useMutation({
    mutationFn: (id: string) => api.del(`/notification-channels/${id}`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["notification-channels"] }),
  });

  if (!me.data) return <p className="text-sm text-slate-500">Loading…</p>;

  return (
    <div className="space-y-4">
      <div>
        <h2 className="text-2xl font-semibold tracking-tight">Alarms</h2>
        <p className="mt-0.5 text-xs text-slate-500">
          Recent alarm events, metric expression rules, and notification channels.
        </p>
      </div>

      <div className="flex gap-1 rounded-full border border-ink-800 bg-ink-900 p-1 text-xs">
        {(["alarms", "rules", "channels"] as const).map((t) => (
          <button
            key={t}
            type="button"
            onClick={() => setTab(t)}
            className={
              tab === t
                ? "rounded-full bg-sonar-500/20 px-3 py-1 text-sonar-200"
                : "rounded-full px-3 py-1 text-slate-400 hover:text-slate-200"
            }
          >
            {t}
          </button>
        ))}
      </div>

      {tab === "alarms" && (
        <div className="overflow-hidden rounded-xl border border-ink-800 bg-ink-900">
          <table className="w-full text-left text-sm">
            <thead className="bg-ink-800/60 text-xs uppercase text-slate-400">
              <tr>
                <th className="px-3 py-2">Severity</th>
                <th className="px-3 py-2">Title</th>
                <th className="px-3 py-2">Target</th>
                <th className="px-3 py-2">Opened</th>
              </tr>
            </thead>
            <tbody>
              {alarms.data?.map((a) => (
                <tr key={String(a.id)} className="border-t border-ink-800">
                  <td className="px-3 py-2">{String(a.severity ?? "")}</td>
                  <td className="px-3 py-2">{String(a.title ?? "")}</td>
                  <td className="px-3 py-2 font-mono text-[11px] text-slate-400">
                    {String(a.targetKind ?? "")}:{String(a.targetId ?? "")}
                  </td>
                  <td className="px-3 py-2 text-slate-500">
                    {a.openedAt ? formatRelative(String(a.openedAt)) : "—"}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
          {!alarms.data?.length && !alarms.isLoading && (
            <div className="px-4 py-8 text-center text-sm text-slate-500">No alarms recorded.</div>
          )}
        </div>
      )}

      {tab === "rules" && (
        <div className="space-y-4">
          {canEdit && (
            <form
              className="space-y-2 rounded-xl border border-ink-800 bg-ink-900 p-4"
              onSubmit={(e) => {
                e.preventDefault();
                if (!ruleName.trim() || !ruleExpr.trim()) return;
                createRule.mutate();
              }}
            >
              <h3 className="text-sm font-semibold text-slate-200">New rule</h3>
              {ruleErr && <p className="text-xs text-red-400">{ruleErr}</p>}
              <div className="flex flex-wrap gap-2">
                <input
                  placeholder="Name"
                  className="rounded-md border border-ink-700 bg-ink-950 px-3 py-2 text-sm"
                  value={ruleName}
                  onChange={(e) => setRuleName(e.target.value)}
                />
                <select
                  className="rounded-md border border-ink-700 bg-ink-950 px-3 py-2 text-sm"
                  value={ruleSev}
                  onChange={(e) => setRuleSev(e.target.value)}
                >
                  <option value="info">info</option>
                  <option value="warning">warning</option>
                  <option value="critical">critical</option>
                </select>
                <select
                  className="rounded-md border border-ink-700 bg-ink-950 px-3 py-2 text-sm"
                  value={ruleSite}
                  onChange={(e) => setRuleSite(e.target.value)}
                >
                  <option value="">All sites</option>
                  {sites.data?.map((s) => (
                    <option key={s.id} value={s.id}>
                      {s.name}
                    </option>
                  ))}
                </select>
              </div>
              <textarea
                placeholder={'Expression e.g. device.cpuPct > 85 && device.memUsedRatio > 0.9'}
                className="min-h-[72px] w-full rounded-md border border-ink-700 bg-ink-950 px-3 py-2 font-mono text-xs"
                value={ruleExpr}
                onChange={(e) => setRuleExpr(e.target.value)}
              />
              <button
                type="submit"
                disabled={createRule.isPending}
                className="rounded-full border border-sonar-700 bg-sonar-950/40 px-4 py-1.5 text-xs text-sonar-200 hover:bg-sonar-900/40 disabled:opacity-40"
              >
                Create rule
              </button>
            </form>
          )}
          {!canEdit && (
            <p className="text-xs text-slate-500">Rule edits require site administrator.</p>
          )}
          <div className="overflow-hidden rounded-xl border border-ink-800 bg-ink-900">
            <table className="w-full text-left text-sm">
              <thead className="bg-ink-800/60 text-xs uppercase text-slate-400">
                <tr>
                  <th className="px-3 py-2">Name</th>
                  <th className="px-3 py-2">Severity</th>
                  <th className="px-3 py-2">Expression</th>
                  <th className="px-3 py-2 text-right">Actions</th>
                </tr>
              </thead>
              <tbody>
                {rules.data?.map((r) => (
                  <tr key={String(r.id)} className="border-t border-ink-800 align-top">
                    <td className="px-3 py-2">{String(r.name ?? "")}</td>
                    <td className="px-3 py-2">{String(r.severity ?? "")}</td>
                    <td className="max-w-xl px-3 py-2 font-mono text-[11px] text-slate-400">
                      {String(r.expression ?? "")}
                    </td>
                    <td className="px-3 py-2 text-right">
                      {canEdit && (
                        <button
                          type="button"
                          className="text-xs text-red-400 hover:underline"
                          onClick={() => {
                            if (confirm("Delete this alarm rule?")) delRule.mutate(String(r.id));
                          }}
                        >
                          Delete
                        </button>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}

      {tab === "channels" && (
        <div className="space-y-4">
          {canEdit && (
            <form
              className="space-y-2 rounded-xl border border-ink-800 bg-ink-900 p-4"
              onSubmit={(e) => {
                e.preventDefault();
                if (!chName.trim()) return;
                createChannel.mutate();
              }}
            >
              <h3 className="text-sm font-semibold text-slate-200">New channel</h3>
              {chErr && <p className="text-xs text-red-400">{chErr}</p>}
              <div className="flex flex-wrap gap-2">
                <select
                  className="rounded-md border border-ink-700 bg-ink-950 px-3 py-2 text-sm"
                  value={chKind}
                  onChange={(e) => setChKind(e.target.value)}
                >
                  <option value="webhook">webhook</option>
                  <option value="email">email</option>
                </select>
                <input
                  placeholder="Label"
                  className="rounded-md border border-ink-700 bg-ink-950 px-3 py-2 text-sm"
                  value={chName}
                  onChange={(e) => setChName(e.target.value)}
                />
                <input
                  placeholder="Webhook URL (stored in config)"
                  className="min-w-[240px] flex-1 rounded-md border border-ink-700 bg-ink-950 px-3 py-2 font-mono text-xs"
                  value={chUrl}
                  onChange={(e) => setChUrl(e.target.value)}
                />
                <input
                  placeholder="Signing secret"
                  type="password"
                  className="rounded-md border border-ink-700 bg-ink-950 px-3 py-2 text-sm"
                  value={chSecret}
                  onChange={(e) => setChSecret(e.target.value)}
                />
              </div>
              <button
                type="submit"
                disabled={createChannel.isPending}
                className="rounded-full border border-sonar-700 bg-sonar-950/40 px-4 py-1.5 text-xs text-sonar-200 hover:bg-sonar-900/40 disabled:opacity-40"
              >
                Create channel
              </button>
            </form>
          )}
          <div className="overflow-hidden rounded-xl border border-ink-800 bg-ink-900">
            <table className="w-full text-left text-sm">
              <thead className="bg-ink-800/60 text-xs uppercase text-slate-400">
                <tr>
                  <th className="px-3 py-2">Kind</th>
                  <th className="px-3 py-2">Name</th>
                  <th className="px-3 py-2">Active</th>
                  <th className="px-3 py-2 text-right">Actions</th>
                </tr>
              </thead>
              <tbody>
                {channels.data?.map((c) => (
                  <tr key={String(c.id)} className="border-t border-ink-800">
                    <td className="px-3 py-2">{String(c.kind ?? "")}</td>
                    <td className="px-3 py-2">{String(c.name ?? "")}</td>
                    <td className="px-3 py-2">{String(c.isActive ?? "")}</td>
                    <td className="px-3 py-2 text-right">
                      {canEdit && (
                        <button
                          type="button"
                          className="text-xs text-red-400 hover:underline"
                          onClick={() => {
                            if (confirm("Delete this notification channel?"))
                              delChannel.mutate(String(c.id));
                          }}
                        >
                          Delete
                        </button>
                      )}
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </div>
      )}
    </div>
  );
}

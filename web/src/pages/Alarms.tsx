import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { ApiError, api } from "../api/client";
import type { Site, User } from "../api/types";
import { formatRelative } from "../lib/format";

type Tab = "alarms" | "rules" | "channels";

type AlarmRule = {
  id: string;
  name?: string;
  severity?: string;
  expression?: string;
  siteId?: string | null;
  channelIds?: string[];
  forSeconds?: number;
  clearForSeconds?: number;
  enabled?: boolean;
  targetKind?: string;
};

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
    queryFn: () => api.get<AlarmRule[]>("/alarm-rules"),
  });

  const channels = useQuery({
    queryKey: ["notification-channels"],
    queryFn: () => api.get<Record<string, unknown>[]>("/notification-channels"),
  });

  const [ruleName, setRuleName] = useState("");
  const [ruleExpr, setRuleExpr] = useState("");
  const [ruleSev, setRuleSev] = useState("warning");
  const [ruleSite, setRuleSite] = useState("");
  const [ruleChannels, setRuleChannels] = useState<string[]>([]);
  const [ruleForSec, setRuleForSec] = useState("0");
  const [ruleClearSec, setRuleClearSec] = useState("0");
  const [ruleTargetKind, setRuleTargetKind] = useState("any");
  const [ruleErr, setRuleErr] = useState<string | null>(null);
  const [editingRule, setEditingRule] = useState<AlarmRule | null>(null);
  const [alarmTargetFilter, setAlarmTargetFilter] = useState<"all" | "agent" | "appliance">("all");

  const resetRuleForm = () => {
    setRuleName("");
    setRuleExpr("");
    setRuleSev("warning");
    setRuleSite("");
    setRuleChannels([]);
    setRuleForSec("0");
    setRuleClearSec("0");
    setRuleTargetKind("any");
    setEditingRule(null);
    setRuleErr(null);
  };

  const createRule = useMutation({
    mutationFn: () =>
      api.post("/alarm-rules", {
        name: ruleName,
        expression: ruleExpr,
        severity: ruleSev,
        siteId: ruleSite || null,
        channelIds: ruleChannels,
        forSeconds: parseInt(ruleForSec, 10) || 0,
        clearForSeconds: parseInt(ruleClearSec, 10) || 0,
        targetKind: ruleTargetKind,
      }),
    onSuccess: async () => {
      resetRuleForm();
      await qc.invalidateQueries({ queryKey: ["alarm-rules"] });
    },
    onError: (e: unknown) =>
      setRuleErr(e instanceof ApiError ? e.message : "Create failed"),
  });

  const patchRule = useMutation({
    mutationFn: (id: string) =>
      api.patch(`/alarm-rules/${id}`, {
        name: ruleName,
        expression: ruleExpr,
        severity: ruleSev,
        siteId: ruleSite || null,
        channelIds: ruleChannels,
        forSeconds: parseInt(ruleForSec, 10) || 0,
        clearForSeconds: parseInt(ruleClearSec, 10) || 0,
        targetKind: ruleTargetKind,
      }),
    onSuccess: async () => {
      resetRuleForm();
      await qc.invalidateQueries({ queryKey: ["alarm-rules"] });
    },
    onError: (e: unknown) =>
      setRuleErr(e instanceof ApiError ? e.message : "Update failed"),
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

  const ackAlarm = useMutation({
    mutationFn: (id: string | number) => api.post(`/alarms/${id}/ack`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["alarms"] }),
  });
  const clearAlarm = useMutation({
    mutationFn: (id: string | number) => api.post(`/alarms/${id}/clear`),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["alarms"] }),
  });

  const channelOptions = channels.data ?? [];

  function startEditRule(r: AlarmRule) {
    setEditingRule(r);
    setRuleName(String(r.name ?? ""));
    setRuleExpr(String(r.expression ?? ""));
    setRuleSev(String(r.severity ?? "warning"));
    setRuleSite(String(r.siteId ?? ""));
    setRuleChannels(r.channelIds ?? []);
    setRuleForSec(String(r.forSeconds ?? 0));
    setRuleClearSec(String(r.clearForSeconds ?? 0));
    setRuleTargetKind(String(r.targetKind ?? "any"));
    setRuleErr(null);
  }

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
        <div className="space-y-3">
          <div className="flex gap-2 text-xs">
            {(["all", "agent", "appliance"] as const).map((f) => (
              <button
                key={f}
                type="button"
                onClick={() => setAlarmTargetFilter(f)}
                className={
                  "rounded-full px-3 py-1 " +
                  (alarmTargetFilter === f
                    ? "bg-sonar-500/20 text-sonar-200"
                    : "bg-ink-800 text-slate-400 hover:text-slate-200")
                }
              >
                {f === "all" ? "All targets" : f}
              </button>
            ))}
          </div>
        <div className="overflow-hidden rounded-xl border border-ink-800 bg-ink-900">
          <table className="w-full text-left text-sm">
            <thead className="bg-ink-800/60 text-xs uppercase text-slate-400">
              <tr>
                <th className="px-3 py-2">Severity</th>
                <th className="px-3 py-2">Title</th>
                <th className="px-3 py-2">Target</th>
                <th className="px-3 py-2">Opened</th>
                <th className="px-3 py-2">State</th>
                <th className="px-3 py-2 text-right">Actions</th>
              </tr>
            </thead>
            <tbody>
              {alarms.data
                ?.filter(
                  (a) =>
                    alarmTargetFilter === "all" ||
                    String(a.targetKind ?? "") === alarmTargetFilter,
                )
                .map((a) => {
                const cleared = a.clearedAt as string | null | undefined;
                const acked = a.ackedAt as string | null | undefined;
                const auto = a.autoCleared as boolean | undefined;
                let state = "open";
                if (cleared) state = auto ? "auto-cleared" : "cleared";
                else if (acked) state = "acked";
                return (
                  <tr key={String(a.id)} className="border-t border-ink-800">
                    <td className="px-3 py-2">{String(a.severity ?? "")}</td>
                    <td className="px-3 py-2">{String(a.title ?? "")}</td>
                    <td className="px-3 py-2 font-mono text-[11px] text-slate-400">
                      {String(a.targetKind ?? "")}:{String(a.targetId ?? "")}
                    </td>
                    <td className="px-3 py-2 text-slate-500">
                      {a.openedAt ? formatRelative(String(a.openedAt)) : "—"}
                    </td>
                    <td className="px-3 py-2 text-slate-400">{state}</td>
                    <td className="px-3 py-2 text-right">
                      {canEdit && !cleared && (
                        <div className="flex justify-end gap-2 text-xs">
                          {!acked && (
                            <button
                              type="button"
                              className="text-sonar-300 hover:underline"
                              onClick={() => ackAlarm.mutate(String(a.id))}
                            >
                              Ack
                            </button>
                          )}
                          <button
                            type="button"
                            className="text-red-400 hover:underline"
                            onClick={() => clearAlarm.mutate(String(a.id))}
                          >
                            Clear
                          </button>
                        </div>
                      )}
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
          {!alarms.data?.length && !alarms.isLoading && (
            <div className="px-4 py-8 text-center text-sm text-slate-500">No alarms recorded.</div>
          )}
        </div>
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
                if (editingRule) patchRule.mutate(editingRule.id);
                else createRule.mutate();
              }}
            >
              <h3 className="text-sm font-semibold text-slate-200">
                {editingRule ? "Edit rule" : "New rule"}
              </h3>
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
                <select
                  className="rounded-md border border-ink-700 bg-ink-950 px-3 py-2 text-sm"
                  value={ruleTargetKind}
                  onChange={(e) => setRuleTargetKind(e.target.value)}
                  title="Limit rule to appliances, agents, or both"
                >
                  <option value="any">any target</option>
                  <option value="agent">agent only</option>
                  <option value="appliance">appliance only</option>
                </select>
                <input
                  type="number"
                  min={0}
                  placeholder="forSeconds"
                  title="Predicate must hold for N seconds before opening"
                  className="w-28 rounded-md border border-ink-700 bg-ink-950 px-3 py-2 text-sm"
                  value={ruleForSec}
                  onChange={(e) => setRuleForSec(e.target.value)}
                />
                <input
                  type="number"
                  min={0}
                  placeholder="clearForSeconds"
                  title="Predicate must be false for N seconds before auto-clear"
                  className="w-36 rounded-md border border-ink-700 bg-ink-950 px-3 py-2 text-sm"
                  value={ruleClearSec}
                  onChange={(e) => setRuleClearSec(e.target.value)}
                />
              </div>
              <div>
                <label className="mb-1 block text-xs text-slate-400">Notification channels</label>
                <select
                  multiple
                  className="min-h-[72px] w-full rounded-md border border-ink-700 bg-ink-950 px-3 py-2 text-sm"
                  value={ruleChannels}
                  onChange={(e) =>
                    setRuleChannels(Array.from(e.target.selectedOptions, (o) => o.value))
                  }
                >
                  {channelOptions.map((c) => (
                    <option key={String(c.id)} value={String(c.id)}>
                      {String(c.kind)} — {String(c.name)}
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
              <p className="text-[11px] text-slate-500">
                Agent fields: cpuPct, memUsedRatio, diskUsedRatio, score, bsod24h, missingPatchCount,
                pendingReboot, batteryHealthPct, wifiRssi, uptimeSec. Example:{" "}
                <code className="text-slate-400">device.score &lt; 5</code>
              </p>
              <div className="flex gap-2">
                <button
                  type="submit"
                  disabled={createRule.isPending || patchRule.isPending}
                  className="rounded-full border border-sonar-700 bg-sonar-950/40 px-4 py-1.5 text-xs text-sonar-200 hover:bg-sonar-900/40 disabled:opacity-40"
                >
                  {editingRule ? "Save rule" : "Create rule"}
                </button>
                {editingRule && (
                  <button
                    type="button"
                    onClick={resetRuleForm}
                    className="rounded-full border border-ink-700 px-4 py-1.5 text-xs text-slate-400"
                  >
                    Cancel
                  </button>
                )}
              </div>
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
                  <th className="px-3 py-2">Target</th>
                  <th className="px-3 py-2">For / Clear</th>
                  <th className="px-3 py-2">Expression</th>
                  <th className="px-3 py-2 text-right">Actions</th>
                </tr>
              </thead>
              <tbody>
                {rules.data?.map((r) => (
                  <tr key={String(r.id)} className="border-t border-ink-800 align-top">
                    <td className="px-3 py-2">{String(r.name ?? "")}</td>
                    <td className="px-3 py-2">{String(r.severity ?? "")}</td>
                    <td className="px-3 py-2 text-slate-400">{String(r.targetKind ?? "any")}</td>
                    <td className="px-3 py-2 font-mono text-[11px] text-slate-400">
                      {r.forSeconds ?? 0}s / {r.clearForSeconds ?? 0}s
                    </td>
                    <td className="max-w-xl px-3 py-2 font-mono text-[11px] text-slate-400">
                      {String(r.expression ?? "")}
                    </td>
                    <td className="px-3 py-2 text-right">
                      {canEdit && (
                        <div className="flex justify-end gap-2 text-xs">
                          <button
                            type="button"
                            className="text-sonar-300 hover:underline"
                            onClick={() => startEditRule(r)}
                          >
                            Edit
                          </button>
                          <button
                            type="button"
                            className="text-red-400 hover:underline"
                            onClick={() => {
                              if (confirm("Delete this alarm rule?")) delRule.mutate(String(r.id));
                            }}
                          >
                            Delete
                          </button>
                        </div>
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
                  <option value="slack">slack</option>
                  <option value="teams">teams</option>
                  <option value="email">email</option>
                </select>
                <input
                  placeholder="Label"
                  className="rounded-md border border-ink-700 bg-ink-950 px-3 py-2 text-sm"
                  value={chName}
                  onChange={(e) => setChName(e.target.value)}
                />
                <input
                  placeholder="Webhook URL (config.url)"
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
              <p className="text-[11px] text-slate-500">
                Slack and Teams channels post JSON to <code className="font-mono">config.url</code>{" "}
                (incoming webhook adapters).
              </p>
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

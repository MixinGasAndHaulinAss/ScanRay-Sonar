// DevicesGroups — manage device groups and membership.

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { api } from "../../api/client";
import type { Agent, Site, User } from "../../api/types";
import { EmptyHint, ErrorHint } from "./common";

type DeviceGroup = {
  id: string;
  siteId: string;
  name: string;
  description: string;
  memberCount: number;
};

export default function DevicesGroups() {
  const qc = useQueryClient();
  const me = useQuery({ queryKey: ["me"], queryFn: () => api.get<User>("/auth/me") });
  const canEdit = me.data?.role === "siteadmin" || me.data?.role === "superadmin";
  const sites = useQuery({ queryKey: ["sites"], queryFn: () => api.get<Site[]>("/sites") });
  const groups = useQuery({
    queryKey: ["device-groups"],
    queryFn: () => api.get<DeviceGroup[]>("/device-groups"),
  });
  const agents = useQuery({
    queryKey: ["agents"],
    queryFn: () => api.get<Agent[]>("/agents"),
  });

  const [siteId, setSiteId] = useState("");
  const [name, setName] = useState("");
  const [desc, setDesc] = useState("");
  const [selectedGroup, setSelectedGroup] = useState<string>("");
  const [pickedAgents, setPickedAgents] = useState<string[]>([]);

  const create = useMutation({
    mutationFn: () => api.post("/device-groups", { siteId, name, description: desc }),
    onSuccess: async () => {
      setName("");
      setDesc("");
      await qc.invalidateQueries({ queryKey: ["device-groups"] });
    },
  });

  const addMembers = useMutation({
    mutationFn: () =>
      api.post(`/device-groups/${selectedGroup}/members`, { agentIds: pickedAgents }),
    onSuccess: async () => {
      setPickedAgents([]);
      await qc.invalidateQueries({ queryKey: ["device-groups"] });
      await qc.invalidateQueries({ queryKey: ["agents"] });
    },
  });

  const removeMembers = useMutation({
    mutationFn: () =>
      api.del(`/device-groups/${selectedGroup}/members`, { agentIds: pickedAgents }),
    onSuccess: async () => {
      setPickedAgents([]);
      await qc.invalidateQueries({ queryKey: ["device-groups"] });
      await qc.invalidateQueries({ queryKey: ["agents"] });
    },
  });

  const delGroup = useMutation({
    mutationFn: (id: string) => api.del(`/device-groups/${id}`),
    onSuccess: async () => {
      if (selectedGroup) setSelectedGroup("");
      await qc.invalidateQueries({ queryKey: ["device-groups"] });
      await qc.invalidateQueries({ queryKey: ["agents"] });
    },
  });

  if (groups.isLoading) return <EmptyHint>Loading groups…</EmptyHint>;
  if (groups.isError) return <ErrorHint>Failed to load groups.</ErrorHint>;

  const siteAgents = (agents.data ?? []).filter((a) => !siteId || a.siteId === siteId);
  const activeGroup = (groups.data ?? []).find((g) => g.id === selectedGroup);

  return (
    <div className="grid gap-6 lg:grid-cols-2">
      <div className="space-y-3">
        <h3 className="text-sm font-semibold text-slate-200">Groups</h3>
        <ul className="space-y-1">
          {(groups.data ?? []).map((g) => (
            <li key={g.id}>
              <button
                type="button"
                onClick={() => {
                  setSelectedGroup(g.id);
                  setSiteId(g.siteId);
                }}
                className={
                  "flex w-full items-center justify-between rounded border px-3 py-2 text-left text-sm " +
                  (selectedGroup === g.id
                    ? "border-sonar-500/50 bg-ink-800"
                    : "border-ink-800 bg-ink-950/40 hover:border-ink-700")
                }
              >
                <span>
                  <span className="text-slate-100">{g.name}</span>
                  <span className="ml-2 text-xs text-slate-500">{g.memberCount} members</span>
                </span>
                {canEdit && (
                  <button
                    type="button"
                    className="text-xs text-rose-400 hover:underline"
                    onClick={(e) => {
                      e.stopPropagation();
                      delGroup.mutate(g.id);
                    }}
                  >
                    Delete
                  </button>
                )}
              </button>
            </li>
          ))}
        </ul>
        {canEdit && (
          <form
            className="space-y-2 rounded border border-ink-800 p-3"
            onSubmit={(e) => {
              e.preventDefault();
              if (siteId && name) create.mutate();
            }}
          >
            <h4 className="text-xs font-medium uppercase text-slate-400">New group</h4>
            <select
              required
              className="w-full rounded border border-ink-700 bg-ink-900 px-2 py-1.5 text-sm"
              value={siteId}
              onChange={(e) => setSiteId(e.target.value)}
            >
              <option value="">Site…</option>
              {(sites.data ?? []).map((s) => (
                <option key={s.id} value={s.id}>
                  {s.name}
                </option>
              ))}
            </select>
            <input
              required
              placeholder="Name"
              className="w-full rounded border border-ink-700 bg-ink-900 px-2 py-1.5 text-sm"
              value={name}
              onChange={(e) => setName(e.target.value)}
            />
            <input
              placeholder="Description"
              className="w-full rounded border border-ink-700 bg-ink-900 px-2 py-1.5 text-sm"
              value={desc}
              onChange={(e) => setDesc(e.target.value)}
            />
            <button
              type="submit"
              className="rounded bg-sonar-600 px-3 py-1.5 text-sm text-white hover:bg-sonar-500"
            >
              Create
            </button>
          </form>
        )}
      </div>

      <div className="space-y-3">
        <h3 className="text-sm font-semibold text-slate-200">
          Members{activeGroup ? ` — ${activeGroup.name}` : ""}
        </h3>
        {!selectedGroup ? (
          <EmptyHint>Select a group to assign agents (1:1 membership).</EmptyHint>
        ) : (
          <>
            <div className="max-h-80 overflow-auto rounded border border-ink-800">
              {(siteAgents ?? []).map((a) => {
                const checked = pickedAgents.includes(a.id);
                return (
                  <label
                    key={a.id}
                    className="flex cursor-pointer items-center gap-2 border-b border-ink-900 px-3 py-1.5 text-sm hover:bg-ink-900/50"
                  >
                    <input
                      type="checkbox"
                      checked={checked}
                      onChange={() =>
                        setPickedAgents((prev) =>
                          checked ? prev.filter((x) => x !== a.id) : [...prev, a.id],
                        )
                      }
                    />
                    <span className="text-slate-100">{a.hostname}</span>
                    {a.groupId === selectedGroup && (
                      <span className="text-[10px] uppercase text-emerald-400">in group</span>
                    )}
                    {a.groupId && a.groupId !== selectedGroup && (
                      <span className="text-[10px] text-slate-500">{a.groupName}</span>
                    )}
                  </label>
                );
              })}
            </div>
            {canEdit && (
              <div className="flex gap-2">
                <button
                  type="button"
                  disabled={!pickedAgents.length || addMembers.isPending}
                  onClick={() => addMembers.mutate()}
                  className="rounded bg-sonar-600 px-3 py-1.5 text-sm text-white disabled:opacity-40"
                >
                  Assign selected
                </button>
                <button
                  type="button"
                  disabled={!pickedAgents.length || removeMembers.isPending}
                  onClick={() => removeMembers.mutate()}
                  className="rounded border border-ink-700 px-3 py-1.5 text-sm text-slate-300"
                >
                  Remove selected
                </button>
              </div>
            )}
          </>
        )}
      </div>
    </div>
  );
}

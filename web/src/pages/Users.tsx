import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { ApiError, api } from "../api/client";
import type { User } from "../api/types";

const ROLES: User["role"][] = ["superadmin", "siteadmin", "tech", "readonly"];

const ROLE_DESCRIPTIONS: Record<User["role"], string> = {
  superadmin: "Full control. Manage users, sites, and the platform.",
  siteadmin: "Manage agents, appliances, and alerts within sites.",
  tech: "Read everything; ack alerts; run on-demand checks.",
  readonly: "Read-only dashboards.",
};

interface FormState {
  email: string;
  displayName: string;
  password: string;
  role: User["role"];
}

const EMPTY_FORM: FormState = {
  email: "",
  displayName: "",
  password: "",
  role: "tech",
};

function generatePassword(len = 20): string {
  const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghjkmnpqrstuvwxyz23456789!@#$%^&*";
  const buf = new Uint32Array(len);
  crypto.getRandomValues(buf);
  let out = "";
  for (let i = 0; i < len; i++) out += alphabet[buf[i] % alphabet.length];
  return out;
}

export default function Users() {
  const qc = useQueryClient();
  const users = useQuery({ queryKey: ["users"], queryFn: () => api.get<User[]>("/users") });
  const me = useQuery({ queryKey: ["me"], queryFn: () => api.get<User>("/auth/me") });

  const [open, setOpen] = useState(false);
  const [form, setForm] = useState<FormState>(EMPTY_FORM);
  const [err, setErr] = useState<string | null>(null);
  const [createdPassword, setCreatedPassword] = useState<{ email: string; password: string } | null>(null);

  const create = useMutation({
    mutationFn: (b: FormState) => api.post<User>("/users", b),
    onSuccess: (_u, vars) => {
      qc.invalidateQueries({ queryKey: ["users"] });
      setCreatedPassword({ email: vars.email, password: vars.password });
      setOpen(false);
      setForm(EMPTY_FORM);
      setErr(null);
    },
    onError: (e) => setErr(e instanceof ApiError ? e.message : "Failed to invite user"),
  });

  const setRole = useMutation({
    mutationFn: ({ id, role }: { id: string; role: User["role"] }) =>
      api.patch<User>(`/users/${id}`, { role }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["users"] }),
  });

  const deactivate = useMutation({
    mutationFn: ({ id, isActive }: { id: string; isActive: boolean }) =>
      api.patch<User>(`/users/${id}`, { isActive }),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["users"] }),
  });

  const [toDelete, setToDelete] = useState<User | null>(null);
  const [deleteConfirm, setDeleteConfirm] = useState("");
  const [deleteErr, setDeleteErr] = useState<string | null>(null);

  const del = useMutation({
    mutationFn: (id: string) => api.del<void>(`/users/${id}`),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["users"] });
      setToDelete(null);
      setDeleteConfirm("");
      setDeleteErr(null);
    },
    onError: (e) => setDeleteErr(e instanceof ApiError ? e.message : "Delete failed"),
  });

  return (
    <div className="space-y-4">
      <div className="flex items-center justify-between">
        <h2 className="text-2xl font-semibold tracking-tight">Users</h2>
        <button
          className="rounded-md bg-sonar-600 px-3 py-1.5 text-sm font-medium hover:bg-sonar-500"
          onClick={() => {
            setForm({ ...EMPTY_FORM, password: generatePassword() });
            setErr(null);
            setOpen(true);
          }}
        >
          Invite user
        </button>
      </div>

      <div className="overflow-hidden rounded-xl border border-ink-800 bg-ink-900">
        <table className="w-full text-left text-sm">
          <thead className="bg-ink-800/60 text-xs uppercase tracking-wide text-slate-400">
            <tr>
              <th className="px-4 py-2">Email</th>
              <th className="px-4 py-2">Name</th>
              <th className="px-4 py-2">Role</th>
              <th className="px-4 py-2">MFA</th>
              <th className="px-4 py-2">Status</th>
              <th className="px-4 py-2">Last login</th>
              <th className="px-4 py-2 text-right">Actions</th>
            </tr>
          </thead>
          <tbody>
            {users.data?.map((u) => {
              const isMe = me.data?.id === u.id;
              return (
                <tr key={u.id} className="border-t border-ink-800 hover:bg-ink-800/30">
                  <td className="px-4 py-2 font-mono text-xs">{u.email}</td>
                  <td className="px-4 py-2">{u.displayName}</td>
                  <td className="px-4 py-2">
                    <select
                      disabled={isMe}
                      value={u.role}
                      onChange={(e) =>
                        setRole.mutate({ id: u.id, role: e.target.value as User["role"] })
                      }
                      className="rounded-md border border-ink-700 bg-ink-950 px-2 py-1 text-xs disabled:opacity-50"
                    >
                      {ROLES.map((r) => (
                        <option key={r} value={r}>
                          {r}
                        </option>
                      ))}
                    </select>
                  </td>
                  <td className="px-4 py-2 text-slate-400">{u.totpEnrolled ? "TOTP" : "—"}</td>
                  <td className="px-4 py-2">
                    <span
                      className={
                        u.isActive
                          ? "rounded-full bg-emerald-900/40 px-2 py-0.5 text-xs text-emerald-300"
                          : "rounded-full bg-slate-700/40 px-2 py-0.5 text-xs text-slate-400"
                      }
                    >
                      {u.isActive ? "active" : "disabled"}
                    </span>
                  </td>
                  <td className="px-4 py-2 text-slate-500">
                    {u.lastLoginAt ? new Date(u.lastLoginAt).toLocaleString() : "—"}
                  </td>
                  <td className="px-4 py-2 text-right">
                    <div className="flex justify-end gap-1.5">
                      <button
                        disabled={isMe}
                        onClick={() => deactivate.mutate({ id: u.id, isActive: !u.isActive })}
                        className="rounded-md border border-ink-700 px-2 py-1 text-xs hover:bg-ink-800 disabled:opacity-30"
                      >
                        {u.isActive ? "Disable" : "Enable"}
                      </button>
                      <button
                        disabled={isMe}
                        onClick={() => {
                          setToDelete(u);
                          setDeleteConfirm("");
                          setDeleteErr(null);
                        }}
                        title={isMe ? "You cannot delete your own account" : "Permanently delete this user"}
                        className="rounded-md border border-red-900/60 bg-red-950/30 px-2 py-1 text-xs text-red-300 hover:bg-red-900/40 disabled:opacity-30"
                      >
                        Delete
                      </button>
                    </div>
                  </td>
                </tr>
              );
            })}
            {users.data?.length === 0 && (
              <tr>
                <td colSpan={7} className="px-4 py-6 text-center text-slate-500">
                  No users yet.
                </td>
              </tr>
            )}
          </tbody>
        </table>
      </div>

      {createdPassword && (
        <div className="rounded-xl border border-emerald-700 bg-emerald-900/20 p-4">
          <div className="text-sm font-medium text-emerald-200">User invited.</div>
          <div className="mt-1 text-xs text-emerald-300">
            Send {createdPassword.email} the password below — it will not be shown again.
          </div>
          <pre className="mt-2 overflow-x-auto rounded bg-ink-950 p-2 font-mono text-xs text-emerald-100">
            {createdPassword.password}
          </pre>
          <div className="mt-2 flex justify-end gap-2">
            <button
              onClick={() => navigator.clipboard.writeText(createdPassword.password)}
              className="rounded-md border border-emerald-700 px-2 py-1 text-xs text-emerald-200 hover:bg-emerald-900/40"
            >
              Copy
            </button>
            <button
              onClick={() => setCreatedPassword(null)}
              className="rounded-md border border-emerald-700 px-2 py-1 text-xs text-emerald-200 hover:bg-emerald-900/40"
            >
              Dismiss
            </button>
          </div>
        </div>
      )}

      {toDelete && (
        <div className="fixed inset-0 z-30 grid place-items-center bg-black/70 px-4">
          <form
            className="w-full max-w-md space-y-3 rounded-xl border border-red-900/60 bg-ink-900 p-5"
            onSubmit={(e) => {
              e.preventDefault();
              if (deleteConfirm === toDelete.email) del.mutate(toDelete.id);
            }}
          >
            <h3 className="text-lg font-semibold text-red-200">Delete user</h3>
            <p className="text-sm text-slate-300">
              This permanently removes{" "}
              <span className="font-mono text-slate-100">{toDelete.email}</span>{" "}
              and revokes all of their site memberships and API keys. Audit-log
              entries the user created are preserved.
            </p>
            <p className="text-xs text-slate-500">
              Disabling is reversible; deletion is not. Type the email to
              confirm.
            </p>
            <input
              autoFocus
              autoComplete="off"
              spellCheck={false}
              className="w-full rounded-md border border-ink-700 bg-ink-950 px-3 py-2 font-mono text-sm"
              value={deleteConfirm}
              onChange={(e) => setDeleteConfirm(e.target.value)}
              placeholder={toDelete.email}
            />
            {deleteErr && <div className="text-xs text-red-300">{deleteErr}</div>}
            <div className="flex justify-end gap-2 pt-2">
              <button
                type="button"
                className="rounded-md border border-ink-700 px-3 py-1.5 text-sm"
                onClick={() => {
                  setToDelete(null);
                  setDeleteConfirm("");
                  setDeleteErr(null);
                }}
              >
                Cancel
              </button>
              <button
                type="submit"
                disabled={deleteConfirm !== toDelete.email || del.isPending}
                className="rounded-md bg-red-600 px-3 py-1.5 text-sm font-medium hover:bg-red-500 disabled:opacity-40"
              >
                {del.isPending ? "Deleting…" : "Delete user"}
              </button>
            </div>
          </form>
        </div>
      )}

      {open && (
        <div className="fixed inset-0 z-20 grid place-items-center bg-black/60 px-4">
          <form
            className="w-full max-w-md space-y-3 rounded-xl border border-ink-800 bg-ink-900 p-5"
            onSubmit={(e) => {
              e.preventDefault();
              create.mutate(form);
            }}
          >
            <h3 className="text-lg font-semibold">Invite user</h3>
            <input
              required
              type="email"
              placeholder="email@example.com"
              autoComplete="off"
              className="w-full rounded-md border border-ink-700 bg-ink-950 px-3 py-2 text-sm"
              value={form.email}
              onChange={(e) => setForm({ ...form, email: e.target.value })}
            />
            <input
              required
              placeholder="Display name"
              className="w-full rounded-md border border-ink-700 bg-ink-950 px-3 py-2 text-sm"
              value={form.displayName}
              onChange={(e) => setForm({ ...form, displayName: e.target.value })}
            />
            <div>
              <label className="mb-1 block text-xs text-slate-400">Role</label>
              <select
                value={form.role}
                onChange={(e) => setForm({ ...form, role: e.target.value as User["role"] })}
                className="w-full rounded-md border border-ink-700 bg-ink-950 px-3 py-2 text-sm"
              >
                {ROLES.map((r) => (
                  <option key={r} value={r}>
                    {r}
                  </option>
                ))}
              </select>
              <p className="mt-1 text-xs text-slate-500">{ROLE_DESCRIPTIONS[form.role]}</p>
            </div>
            <div>
              <label className="mb-1 block text-xs text-slate-400">
                Initial password (≥12 chars, share securely)
              </label>
              <div className="flex gap-2">
                <input
                  required
                  minLength={12}
                  className="flex-1 rounded-md border border-ink-700 bg-ink-950 px-3 py-2 font-mono text-xs"
                  value={form.password}
                  onChange={(e) => setForm({ ...form, password: e.target.value })}
                />
                <button
                  type="button"
                  onClick={() => setForm({ ...form, password: generatePassword() })}
                  className="rounded-md border border-ink-700 px-2 py-1 text-xs hover:bg-ink-800"
                >
                  Regenerate
                </button>
              </div>
            </div>
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
                {create.isPending ? "Inviting…" : "Invite"}
              </button>
            </div>
          </form>
        </div>
      )}
    </div>
  );
}

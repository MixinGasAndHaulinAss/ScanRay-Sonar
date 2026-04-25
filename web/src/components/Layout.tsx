import { NavLink, useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api, tokens } from "../api/client";
import type { Site, User, VersionInfo } from "../api/types";
import clsx from "clsx";

const APP_VERSION = "2026.4.24.1";

const navItems = [
  { to: "/", label: "Dashboard" },
  { to: "/sites", label: "Sites" },
  { to: "/agents", label: "Agents" },
  { to: "/appliances", label: "Appliances" },
];

export default function Layout({ children }: { children: React.ReactNode }) {
  const navigate = useNavigate();
  const { data: me } = useQuery({ queryKey: ["me"], queryFn: () => api.get<User>("/auth/me") });
  const { data: sites } = useQuery({ queryKey: ["sites"], queryFn: () => api.get<Site[]>("/sites") });
  const { data: ver } = useQuery({ queryKey: ["version"], queryFn: () => api.get<VersionInfo>("/version") });

  function logout() {
    tokens.clear();
    navigate("/login", { replace: true });
  }

  return (
    <div className="flex min-h-full flex-col bg-ink-950 text-slate-100">
      <header className="sticky top-0 z-10 flex items-center gap-6 border-b border-ink-800 bg-ink-900/80 px-6 py-3 backdrop-blur">
        <div className="flex items-baseline gap-2">
          <span className="text-lg font-semibold tracking-tight text-sonar-400">ScanRay</span>
          <span className="text-lg font-semibold tracking-tight">Sonar</span>
          <span className="text-xs text-slate-500">v{ver?.version ?? APP_VERSION}</span>
        </div>
        <nav className="flex flex-1 items-center gap-1">
          {navItems.map((n) => (
            <NavLink
              key={n.to}
              to={n.to}
              end={n.to === "/"}
              className={({ isActive }) =>
                clsx(
                  "rounded-md px-3 py-1.5 text-sm transition-colors",
                  isActive
                    ? "bg-ink-800 text-white"
                    : "text-slate-400 hover:bg-ink-800/60 hover:text-slate-100",
                )
              }
            >
              {n.label}
            </NavLink>
          ))}
        </nav>
        <div className="flex items-center gap-3">
          <select
            className="rounded-md border border-ink-800 bg-ink-900 px-2 py-1 text-sm text-slate-300"
            defaultValue=""
            aria-label="Site selector"
          >
            <option value="">All sites</option>
            {sites?.map((s) => (
              <option key={s.id} value={s.id}>
                {s.name}
              </option>
            ))}
          </select>
          {me && <span className="text-sm text-slate-400">{me.email}</span>}
          <button
            onClick={logout}
            className="rounded-md border border-ink-700 bg-ink-800 px-3 py-1 text-sm text-slate-200 hover:bg-ink-700"
          >
            Sign out
          </button>
        </div>
      </header>
      <main className="flex-1 px-6 py-6">{children}</main>
    </div>
  );
}

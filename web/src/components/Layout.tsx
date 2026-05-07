import { NavLink, useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import { api, tokens } from "../api/client";
import type { Site, User, VersionInfo } from "../api/types";
import clsx from "clsx";
import ThemeToggle from "./ThemeToggle";

// APP_VERSION is just the static fallback shown if /version hasn't
// returned yet (or fails). The real source of truth is the Go binary's
// embedded VERSION, fetched below via useQuery.
const APP_VERSION = "2026.5.6.18";

const navItems: { to: string; label: string; roles?: User["role"][] }[] = [
  { to: "/", label: "Dashboard" },
  { to: "/sites", label: "Sites" },
  { to: "/agents", label: "Agents" },
  { to: "/appliances", label: "Appliances" },
  { to: "/collectors", label: "Collectors" },
  { to: "/topology", label: "Topology" },
  { to: "/world", label: "World" },
  { to: "/documents", label: "Documents" },
  { to: "/alarms", label: "Alarms" },
  { to: "/api-keys", label: "API keys" },
  { to: "/settings", label: "Settings", roles: ["siteadmin", "superadmin"] },
  { to: "/discovery", label: "Discovery", roles: ["superadmin"] },
  { to: "/audit-log", label: "Audit", roles: ["superadmin"] },
  { to: "/users", label: "Users", roles: ["superadmin"] },
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

  const initials = me?.displayName
    ? me.displayName
        .split(/\s+/)
        .filter(Boolean)
        .slice(0, 2)
        .map((s) => s[0]?.toUpperCase())
        .join("") || "?"
    : me?.email?.[0]?.toUpperCase() ?? "?";

  return (
    <div className="relative flex min-h-full flex-col bg-ink-950 text-slate-100">
      {/* Subtle radial gradient backdrop — sits behind everything and
          gives the dark UI a touch of depth without competing with
          chart colors. Pointer-events-none so it never intercepts
          clicks. */}
      <div
        aria-hidden
        className="pointer-events-none absolute inset-0 -z-10 opacity-60"
        style={{
          backgroundImage:
            "radial-gradient(1200px 600px at 10% -10%, rgba(59,130,246,0.08), transparent 60%), radial-gradient(900px 500px at 95% 0%, rgba(99,102,241,0.07), transparent 55%)",
        }}
      />

      <header className="sticky top-0 z-10 border-b border-ink-800 bg-ink-900/85 backdrop-blur-md">
        <div className="mx-auto flex max-w-screen-2xl items-center gap-6 px-6 py-3">
          <div className="flex items-center gap-2.5">
            <Logo />
            <div className="flex items-baseline gap-1.5">
              <span className="text-base font-semibold tracking-tight text-sonar-400">
                ScanRay
              </span>
              <span className="text-base font-semibold tracking-tight">Sonar</span>
              <span
                className="rounded-full border border-ink-700 px-1.5 py-px text-[10px] font-mono text-slate-500"
                title={ver?.commit ? `commit ${ver.commit.slice(0, 7)}` : undefined}
              >
                v{ver?.version ?? APP_VERSION}
              </span>
            </div>
          </div>

          <nav className="flex flex-1 items-center gap-1">
            {navItems
              .filter((n) => !n.roles || (me?.role != null && n.roles.includes(me.role)))
              .map((n) => (
                <NavLink
                  key={n.to}
                  to={n.to}
                  end={n.to === "/"}
                  className={({ isActive }) =>
                    clsx(
                      "rounded-full px-3 py-1.5 text-sm transition-colors",
                      isActive
                        ? "bg-sonar-500/15 text-sonar-200 shadow-sm"
                        : "text-slate-400 hover:bg-ink-800/60 hover:text-slate-100",
                    )
                  }
                >
                  {n.label}
                </NavLink>
              ))}
          </nav>

          <div className="flex items-center gap-3">
            <ThemeToggle />
            <select
              className="rounded-full border border-ink-800 bg-ink-900 px-3 py-1 text-sm text-slate-300 hover:border-ink-700 focus:border-sonar-500 focus:outline-none"
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
            {me && (
              <div
                className="flex items-center gap-2 rounded-full border border-ink-800 bg-ink-900 px-2 py-0.5 text-sm"
                title={me.email}
              >
                <span className="grid h-6 w-6 place-items-center rounded-full bg-sonar-500/20 text-[10px] font-semibold uppercase text-sonar-200">
                  {initials}
                </span>
                <span className="hidden truncate text-slate-300 md:inline-block">
                  {me.displayName || me.email}
                </span>
              </div>
            )}
            <button
              onClick={logout}
              className="rounded-full border border-ink-800 bg-ink-900 px-3 py-1 text-sm text-slate-300 hover:border-ink-700 hover:bg-ink-800 hover:text-slate-100"
            >
              Sign out
            </button>
          </div>
        </div>
      </header>

      <main className="mx-auto w-full max-w-screen-2xl flex-1 px-6 py-6">
        {children}
      </main>
    </div>
  );
}

// Logo — a small inline SVG that doesn't require an asset import. Three
// concentric arcs to suggest "sonar ping" + a center dot, in the same
// blue used elsewhere in the app.
function Logo() {
  return (
    <svg
      viewBox="0 0 32 32"
      width="22"
      height="22"
      aria-hidden
      className="text-sonar-400"
    >
      <circle cx="16" cy="16" r="2" fill="currentColor" />
      <path
        d="M9 16a7 7 0 0114 0"
        fill="none"
        stroke="currentColor"
        strokeWidth="2"
        strokeLinecap="round"
        opacity="0.85"
      />
      <path
        d="M5 16a11 11 0 0122 0"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.6"
        strokeLinecap="round"
        opacity="0.55"
      />
      <path
        d="M1 16a15 15 0 0130 0"
        fill="none"
        stroke="currentColor"
        strokeWidth="1.2"
        strokeLinecap="round"
        opacity="0.3"
      />
    </svg>
  );
}

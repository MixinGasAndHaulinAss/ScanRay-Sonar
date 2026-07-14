import { useState } from "react";
import { NavLink, useNavigate } from "react-router-dom";
import { useQuery } from "@tanstack/react-query";
import {
  HomeIcon,
  GlobeAltIcon,
  ComputerDesktopIcon,
  ServerStackIcon,
  SignalIcon,
  ShareIcon,
  MapIcon,
  DocumentTextIcon,
  BellAlertIcon,
  ChartBarIcon,
  ArrowsRightLeftIcon,
  MagnifyingGlassCircleIcon,
  KeyIcon,
  Cog6ToothIcon,
  FunnelIcon,
  ClipboardDocumentListIcon,
  UsersIcon,
  Bars3Icon,
  XMarkIcon,
  ArrowRightOnRectangleIcon,
} from "@heroicons/react/24/outline";
import type { ComponentType, SVGProps } from "react";
import { api, tokens } from "../api/client";
import type { Site, User, VersionInfo } from "../api/types";
import clsx from "clsx";
import ThemeToggle from "./ThemeToggle";

// APP_VERSION is just the static fallback shown if /version hasn't
// returned yet (or fails). The real source of truth is the Go binary's
// embedded VERSION, fetched below via useQuery.
const APP_VERSION = "2026.7.13.14";

type HeroIcon = ComponentType<SVGProps<SVGSVGElement>>;

type NavItem = {
  to: string;
  label: string;
  icon: HeroIcon;
  roles?: User["role"][];
};

const primaryNav: NavItem[] = [
  { to: "/", label: "Dashboard", icon: HomeIcon },
  { to: "/sites", label: "Sites", icon: GlobeAltIcon },
  { to: "/agents", label: "Devices", icon: ComputerDesktopIcon },
  { to: "/appliances", label: "Appliances", icon: ServerStackIcon },
  { to: "/collectors", label: "Collectors", icon: SignalIcon },
  { to: "/topology", label: "Topology", icon: ShareIcon },
  { to: "/traffic", label: "Traffic", icon: ArrowsRightLeftIcon },
  { to: "/world", label: "World", icon: MapIcon },
  { to: "/documents", label: "Documents", icon: DocumentTextIcon },
  { to: "/alarms", label: "Alarms", icon: BellAlertIcon },
  { to: "/reports", label: "Reports", icon: ChartBarIcon },
  { to: "/passive-snmp", label: "Discovered", icon: MagnifyingGlassCircleIcon },
  { to: "/api-keys", label: "API keys", icon: KeyIcon },
];

const adminNav: NavItem[] = [
  { to: "/settings", label: "Settings", icon: Cog6ToothIcon, roles: ["siteadmin", "superadmin"] },
  { to: "/discovery", label: "Discovery", icon: FunnelIcon, roles: ["superadmin"] },
  { to: "/audit-log", label: "Audit", icon: ClipboardDocumentListIcon, roles: ["superadmin"] },
  { to: "/users", label: "Users", icon: UsersIcon, roles: ["superadmin"] },
];

function visibleNav(items: NavItem[], role: User["role"] | undefined): NavItem[] {
  return items.filter((n) => !n.roles || (role != null && n.roles.includes(role)));
}

export default function Layout({ children }: { children: React.ReactNode }) {
  const navigate = useNavigate();
  const [sidebarOpen, setSidebarOpen] = useState(false);
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

  const primary = visibleNav(primaryNav, me?.role);
  const admin = visibleNav(adminNav, me?.role);

  function navClass({ isActive }: { isActive: boolean }) {
    return clsx(
      "flex items-center gap-3 rounded-lg px-3 py-2 text-sm transition-colors",
      isActive
        ? "bg-sonar-500/15 text-sonar-200"
        : "text-slate-400 hover:bg-ink-800/60 hover:text-slate-100",
    );
  }

  function renderNavItem(item: NavItem) {
    const Icon = item.icon;
    return (
      <NavLink
        key={item.to}
        to={item.to}
        end={item.to === "/"}
        onClick={() => setSidebarOpen(false)}
        className={navClass}
      >
        <Icon className="h-5 w-5 shrink-0" aria-hidden />
        {item.label}
      </NavLink>
    );
  }

  return (
    <div className="relative min-h-full bg-ink-950 text-slate-100">
      <div
        aria-hidden
        className="pointer-events-none absolute inset-0 -z-10 opacity-60"
        style={{
          backgroundImage:
            "radial-gradient(1200px 600px at 10% -10%, rgba(59,130,246,0.08), transparent 60%), radial-gradient(900px 500px at 95% 0%, rgba(99,102,241,0.07), transparent 55%)",
        }}
      />

      {sidebarOpen && (
        <button
          type="button"
          aria-label="Close navigation"
          className="fixed inset-0 z-40 bg-black/50 lg:hidden"
          onClick={() => setSidebarOpen(false)}
        />
      )}

      <aside
        className={clsx(
          "fixed top-0 left-0 z-50 flex h-full w-64 flex-col border-r border-ink-800 bg-ink-900 transition-transform duration-200",
          "lg:translate-x-0",
          sidebarOpen ? "translate-x-0" : "-translate-x-full",
        )}
      >
        <div className="flex h-16 items-center justify-between gap-2 border-b border-ink-800 px-4">
          <div className="flex min-w-0 items-center gap-2.5">
            <Logo />
            <div className="flex min-w-0 flex-col leading-tight">
              <div className="flex items-baseline gap-1.5 truncate">
                <span className="text-sm font-semibold tracking-tight text-sonar-400">ScanRay</span>
                <span className="text-sm font-semibold tracking-tight">Sonar</span>
              </div>
              <span
                className="font-mono text-[10px] text-slate-500"
                title={ver?.commit ? `commit ${ver.commit.slice(0, 7)}` : undefined}
              >
                v{ver?.version ?? APP_VERSION}
              </span>
            </div>
          </div>
          <button
            type="button"
            className="rounded-lg p-1 text-slate-400 hover:bg-ink-800 hover:text-slate-100 lg:hidden"
            onClick={() => setSidebarOpen(false)}
            aria-label="Close sidebar"
          >
            <XMarkIcon className="h-6 w-6" />
          </button>
        </div>

        <div className="border-b border-ink-800 p-4">
          <select
            className="w-full rounded-lg border border-ink-800 bg-ink-950 px-3 py-2 text-sm text-slate-300 hover:border-ink-700 focus:border-sonar-500 focus:outline-none"
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
        </div>

        <nav className="flex-1 space-y-1 overflow-y-auto px-3 py-4">
          {primary.map(renderNavItem)}
          {admin.length > 0 && (
            <>
              <div className="px-3 pb-2 pt-4">
                <span className="text-xs font-medium uppercase tracking-wider text-slate-500">Admin</span>
              </div>
              {admin.map(renderNavItem)}
            </>
          )}
        </nav>

        <div className="space-y-3 border-t border-ink-800 p-4">
          {me && (
            <div className="flex items-center gap-2" title={me.email}>
              <span className="grid h-8 w-8 shrink-0 place-items-center rounded-full bg-sonar-500/20 text-[11px] font-semibold uppercase text-sonar-200">
                {initials}
              </span>
              <div className="min-w-0">
                <div className="truncate text-sm text-slate-200">{me.displayName || me.email}</div>
                <div className="truncate text-xs capitalize text-slate-500">{me.role}</div>
              </div>
            </div>
          )}
          <div className="flex items-center justify-between gap-2">
            <ThemeToggle />
            <button
              type="button"
              onClick={logout}
              className="inline-flex items-center gap-1.5 rounded-lg border border-ink-800 bg-ink-950 px-3 py-1.5 text-sm text-slate-300 hover:border-ink-700 hover:bg-ink-800 hover:text-slate-100"
            >
              <ArrowRightOnRectangleIcon className="h-4 w-4" aria-hidden />
              Sign out
            </button>
          </div>
        </div>
      </aside>

      <div className="lg:pl-64">
        <header className="sticky top-0 z-30 flex h-16 items-center border-b border-ink-800 bg-ink-950/80 px-4 backdrop-blur-md lg:px-6">
          <button
            type="button"
            className="rounded-lg p-1.5 text-slate-400 hover:bg-ink-800 hover:text-slate-100 lg:hidden"
            onClick={() => setSidebarOpen(true)}
            aria-label="Open navigation"
          >
            <Bars3Icon className="h-6 w-6" />
          </button>
          <div className="flex-1" />
        </header>

        <main className="mx-auto w-full max-w-screen-2xl flex-1 px-4 py-6 lg:px-6">{children}</main>
      </div>
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
      className="shrink-0 text-sonar-400"
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

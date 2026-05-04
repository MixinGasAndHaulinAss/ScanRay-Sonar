// ThemeToggle — three-state segmented control: System / Light / Dark.
//
// The button reads the current preference from localStorage on mount
// and updates it via lib/theme.setTheme. We listen for the custom
// `sonar:theme` event so multiple toggles on the same page (or any
// programmatic theme change) keep their visual state in sync.

import { useEffect, useState } from "react";
import clsx from "clsx";
import { readThemePreference, setTheme, type ThemeMode } from "../lib/theme";

const OPTIONS: { mode: ThemeMode; label: string; icon: React.ReactNode }[] = [
  { mode: "system", label: "Match OS", icon: <SystemIcon /> },
  { mode: "light", label: "Light mode", icon: <SunIcon /> },
  { mode: "dark", label: "Dark mode", icon: <MoonIcon /> },
];

export default function ThemeToggle() {
  const [mode, setMode] = useState<ThemeMode>(() => readThemePreference());

  useEffect(() => {
    const onChange = (e: Event) => {
      const m = (e as CustomEvent<ThemeMode>).detail;
      if (m === "dark" || m === "light" || m === "system") setMode(m);
    };
    window.addEventListener("sonar:theme", onChange as EventListener);
    return () => window.removeEventListener("sonar:theme", onChange as EventListener);
  }, []);

  return (
    <div
      role="radiogroup"
      aria-label="Theme"
      className="inline-flex items-center rounded-full border border-ink-800 bg-ink-900 p-0.5"
    >
      {OPTIONS.map((o) => (
        <button
          key={o.mode}
          type="button"
          role="radio"
          aria-checked={mode === o.mode}
          title={o.label}
          onClick={() => {
            setMode(o.mode);
            setTheme(o.mode);
          }}
          className={clsx(
            "grid h-7 w-7 place-items-center rounded-full transition-colors",
            mode === o.mode
              ? "bg-sonar-500/15 text-sonar-300"
              : "text-slate-400 hover:bg-ink-800 hover:text-slate-200",
          )}
        >
          {o.icon}
          <span className="sr-only">{o.label}</span>
        </button>
      ))}
    </div>
  );
}

function SunIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
      <circle cx="12" cy="12" r="4" />
      <path d="M12 2v2" />
      <path d="M12 20v2" />
      <path d="m4.93 4.93 1.41 1.41" />
      <path d="m17.66 17.66 1.41 1.41" />
      <path d="M2 12h2" />
      <path d="M20 12h2" />
      <path d="m4.93 19.07 1.41-1.41" />
      <path d="m17.66 6.34 1.41-1.41" />
    </svg>
  );
}

function MoonIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
      <path d="M12 3a6 6 0 0 0 9 9 9 9 0 1 1-9-9Z" />
    </svg>
  );
}

function SystemIcon() {
  return (
    <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" strokeWidth="2" strokeLinecap="round" strokeLinejoin="round" aria-hidden>
      <rect x="3" y="4" width="18" height="12" rx="2" />
      <path d="M8 20h8" />
      <path d="M12 16v4" />
    </svg>
  );
}

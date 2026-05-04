// theme.ts — small theme controller. Two modes ("dark", "light");
// "system" tracks `prefers-color-scheme`. Persisted to localStorage
// so refreshing the tab or opening a new window keeps the choice.
//
// Apply synchronously from main.tsx before React renders to avoid a
// flash-of-unstyled-content on first paint.

export type ThemeMode = "dark" | "light" | "system";

const KEY = "sonar.theme";

export function readThemePreference(): ThemeMode {
  try {
    const v = localStorage.getItem(KEY);
    if (v === "dark" || v === "light" || v === "system") return v;
  } catch {
    // localStorage unavailable (private mode etc.) — fall through.
  }
  return "system";
}

function resolve(mode: ThemeMode): "dark" | "light" {
  if (mode === "system") {
    if (typeof window !== "undefined" && window.matchMedia) {
      return window.matchMedia("(prefers-color-scheme: light)").matches
        ? "light"
        : "dark";
    }
    return "dark";
  }
  return mode;
}

export function applyTheme(mode: ThemeMode): void {
  const resolved = resolve(mode);
  const cls = document.documentElement.classList;
  cls.toggle("light", resolved === "light");
  cls.toggle("dark", resolved === "dark");
}

export function setTheme(mode: ThemeMode): void {
  try {
    localStorage.setItem(KEY, mode);
  } catch {
    // ignore quota/availability errors
  }
  applyTheme(mode);
  // Notify in-page subscribers (the toggle button re-renders on change).
  window.dispatchEvent(new CustomEvent("sonar:theme", { detail: mode }));
}

// initTheme is called once from main.tsx before React mounts. It also
// subscribes to OS color-scheme changes so a user on "system" mode
// follows their OS toggle without a tab refresh.
export function initTheme(): void {
  const mode = readThemePreference();
  applyTheme(mode);
  if (typeof window !== "undefined" && window.matchMedia) {
    const mq = window.matchMedia("(prefers-color-scheme: light)");
    const onChange = () => {
      if (readThemePreference() === "system") applyTheme("system");
    };
    if (mq.addEventListener) {
      mq.addEventListener("change", onChange);
    } else if ((mq as MediaQueryList & { addListener?: (cb: () => void) => void }).addListener) {
      (mq as MediaQueryList & { addListener: (cb: () => void) => void }).addListener(onChange);
    }
  }
}

// TagFilter — searchable dropdown that lets an operator narrow a list
// down to a tag (or set of tags) without a row of pills eating the
// header. Replaces the old pill row used on Agents / World; the
// Topology page picks it up for free.
//
// Modes:
//   - mode="and" (default) — selected tags AND-match: a row must
//     have every selected tag. Matches the operator mental model of
//     "narrow my list to prod hosts that are also critical".
//   - mode="or" — any selected tag matches. Useful when an operator
//     wants to widen instead of narrow.
//   - singleSelect — radio-style, picking a tag replaces the
//     current selection. Used by World, where a single tag is the
//     historical UI.
//
// The component owns the popover open/close state and keyboard
// handling; the parent owns the selection (controlled component) so
// it can persist to localStorage and share state with other UI like
// per-row tag chips.

import { useEffect, useMemo, useRef, useState } from "react";

interface Props {
  availableTags: string[];
  selected: string[];
  onChange: (next: string[]) => void;
  mode?: "and" | "or";
  /** When true, picking a tag replaces the selection instead of toggling. */
  singleSelect?: boolean;
  /** Optional label override; defaults to "Tags". */
  label?: string;
  /** Width of the popover panel in px (default 256). */
  panelWidthPx?: number;
}

export default function TagFilter({
  availableTags,
  selected,
  onChange,
  mode = "and",
  singleSelect = false,
  label = "Tags",
  panelWidthPx = 256,
}: Props) {
  const [open, setOpen] = useState(false);
  const [query, setQuery] = useState("");
  const wrapRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);

  // Close on outside click or Escape. We listen on document so the
  // popover dismisses when the user clicks anywhere on the page —
  // matching the muscle memory of every other dropdown in the app.
  useEffect(() => {
    if (!open) return;
    const onDoc = (e: MouseEvent) => {
      if (!wrapRef.current) return;
      if (!wrapRef.current.contains(e.target as Node)) setOpen(false);
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key === "Escape") setOpen(false);
    };
    document.addEventListener("mousedown", onDoc);
    document.addEventListener("keydown", onKey);
    return () => {
      document.removeEventListener("mousedown", onDoc);
      document.removeEventListener("keydown", onKey);
    };
  }, [open]);

  // Auto-focus the search box on open so the operator can just type.
  useEffect(() => {
    if (open) {
      const t = setTimeout(() => inputRef.current?.focus(), 0);
      return () => clearTimeout(t);
    }
  }, [open]);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    if (!q) return availableTags;
    return availableTags.filter((t) => t.toLowerCase().includes(q));
  }, [availableTags, query]);

  const isSelected = (t: string) => selected.includes(t);
  const toggle = (t: string) => {
    if (singleSelect) {
      onChange(isSelected(t) ? [] : [t]);
      setOpen(false);
      return;
    }
    onChange(
      isSelected(t) ? selected.filter((x) => x !== t) : [...selected, t],
    );
  };
  const clear = () => onChange([]);

  const buttonLabel =
    selected.length === 0
      ? label
      : selected.length === 1
        ? `${label}: ${selected[0]}`
        : `${label}: ${selected.length} selected`;

  return (
    <div className="relative inline-block" ref={wrapRef}>
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        disabled={availableTags.length === 0}
        className={
          "inline-flex h-8 items-center gap-1.5 rounded-md border px-2.5 text-xs transition " +
          (selected.length > 0
            ? "border-sonar-500 bg-sonar-700/40 text-sonar-100 hover:bg-sonar-700/60"
            : "border-ink-700 bg-ink-950 text-slate-200 hover:border-ink-600 hover:bg-ink-900") +
          " disabled:cursor-not-allowed disabled:opacity-50"
        }
        title={
          availableTags.length === 0
            ? "No tags defined"
            : selected.length > 0
              ? `Filtering by ${selected.join(", ")}`
              : "Filter by tag"
        }
      >
        <svg
          width="12"
          height="12"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2"
          strokeLinecap="round"
          strokeLinejoin="round"
        >
          <path d="M20.59 13.41 13.42 20.58a2 2 0 0 1-2.83 0L2 12V2h10l8.59 8.59a2 2 0 0 1 0 2.82z" />
          <line x1="7" y1="7" x2="7.01" y2="7" />
        </svg>
        <span className="truncate">{buttonLabel}</span>
        <svg
          width="10"
          height="10"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          strokeWidth="2.5"
          strokeLinecap="round"
          strokeLinejoin="round"
          className="opacity-70"
        >
          <polyline points="6 9 12 15 18 9" />
        </svg>
      </button>

      {open && (
        <div
          className="absolute right-0 z-30 mt-1 overflow-hidden rounded-lg border border-ink-700 bg-ink-900 shadow-2xl"
          style={{ width: panelWidthPx }}
          role="dialog"
        >
          <div className="border-b border-ink-800 p-2">
            <input
              ref={inputRef}
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Search tags…"
              className="h-7 w-full rounded-md border border-ink-700 bg-ink-950 px-2 text-xs text-slate-100 placeholder:text-slate-600 focus:border-sonar-500 focus:outline-none"
            />
          </div>
          <ul className="max-h-64 overflow-auto py-1">
            {filtered.length === 0 && (
              <li className="px-3 py-2 text-[11px] text-slate-500">
                {query ? "No matches" : "No tags"}
              </li>
            )}
            {filtered.map((t) => {
              const on = isSelected(t);
              return (
                <li key={t}>
                  <button
                    type="button"
                    onClick={() => toggle(t)}
                    className={
                      "flex w-full items-center gap-2 px-3 py-1.5 text-left text-xs hover:bg-ink-800 " +
                      (on ? "text-sonar-200" : "text-slate-200")
                    }
                  >
                    {singleSelect ? (
                      <span
                        className={
                          "inline-block h-3 w-3 shrink-0 rounded-full border " +
                          (on
                            ? "border-sonar-500 bg-sonar-500"
                            : "border-ink-600 bg-transparent")
                        }
                      />
                    ) : (
                      <span
                        className={
                          "inline-flex h-3.5 w-3.5 shrink-0 items-center justify-center rounded-sm border " +
                          (on
                            ? "border-sonar-500 bg-sonar-500"
                            : "border-ink-600 bg-transparent")
                        }
                      >
                        {on && (
                          <svg
                            width="10"
                            height="10"
                            viewBox="0 0 24 24"
                            fill="none"
                            stroke="white"
                            strokeWidth="3"
                            strokeLinecap="round"
                            strokeLinejoin="round"
                          >
                            <polyline points="20 6 9 17 4 12" />
                          </svg>
                        )}
                      </span>
                    )}
                    <span className="truncate">{t}</span>
                  </button>
                </li>
              );
            })}
          </ul>
          {selected.length > 0 && (
            <div className="flex items-center justify-between gap-2 border-t border-ink-800 px-2 py-1.5 text-[10px]">
              <span className="text-slate-500">
                {!singleSelect && selected.length > 1 && (
                  <>match <span className="uppercase tracking-wider">{mode}</span> · </>
                )}
                {selected.length} selected
              </span>
              <button
                onClick={clear}
                className="rounded border border-ink-700 px-2 py-0.5 text-slate-300 hover:border-ink-600 hover:bg-ink-800"
              >
                Clear
              </button>
            </div>
          )}
        </div>
      )}
    </div>
  );
}

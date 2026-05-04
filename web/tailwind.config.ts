import type { Config } from "tailwindcss";

// Sonar palette is themed via CSS custom properties (see index.css).
// Each color is declared with the `rgb(var(--name) / <alpha-value>)`
// pattern so Tailwind's opacity modifiers (e.g. `bg-ink-950/40`) keep
// working after the theme swap. Overriding specific slate shades here
// means the existing class names across the codebase (text-slate-100,
// text-slate-400, etc.) automatically flip palette in light mode —
// no per-component `dark:` prefixing required.
export default {
  content: ["./index.html", "./src/**/*.{ts,tsx}"],
  darkMode: "class",
  theme: {
    extend: {
      colors: {
        ink: {
          950: "rgb(var(--ink-950) / <alpha-value>)",
          900: "rgb(var(--ink-900) / <alpha-value>)",
          800: "rgb(var(--ink-800) / <alpha-value>)",
          700: "rgb(var(--ink-700) / <alpha-value>)",
        },
        // Slate is fully themed. The 100-600 weights are used as TEXT
        // and flip dark-on-dark / dark-on-light correctly. The 700-950
        // weights are used as backgrounds for status pills and panels;
        // they invert in light mode so a `bg-slate-800` pill becomes
        // light-grey instead of staying near-black.
        slate: {
          100: "rgb(var(--slate-100) / <alpha-value>)",
          200: "rgb(var(--slate-200) / <alpha-value>)",
          300: "rgb(var(--slate-300) / <alpha-value>)",
          400: "rgb(var(--slate-400) / <alpha-value>)",
          500: "rgb(var(--slate-500) / <alpha-value>)",
          600: "rgb(var(--slate-600) / <alpha-value>)",
          700: "rgb(var(--slate-700) / <alpha-value>)",
          800: "rgb(var(--slate-800) / <alpha-value>)",
          900: "rgb(var(--slate-900) / <alpha-value>)",
          950: "rgb(var(--slate-950) / <alpha-value>)",
        },
        sonar: {
          200: "rgb(var(--sonar-200) / <alpha-value>)",
          300: "rgb(var(--sonar-300) / <alpha-value>)",
          400: "rgb(var(--sonar-400) / <alpha-value>)",
          500: "rgb(var(--sonar-500) / <alpha-value>)",
          600: "rgb(var(--sonar-600) / <alpha-value>)",
        },
        // Status / tone text shades. The 200 & 300 weights are used as
        // *text* (e.g. text-emerald-300 for "good" KPI values). On the
        // dark surface those pastel weights have enough contrast; on a
        // light surface they wash out. We CSS-var-back them so light
        // mode swaps in the 700/800 weights of the same hue.
        emerald: {
          100: "rgb(var(--emerald-100) / <alpha-value>)",
          200: "rgb(var(--emerald-200) / <alpha-value>)",
          300: "rgb(var(--emerald-300) / <alpha-value>)",
        },
        amber: {
          100: "rgb(var(--amber-100) / <alpha-value>)",
          200: "rgb(var(--amber-200) / <alpha-value>)",
          300: "rgb(var(--amber-300) / <alpha-value>)",
        },
        red: {
          100: "rgb(var(--red-100) / <alpha-value>)",
          200: "rgb(var(--red-200) / <alpha-value>)",
          300: "rgb(var(--red-300) / <alpha-value>)",
        },
        sky: {
          100: "rgb(var(--sky-100) / <alpha-value>)",
          200: "rgb(var(--sky-200) / <alpha-value>)",
          300: "rgb(var(--sky-300) / <alpha-value>)",
        },
        indigo: {
          200: "rgb(var(--indigo-200) / <alpha-value>)",
          300: "rgb(var(--indigo-300) / <alpha-value>)",
        },
      },
    },
  },
  plugins: [],
} satisfies Config;

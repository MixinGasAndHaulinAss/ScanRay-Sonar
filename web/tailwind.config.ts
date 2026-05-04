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
        // Override the slate shades the app uses for text. Other slate
        // shades (50, 700-950) fall through to Tailwind defaults via
        // the deep merge that `extend.colors` performs.
        slate: {
          100: "rgb(var(--slate-100) / <alpha-value>)",
          200: "rgb(var(--slate-200) / <alpha-value>)",
          300: "rgb(var(--slate-300) / <alpha-value>)",
          400: "rgb(var(--slate-400) / <alpha-value>)",
          500: "rgb(var(--slate-500) / <alpha-value>)",
          600: "rgb(var(--slate-600) / <alpha-value>)",
        },
        sonar: {
          200: "rgb(var(--sonar-200) / <alpha-value>)",
          300: "rgb(var(--sonar-300) / <alpha-value>)",
          400: "rgb(var(--sonar-400) / <alpha-value>)",
          500: "rgb(var(--sonar-500) / <alpha-value>)",
          600: "rgb(var(--sonar-600) / <alpha-value>)",
        },
      },
    },
  },
  plugins: [],
} satisfies Config;

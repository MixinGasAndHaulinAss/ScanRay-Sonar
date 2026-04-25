import { defineConfig, loadEnv } from "vite";
import react from "@vitejs/plugin-react";

// Vite proxies API + websocket to the Go server during development so the
// browser can speak to the same origin as production. In production the Go
// binary serves dist/ via go:embed, so no proxy is needed.
//
// SONAR_DEV_API_URL overrides the upstream when running the API on a
// different host/port (e.g. inside docker on the dev VM). Default
// matches the docker-compose published port (6969).
export default defineConfig(({ mode }) => {
  const env = loadEnv(mode, process.cwd(), "");
  const apiURL = env.SONAR_DEV_API_URL || "http://127.0.0.1:6969";
  const wsURL = apiURL.replace(/^http/, "ws");
  return {
    plugins: [react()],
    server: {
      port: 5173,
      proxy: {
        "/api": {
          target: apiURL,
          changeOrigin: false,
        },
        "/ws": {
          target: wsURL,
          ws: true,
        },
      },
    },
    build: {
      outDir: "dist",
      sourcemap: true,
    },
  };
});

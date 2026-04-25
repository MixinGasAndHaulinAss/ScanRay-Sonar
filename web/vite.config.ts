import { defineConfig } from "vite";
import react from "@vitejs/plugin-react";

// Vite proxies API + websocket to the Go server during development so the
// browser can speak to the same origin as production. In production the Go
// binary serves dist/ via go:embed, so no proxy is needed.
export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    proxy: {
      "/api": {
        target: "http://127.0.0.1:8080",
        changeOrigin: false,
      },
      "/ws": {
        target: "ws://127.0.0.1:8080",
        ws: true,
      },
    },
  },
  build: {
    outDir: "dist",
    sourcemap: true,
  },
});

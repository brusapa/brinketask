import react from "@vitejs/plugin-react";
import { defineConfig } from "vitest/config";

// The Go server listens on 8081 in development (CLAUDE.md). The dev server
// forwards the API and the login routes to it, so the browser sees a single
// origin, http://localhost:5173, as in production (SPEC section 2).
const backend = "http://localhost:8081";

export default defineConfig({
  plugins: [react()],
  server: {
    port: 5173,
    strictPort: true,
    proxy: {
      "/api": backend,
      "/auth": backend,
    },
  },
  build: {
    outDir: "dist",
    // No source maps in the embedded build: they would double its size.
    sourcemap: false,
    // The main chunk is about 850 kB (250 kB compressed), mostly React,
    // React Aria and its messages for every language it supports. Assets
    // are cached for a year, so this is paid once per release; Vite's
    // default warning (500 kB) is raised to that size on purpose.
    chunkSizeWarningLimit: 900,
  },
  test: {
    environment: "jsdom",
    setupFiles: ["src/test/setup.ts"],
    restoreMocks: true,
  },
});

// Builds the service worker on its own: one classic script, dist/sw.js,
// with everything it imports inlined. A module service worker would need
// browser support that is not universal yet, and a hashed name would
// change its URL, which identifies the worker.
import { defineConfig } from "vite";

export default defineConfig({
  build: {
    outDir: "dist",
    // The main build ran first; keep its files.
    emptyOutDir: false,
    sourcemap: false,
    lib: {
      entry: "src/sw/sw.ts",
      formats: ["iife"],
      name: "brinketaskServiceWorker",
      fileName: () => "sw.js",
    },
  },
});

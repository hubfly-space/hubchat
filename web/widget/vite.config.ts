import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { fileURLToPath, URL } from "node:url";
import { defineConfig } from "vite";

/**
 * Widget build (§17 performance).
 *
 * Two artifacts, and the split is the entire performance strategy:
 *
 *   public/v1.js  — the loader. Hand-written, no framework, a couple of KB.
 *                   Copied verbatim; this is what a customer's <script> tag
 *                   points at, and it is the only thing that touches their
 *                   page before someone clicks the launcher.
 *
 *   app.js        — the interface. Fetched on first open, never on page load.
 *                   Mounted into a shadow root so no style crosses either way.
 *
 * CSS is inlined into the JS bundle for the normal path and emitted as
 * app.css for browsers where the host blocks inline Shadow DOM styles.
 */
export default defineConfig({
  base: "/widget/",
  plugins: [react(), tailwindcss()],
  // App builds get this for free; a library build does not, because a
  // published library expects its own consumer's bundler to define it. This
  // is not a published library — it is the final artifact a browser runs
  // directly — so without it, React's own dev-mode checks throw on the very
  // first render: "process is not defined".
  define: {
    "process.env.NODE_ENV": JSON.stringify(process.env.NODE_ENV ?? "production"),
  },
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
      "@hubchat/shared": fileURLToPath(new URL("../shared/src", import.meta.url)),
    },
  },
  build: {
    outDir: "../../embedded/assets/widget",
    emptyOutDir: true,
    sourcemap: true,
    cssCodeSplit: false,
    // A hard ceiling, not a suggestion. If the widget crosses this, something
    // was imported that belongs in the dashboard.
    chunkSizeWarningLimit: 220,
    // Library mode, not an application build: v1.js loads this file with a
    // dynamic `import()` and reads `module.mount` off the result (see
    // main.tsx's exported `mount`). An application build does not preserve
    // an entry's exports in its output — Rollup only keeps them for a
    // declared library entry — so without this, the import resolves to an
    // empty namespace object and `module.mount is not a function` at the one
    // moment a visitor actually opens the widget.
    lib: {
      entry: fileURLToPath(new URL("./src/main.tsx", import.meta.url)),
      formats: ["es"],
      fileName: () => "app.js",
      cssFileName: "app",
    },
    rollupOptions: {
      output: {
        chunkFileNames: "[name]-[hash].js",
        assetFileNames: "[name][extname]",
      },
    },
  },
  server: {
    port: 5175,
    proxy: {
      "/api": { target: "http://localhost:8080", changeOrigin: true },
      "/ws": { target: "ws://localhost:8080", ws: true },
    },
  },
});

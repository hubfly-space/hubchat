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
      "react/jsx-runtime": fileURLToPath(import.meta.resolve("preact/jsx-runtime")),
      "react/jsx-dev-runtime": fileURLToPath(import.meta.resolve("preact/jsx-dev-runtime")),
      "react-dom/client": fileURLToPath(import.meta.resolve("preact/compat/client")),
      "react-dom": fileURLToPath(import.meta.resolve("preact/compat")),
      "react": fileURLToPath(import.meta.resolve("preact/compat")),
      "@": fileURLToPath(new URL("./src", import.meta.url)),
      "@hubchat/shared": fileURLToPath(new URL("../shared/src", import.meta.url)),
    },
  },
  build: {
    outDir: "../../embedded/assets/widget",
    emptyOutDir: true,
    sourcemap: true,
    cssCodeSplit: false,
    // Developer warning only; check-bundle-budgets.mjs enforces gzip sizes.
    chunkSizeWarningLimit: 220,
    // A browser module with a stable mount export. Application minification
    // plus Preact's React compatibility layer keeps the interface below its
    // compressed budget; the dashboard and portal use their own build configs.
    rollupOptions: {
      input: fileURLToPath(new URL("./src/main.tsx", import.meta.url)),
      preserveEntrySignatures: "strict",
      output: {
        entryFileNames: "app.js",
        chunkFileNames: "[name]-[hash].js",
        assetFileNames: (asset) => asset.names?.some((name) => name.endsWith(".css")) ? "app.css" : "[name][extname]",
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

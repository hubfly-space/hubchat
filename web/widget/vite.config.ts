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
 * CSS is inlined into the JS bundle rather than emitted as a separate file:
 * the shadow root needs the stylesheet as a string, and a second network
 * request for it would leave the panel unstyled for a frame.
 */
export default defineConfig({
  base: "/widget/",
  plugins: [react(), tailwindcss()],
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
    rollupOptions: {
      input: fileURLToPath(new URL("./src/main.tsx", import.meta.url)),
      output: {
        entryFileNames: "app.js",
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

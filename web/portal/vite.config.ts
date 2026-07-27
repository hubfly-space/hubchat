import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { fileURLToPath, URL } from "node:url";
import { defineConfig } from "vite";

/**
 * The portal is public and indexed, so its budget is tighter than the
 * dashboard's — a customer arriving from a search result should not download
 * an agent console to read one article.
 */
export default defineConfig({
  base: "/portal/",
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
      "@hubchat/shared": fileURLToPath(new URL("../shared/src", import.meta.url)),
    },
  },
  build: {
    outDir: "../../embedded/assets/portal",
    emptyOutDir: true,
    sourcemap: true,
    // The initial chunk is what a first-time visitor pays for. Routes beyond
    // the landing page are split, so this ceiling applies to the shell plus
    // home only — if it trips, something was imported that does not belong on
    // the critical path.
    // Set just above the React vendor chunk, whose size is React's and not
    // ours. The portal's own entry sits around 80 KB, so anything that trips
    // this is a genuine regression rather than background noise.
    chunkSizeWarningLimit: 300,
    rollupOptions: {
      output: {
        // Matched by resolved path rather than by bare specifier: listing
        // "react-dom" by name misses `react-dom/client`, which is the entry the
        // app actually imports, and leaves the largest dependency in the app
        // chunk where it invalidates on every content change.
        manualChunks(id) {
          if (!id.includes("node_modules")) return undefined;
          if (/[\\/]node_modules[\\/](\.pnpm[\\/])?(react|react-dom|react-router|scheduler)[@\\/]/.test(id)) {
            return "vendor-react";
          }
          if (id.includes("@radix-ui") || id.includes("@floating-ui")) return "vendor-ui";
          return undefined;
        },
      },
    },
  },
  server: {
    port: 5174,
    proxy: {
      "/api": { target: "http://localhost:8080", changeOrigin: true },
      "/ws": { target: "ws://localhost:8080", ws: true },
    },
  },
});

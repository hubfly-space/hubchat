import tailwindcss from "@tailwindcss/vite";
import react from "@vitejs/plugin-react";
import { fileURLToPath, URL } from "node:url";
import { defineConfig } from "vite";

/**
 * Output lands directly in embedded/assets so `go:embed` picks it up without a
 * copy step (§8.3). Asset filenames are content-hashed; the Go asset handler
 * serves them immutable and the HTML entry point with a short max-age.
 */
export default defineConfig({
  base: "/app/",
  plugins: [react(), tailwindcss()],
  resolve: {
    alias: {
      "@": fileURLToPath(new URL("./src", import.meta.url)),
      "@hubchat/shared": fileURLToPath(new URL("../shared/src", import.meta.url)),
    },
  },
  build: {
    outDir: "../../embedded/assets/dashboard",
    emptyOutDir: true,
    sourcemap: true,
    // The dashboard is behind auth and loaded once per session, so a larger
    // initial payload is a fair trade for fewer round trips. The widget budget
    // (§17) is the one that is actually tight.
    chunkSizeWarningLimit: 320,
    rollupOptions: {
      output: {
        // Matched by resolved path, not bare specifier: listing "react-dom"
        // by name misses `react-dom/client`, which is what the app imports,
        // and silently leaves the largest dependency in the app chunk.
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
    port: 5173,
    proxy: {
      // Overridable so a Go server on a non-default port (a second instance,
      // a busy 8080 on a shared dev box) doesn't require editing this file —
      // `HUBCHAT_DEV_API_PORT=8091 pnpm dev`.
      "/api": { target: `http://localhost:${process.env.HUBCHAT_DEV_API_PORT ?? 8080}`, changeOrigin: true },
      "/ws": { target: `ws://localhost:${process.env.HUBCHAT_DEV_API_PORT ?? 8080}`, ws: true },
    },
  },
});

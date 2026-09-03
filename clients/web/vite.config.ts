import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";
import { VitePWA } from "vite-plugin-pwa";

// React + PWA. VitePWA (Workbox) emits the service worker: precache the app
// shell for offline, navigate-fallback to index.html for the SPA. The DB/crypto
// dedicated worker is bundled as ESM (`worker.format`).
export default defineConfig({
  plugins: [
    react(),
    VitePWA({
      registerType: "prompt",
      manifest: {
        name: "WhatsApp V2",
        short_name: "WA V2",
        description: "Self-hostable, end-to-end encrypted messaging.",
        theme_color: "#128C7E",
        background_color: "#ffffff",
        display: "standalone",
        start_url: "/",
        icons: [
          { src: "pwa-192x192.png", sizes: "192x192", type: "image/png" },
          { src: "pwa-512x512.png", sizes: "512x512", type: "image/png" },
          { src: "pwa-512x512.png", sizes: "512x512", type: "image/png", purpose: "maskable" },
        ],
      },
      workbox: {
        globPatterns: ["**/*.{js,css,html,ico,png,svg,woff2}"],
        navigateFallback: "index.html",
      },
    }),
  ],
  worker: { format: "es" },
  // sqlite-wasm resolves its .wasm relative to its own module URL and must not
  // be pre-bundled, or the dev server rewrites that URL and the fetch 404s.
  optimizeDeps: { exclude: ["@sqlite.org/sqlite-wasm"] },
});

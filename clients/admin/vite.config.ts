import react from "@vitejs/plugin-react";
import { defineConfig } from "vite";

// The admin console is a plain SPA (no PWA/offline — it's an internal tool that
// must always talk to a live backend). It runs on its own port so it can be
// served from a separate, IP-allowlisted hostname in production (HLD §15.6).
export default defineConfig({
  plugins: [react()],
  server: { port: 5174, strictPort: true, host: "0.0.0.0" },
});

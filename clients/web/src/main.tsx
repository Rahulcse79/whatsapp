import { StrictMode } from "react";
import { createRoot } from "react-dom/client";
import { registerSW } from "virtual:pwa-register";
import { App } from "./App";
import "./styles.css";
import { applyTheme } from "./theme";

// Apply the persisted light/dark theme before first paint (T5.15).
applyTheme();

// Register the Workbox-generated service worker (offline shell + push).
registerSW({ immediate: true });

const root = document.getElementById("root");
if (root) {
  createRoot(root).render(
    <StrictMode>
      <App />
    </StrictMode>,
  );
}

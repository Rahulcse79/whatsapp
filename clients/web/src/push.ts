import { config } from "./config";

// WebPush registration (web-app-architecture.md §2). Guarded and best-effort:
// only runs where the browser supports it AND a VAPID key is configured. Actual
// push delivery is server-side (notification-svc, T0.16) with local
// decrypt-and-render; the subscription would be POSTed to the device-token
// endpoint in the full build.
export async function registerWebPush(): Promise<PushSubscription | null> {
  if (!("serviceWorker" in navigator) || !("PushManager" in globalThis)) return null;
  if (!config.vapidPublicKey) return null;
  if ((await Notification.requestPermission()) !== "granted") return null;

  const registration = await navigator.serviceWorker.ready;
  const existing = await registration.pushManager.getSubscription();
  if (existing) return existing;

  return registration.pushManager.subscribe({
    userVisibleOnly: true,
    applicationServerKey: urlBase64ToUint8Array(config.vapidPublicKey),
  });
}

function urlBase64ToUint8Array(base64: string): Uint8Array {
  const padding = "=".repeat((4 - (base64.length % 4)) % 4);
  const normalized = (base64 + padding).replace(/-/g, "+").replace(/_/g, "/");
  const raw = atob(normalized);
  const out = new Uint8Array(raw.length);
  for (let i = 0; i < raw.length; i++) out[i] = raw.charCodeAt(i);
  return out;
}

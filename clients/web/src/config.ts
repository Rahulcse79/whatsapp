// Runtime config. Defaults target a locally self-hosted stack (the project runs
// fully offline — HLD §17.5); override at build time with EXPO-style
// VITE_-prefixed env (inlined by Vite into import.meta.env).
function envStr(key: string, fallback: string): string {
  const v = import.meta.env[key];
  return typeof v === "string" && v.length > 0 ? v : fallback;
}

// mediaBaseUrl derives from VITE_MEDIA_URL, else the API host on the media-svc
// port (:8082) — media-svc is a separate deployable from core-api.
function defaultMediaUrl(api: string): string {
  try {
    const u = new URL(api);
    u.port = "8082";
    return u.origin;
  } catch {
    return "http://localhost:8082";
  }
}

const apiBaseUrl = envStr("VITE_API_URL", "http://localhost:8080");

export const config = {
  apiBaseUrl,
  mediaBaseUrl: envStr("VITE_MEDIA_URL", defaultMediaUrl(apiBaseUrl)),
  wsUrl: envStr("VITE_WS_URL", "ws://localhost:8080/v1/ws"),
  // VAPID public key for WebPush; the self-hosted notification-svc provides one.
  vapidPublicKey: envStr("VITE_VAPID_PUBLIC_KEY", ""),
};

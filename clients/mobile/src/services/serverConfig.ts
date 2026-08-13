import { secureStore } from "../platform/secureStore";
import type { AppConfig } from "./appServices";

// The server the app talks to, entered by the user and persisted on-device (so
// there's no need to rebuild the APK when your machine's IP changes). Stored in
// the platform secure store (Keychain/Keystore) under this key.
const KEY = "wa.serverUrl";

export async function loadServerUrl(): Promise<string | null> {
  try {
    return await secureStore.get(KEY);
  } catch {
    return null;
  }
}

export async function saveServerUrl(raw: string): Promise<void> {
  await secureStore.set(KEY, raw.trim());
}

export async function clearServerUrl(): Promise<void> {
  try {
    await secureStore.delete(KEY);
  } catch {
    /* nothing persisted — fine */
  }
}

// deriveConfig turns what the user typed into API + WebSocket endpoints. It
// accepts a bare host/IP ("192.168.1.5"), a host:port, or a full URL. The dev
// backend serves the REST API on :8080 and the WS gateway on :8081 (see
// start.sh), so when only a host is given those ports are assumed. For an https
// URL (a single reverse-proxied origin) the WS rides the same host/port as wss.
export function deriveConfig(raw: string): AppConfig {
  const s = raw.trim().replace(/\s+/g, "").replace(/\/+$/, "");

  const withScheme = s.match(/^(https?):\/\/([^/:]+)(?::(\d+))?/i);
  if (withScheme) {
    const scheme = withScheme[1].toLowerCase();
    const host = withScheme[2];
    const secure = scheme === "https";
    const apiPort = withScheme[3] ?? (secure ? "443" : "8080");
    const wsPort = secure ? apiPort : "8081";
    return {
      apiBaseUrl: `${scheme}://${host}:${apiPort}`,
      wsUrl: `${secure ? "wss" : "ws"}://${host}:${wsPort}/v1/ws`,
    };
  }

  // Bare host or host:port → http dev ports.
  const hostPort = s.match(/^([^/:]+)(?::(\d+))?$/);
  const host = hostPort ? hostPort[1] : s;
  const apiPort = hostPort && hostPort[2] ? hostPort[2] : "8080";
  return {
    apiBaseUrl: `http://${host}:${apiPort}`,
    wsUrl: `ws://${host}:8081/v1/ws`,
  };
}

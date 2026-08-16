// Device-auth client helpers (T10.02): base64url codec for the WebAuthn wire and
// the app-lock idle policy. Pure + framework-free (no DOM types) so both web and
// React Native share it; the actual navigator.credentials / native-biometric
// ceremonies live in the platform layer.

/** bytesToB64url encodes bytes as raw-URL base64 (no padding) — the form the
 *  WebAuthn REST surface uses for challenges, keys, and signatures. */
export function bytesToB64url(bytes: Uint8Array): string {
  let bin = "";
  for (let i = 0; i < bytes.length; i++) bin += String.fromCharCode(bytes[i]!);
  return btoa(bin).replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

/** b64urlToBytes decodes raw-URL base64 back to bytes. */
export function b64urlToBytes(s: string): Uint8Array {
  const b64 = s.replace(/-/g, "+").replace(/_/g, "/") + "===".slice((s.length + 3) % 4);
  const bin = atob(b64);
  const out = new Uint8Array(bin.length);
  for (let i = 0; i < bin.length; i++) out[i] = bin.charCodeAt(i);
  return out;
}

/** App-lock idle presets (seconds). 0 = lock immediately when the app backgrounds;
 *  Infinity-ish handled by "off". */
export const APP_LOCK_TIMEOUTS: { seconds: number; label: string }[] = [
  { seconds: 0, label: "Immediately" },
  { seconds: 60, label: "After 1 minute" },
  { seconds: 900, label: "After 15 minutes" },
  { seconds: 3600, label: "After 1 hour" },
];

/** shouldRelock decides whether a biometric-locked app must re-authenticate:
 *  true once `timeoutSeconds` have elapsed since the app was last active. */
export function shouldRelock(lastActiveMs: number, timeoutSeconds: number, now: number): boolean {
  if (timeoutSeconds <= 0) return true; // lock immediately on background
  return now - lastActiveMs >= timeoutSeconds * 1000;
}

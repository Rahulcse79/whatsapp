// Device-linking QR payload (sequence-diagrams §8). The new (unauthenticated)
// device shows a QR carrying {link_token, deviceId, identityKey (its new public
// key)}; the primary scans it, approves, and signs the new device onto the list
// (deviceList.ts). Pure encode/parse — the QR rendering + camera are UI concerns.

export interface LinkQrPayload {
  linkToken: string;
  deviceId: string;
  /** The new device's public identity key. */
  identityKey: Uint8Array;
}

/** encodeLinkQr serializes the payload to a compact JSON string for the QR. */
export function encodeLinkQr(p: LinkQrPayload): string {
  return JSON.stringify({ v: 1, t: p.linkToken, d: p.deviceId, k: toHex(p.identityKey) });
}

/** parseLinkQr parses a scanned QR payload, or null when malformed / wrong
 *  version — a scanner should reject rather than trust a garbled code. */
export function parseLinkQr(s: string): LinkQrPayload | null {
  let o: unknown;
  try {
    o = JSON.parse(s);
  } catch {
    return null;
  }
  if (typeof o !== "object" || o === null) return null;
  const r = o as Record<string, unknown>;
  if (r.v !== 1 || typeof r.t !== "string" || typeof r.d !== "string" || typeof r.k !== "string") return null;
  const key = fromHex(r.k);
  if (!key || !r.t || !r.d) return null;
  return { linkToken: r.t, deviceId: r.d, identityKey: key };
}

function toHex(b: Uint8Array): string {
  let s = "";
  for (const x of b) s += x.toString(16).padStart(2, "0");
  return s;
}

function fromHex(s: string): Uint8Array | null {
  if (s.length === 0 || s.length % 2 !== 0) return null;
  const out = new Uint8Array(s.length / 2);
  for (let i = 0; i < out.length; i++) {
    const byte = parseInt(s.slice(i * 2, i * 2 + 2), 16);
    if (Number.isNaN(byte)) return null;
    out[i] = byte;
  }
  return out;
}

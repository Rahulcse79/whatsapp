// Standard base64 (RFC 4648) over raw bytes. The media-svc contract carries the
// content hash and the E2EE envelope's key material as base64 strings, and the
// codec must agree byte-for-byte with the Go side (encoding/base64.StdEncoding).
// Implemented by hand rather than via btoa/atob: those operate on "binary
// strings" (latin1) and blow the call stack on large inputs — a hazard for
// multi-megabyte media ciphertext.

const ALPHABET = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789+/";

/** toBase64 encodes bytes to a standard, padded base64 string. */
export function toBase64(bytes: Uint8Array): string {
  let out = "";
  for (let i = 0; i < bytes.length; i += 3) {
    const b0 = bytes[i]!;
    const has1 = i + 1 < bytes.length;
    const has2 = i + 2 < bytes.length;
    const b1 = has1 ? bytes[i + 1]! : 0;
    const b2 = has2 ? bytes[i + 2]! : 0;
    out += ALPHABET.charAt(b0 >> 2);
    out += ALPHABET.charAt(((b0 & 0x03) << 4) | (b1 >> 4));
    out += has1 ? ALPHABET.charAt(((b1 & 0x0f) << 2) | (b2 >> 6)) : "=";
    out += has2 ? ALPHABET.charAt(b2 & 0x3f) : "=";
  }
  return out;
}

/** fromBase64 decodes a standard base64 string, tolerating padding/whitespace. */
export function fromBase64(text: string): Uint8Array {
  const clean = text.replace(/[^A-Za-z0-9+/]/g, "");
  const full = clean.length >> 2; // complete 4-char groups
  const rem = clean.length & 3; // 0, 2, or 3 trailing chars
  const outLen = full * 3 + (rem === 0 ? 0 : rem - 1);
  const out = new Uint8Array(outLen);
  let o = 0;
  let i = 0;
  for (let g = 0; g < full; g++, i += 4) {
    const n = (sextet(clean, i) << 18) | (sextet(clean, i + 1) << 12) | (sextet(clean, i + 2) << 6) | sextet(clean, i + 3);
    out[o++] = (n >> 16) & 0xff;
    out[o++] = (n >> 8) & 0xff;
    out[o++] = n & 0xff;
  }
  if (rem === 2) {
    const n = (sextet(clean, i) << 18) | (sextet(clean, i + 1) << 12);
    out[o++] = (n >> 16) & 0xff;
  } else if (rem === 3) {
    const n = (sextet(clean, i) << 18) | (sextet(clean, i + 1) << 12) | (sextet(clean, i + 2) << 6);
    out[o++] = (n >> 16) & 0xff;
    out[o++] = (n >> 8) & 0xff;
  }
  return out;
}

function sextet(s: string, i: number): number {
  const v = ALPHABET.indexOf(s.charAt(i));
  return v < 0 ? 0 : v;
}

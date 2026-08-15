// convCrypto — DEV DOUBLE conversation cipher. React Native has no WebCrypto, so
// (unlike the web worker's AES-GCM) this is a small pure-JS stream AEAD keyed off
// the conversation id. Both participants derive the SAME key from the shared
// conversation id, so real 1:1 messaging works: the sender seals, the recipient
// opens. INSECURE by construction — anyone who learns the conversation id can
// read it — and it never ships: real per-device libsignal E2EE (keys directory
// + E2EEEngine) is the seam that replaces it. Wire: nonce(12) ‖ mac(8) ‖ ct.

const enc = new TextEncoder();
const dec = new TextDecoder();

// hash: a 32-byte NON-CRYPTOGRAPHIC PRF (FNV-1a seed → xorshift32 stream),
// mirroring the crypto-wrapper dev digest. Deterministic across devices.
function hash(input: Uint8Array): Uint8Array {
  let h = 0x811c9dc5 >>> 0;
  for (let i = 0; i < input.length; i++) {
    h ^= input[i]!;
    h = Math.imul(h, 0x01000193) >>> 0;
  }
  const out = new Uint8Array(32);
  let s = (h || 0x9e3779b9) >>> 0;
  for (let i = 0; i < 32; i++) {
    s ^= s << 13;
    s >>>= 0;
    s ^= s >>> 17;
    s ^= s << 5;
    s >>>= 0;
    out[i] = s & 0xff;
  }
  return out;
}

function cat(...parts: Uint8Array[]): Uint8Array {
  let n = 0;
  for (const p of parts) n += p.length;
  const out = new Uint8Array(n);
  let off = 0;
  for (const p of parts) {
    out.set(p, off);
    off += p.length;
  }
  return out;
}

function u32(n: number): Uint8Array {
  return new Uint8Array([(n >>> 24) & 0xff, (n >>> 16) & 0xff, (n >>> 8) & 0xff, n & 0xff]);
}

function keyOf(conversationId: string): Uint8Array {
  return hash(enc.encode("wa-dev-conv-v1:" + conversationId));
}

function keystream(key: Uint8Array, nonce: Uint8Array, len: number): Uint8Array {
  const out = new Uint8Array(len);
  for (let off = 0, ctr = 0; off < len; off += 32, ctr++) {
    const block = hash(cat(key, nonce, u32(ctr)));
    out.set(block.subarray(0, Math.min(32, len - off)), off);
  }
  return out;
}

// A per-message nonce. Non-crypto randomness is fine for a dev double: it only
// needs to vary the keystream between messages.
function nonce12(): Uint8Array {
  const n = new Uint8Array(12);
  for (let i = 0; i < 12; i++) n[i] = Math.floor(Math.random() * 256);
  return n;
}

/** sealForConversation encrypts a message body for its conversation. */
export function sealForConversation(conversationId: string, plaintext: string): Uint8Array {
  const key = keyOf(conversationId);
  const nonce = nonce12();
  const pt = enc.encode(plaintext);
  const ks = keystream(key, nonce, pt.length);
  const ct = new Uint8Array(pt.length);
  for (let i = 0; i < pt.length; i++) ct[i] = pt[i]! ^ ks[i]!;
  const mac = hash(cat(key, nonce, ct)).subarray(0, 8);
  return cat(nonce, mac, ct);
}

/** openForConversation decrypts an inbound envelope; throws on a bad MAC (a
 *  foreign/rotated key), which the caller treats as "leave as placeholder". */
export function openForConversation(conversationId: string, envelope: Uint8Array): string {
  if (envelope.length < 20) throw new Error("short envelope");
  const key = keyOf(conversationId);
  const nonce = envelope.subarray(0, 12);
  const mac = envelope.subarray(12, 20);
  const ct = envelope.subarray(20);
  const expect = hash(cat(key, nonce, ct)).subarray(0, 8);
  for (let i = 0; i < 8; i++) if (mac[i] !== expect[i]) throw new Error("bad mac");
  const ks = keystream(key, nonce, ct.length);
  const pt = new Uint8Array(ct.length);
  for (let i = 0; i < ct.length; i++) pt[i] = ct[i]! ^ ks[i]!;
  return dec.decode(pt);
}

// DevSessionCipher — an INSECURE dev/test double that models the *shape* of a
// real Signal session so the E2EE integration (engine.ts) is testable end to end
// WITHOUT real cryptography. e2ee-design.md's iron rule stands: production wraps
// libsignal (X3DH + Double Ratchet); we never ship hand-rolled crypto. This
// double reproduces two properties the integration depends on:
//
//   • X3DH shared secret — both peers derive the SAME session root from the pair
//     of identity public keys (symmetric), so Alice-encrypt / Bob-decrypt agree;
//   • Double-Ratchet symmetric chain — every message uses a fresh per-message key
//     derived from (root, sender, counter), so repeats never share a key and the
//     wire bytes reveal no plaintext.
//
// The PRF below is a NON-CRYPTOGRAPHIC hash (FNV-1a + xorshift). Never ship it.

import { addressKey, type DeviceAddress, type PrekeyBundle, type SessionCipher } from "./cipher";

/** A device's identity for the dev cipher: the public key goes in its bundle;
 *  the secret never leaves the device. */
export interface DeviceSecret {
  address: DeviceAddress;
  publicKey: Uint8Array;
  secret: Uint8Array;
}

/** Thrown when an envelope fails its authentication tag (wrong session / tamper). */
export class DecryptError extends Error {
  constructor(message = "authentication failed") {
    super(message);
    this.name = "DecryptError";
  }
}

interface Session {
  root: Uint8Array;
  peerPublicKey: Uint8Array;
  sendCounter: number;
}

export class DevSessionCipher implements SessionCipher {
  private readonly sessions = new Map<string, Session>();

  constructor(private readonly self: DeviceSecret) {}

  establish(bundle: PrekeyBundle): Promise<void> {
    this.sessions.set(addressKey(bundle.address), {
      root: sharedSecret(this.self.publicKey, bundle.identityKey),
      peerPublicKey: bundle.identityKey,
      sendCounter: 0,
    });
    return Promise.resolve();
  }

  encrypt(address: DeviceAddress, plaintext: Uint8Array): Promise<Uint8Array> {
    const session = this.require(address);
    const n = session.sendCounter++;
    const mk = messageKey(session.root, this.self.publicKey, n); // sender = self
    return Promise.resolve(sealMessage(mk, n, plaintext));
  }

  decrypt(address: DeviceAddress, envelope: Uint8Array): Promise<Uint8Array> {
    const session = this.require(address);
    const n = readU32(envelope, 0);
    const tag = envelope.subarray(4, 12);
    const ciphertext = envelope.subarray(12);
    const mk = messageKey(session.root, session.peerPublicKey, n); // sender = peer
    if (!bytesEqual(tag, mac(mk, ciphertext))) throw new DecryptError();
    return Promise.resolve(xor(ciphertext, keystream(mk, ciphertext.length)));
  }

  hasSession(address: DeviceAddress): boolean {
    return this.sessions.has(addressKey(address));
  }

  private require(address: DeviceAddress): Session {
    const session = this.sessions.get(addressKey(address));
    if (!session) throw new Error(`no session established for ${addressKey(address)}`);
    return session;
  }
}

/** generateDevIdentity derives a device identity from its address + a seed. The
 *  "public key" is a one-way function of the secret (mock). */
export function generateDevIdentity(address: DeviceAddress, seed: Uint8Array = randomSeed()): DeviceSecret {
  const secret = digest(concat(utf8(addressKey(address)), seed));
  return { address, publicKey: digest(concat(secret, utf8("identity"))), secret };
}

/** devBundle builds the public bundle other devices fetch to open a session. */
export function devBundle(id: DeviceSecret): PrekeyBundle {
  return {
    address: id.address,
    identityKey: id.publicKey,
    signedPrekey: digest(concat(id.publicKey, utf8("signed-prekey"))),
  };
}

// ── mock primitives (INSECURE — never shipped) ──────────────────────────────

function sharedSecret(a: Uint8Array, b: Uint8Array): Uint8Array {
  // Symmetric in the two identity keys so both peers derive the same root.
  const [lo, hi] = compareBytes(a, b) <= 0 ? [a, b] : [b, a];
  return digest(concat(utf8("x3dh"), lo, hi));
}

function messageKey(root: Uint8Array, sender: Uint8Array, n: number): Uint8Array {
  return digest(concat(root, sender, u32(n)));
}

function mac(mk: Uint8Array, ciphertext: Uint8Array): Uint8Array {
  return digest(concat(utf8("mac"), mk, ciphertext)).subarray(0, 8);
}

function sealMessage(mk: Uint8Array, n: number, plaintext: Uint8Array): Uint8Array {
  const ciphertext = xor(plaintext, keystream(mk, plaintext.length));
  return concat(u32(n), mac(mk, ciphertext), ciphertext); // n(4) || tag(8) || ct
}

function keystream(mk: Uint8Array, len: number): Uint8Array {
  const out = new Uint8Array(len);
  let filled = 0;
  for (let ctr = 0; filled < len; ctr++) {
    const block = digest(concat(utf8("ks"), mk, u32(ctr)));
    const take = Math.min(block.length, len - filled);
    out.set(block.subarray(0, take), filled);
    filled += take;
  }
  return out;
}

/** digest: a 32-byte NON-CRYPTOGRAPHIC PRF (FNV-1a seed → xorshift32 stream). */
function digest(input: Uint8Array): Uint8Array {
  let h = 0x811c9dc5 >>> 0;
  for (let i = 0; i < input.length; i++) {
    h = (h ^ input[i]!) >>> 0;
    h = Math.imul(h, 0x01000193) >>> 0;
  }
  let x = (h ^ 0x9e3779b9) >>> 0;
  if (x === 0) x = 0x1a2b3c4d;
  const out = new Uint8Array(32);
  for (let i = 0; i < 32; i++) {
    x ^= x << 13;
    x >>>= 0;
    x ^= x >>> 17;
    x ^= x << 5;
    x >>>= 0;
    out[i] = x & 0xff;
  }
  return out;
}

function utf8(s: string): Uint8Array {
  // ASCII labels/ids only — good enough for a deterministic mock.
  const out = new Uint8Array(s.length);
  for (let i = 0; i < s.length; i++) out[i] = s.charCodeAt(i) & 0xff;
  return out;
}

function randomSeed(): Uint8Array {
  const out = new Uint8Array(16);
  for (let i = 0; i < 16; i++) out[i] = Math.floor(Math.random() * 256);
  return out;
}

function concat(...arrays: Uint8Array[]): Uint8Array {
  let len = 0;
  for (const a of arrays) len += a.length;
  const out = new Uint8Array(len);
  let off = 0;
  for (const a of arrays) {
    out.set(a, off);
    off += a.length;
  }
  return out;
}

function xor(data: Uint8Array, key: Uint8Array): Uint8Array {
  const out = new Uint8Array(data.length);
  for (let i = 0; i < data.length; i++) out[i] = data[i]! ^ key[i]!;
  return out;
}

function u32(n: number): Uint8Array {
  return new Uint8Array([(n >>> 24) & 0xff, (n >>> 16) & 0xff, (n >>> 8) & 0xff, n & 0xff]);
}

function readU32(buf: Uint8Array, off: number): number {
  return ((buf[off]! << 24) | (buf[off + 1]! << 16) | (buf[off + 2]! << 8) | buf[off + 3]!) >>> 0;
}

function bytesEqual(a: Uint8Array, b: Uint8Array): boolean {
  if (a.length !== b.length) return false;
  let d = 0;
  for (let i = 0; i < a.length; i++) d |= a[i]! ^ b[i]!;
  return d === 0;
}

function compareBytes(a: Uint8Array, b: Uint8Array): number {
  const n = Math.min(a.length, b.length);
  for (let i = 0; i < n; i++) {
    const d = a[i]! - b[i]!;
    if (d !== 0) return d;
  }
  return a.length - b.length;
}

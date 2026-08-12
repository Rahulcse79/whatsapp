// Signed device lists (e2ee-design §5) — multi-device trust. The user's PRIMARY
// device signs its device list with its identity key; every other client verifies
// that a sender device is on that signed list before trusting it (this is the
// DeviceTrust the E2EEEngine checks in open()). A server that inserts a rogue
// device either fails the signature or forces a *visible* primary-key change —
// the key-change warning §5 describes.
//
// SignatureScheme is INJECTED: production binds it to libsignal's XEdDSA over the
// Curve25519 identity key (verifiers hold only the public key). createDevSignatureScheme
// is an INSECURE dev double (a symmetric keyed hash, like DevSessionCipher stands
// in for X3DH). The device-list LOGIC here — canonical signing, verification,
// primary-key pinning, membership — is real and independent of the primitive.

import type { DeviceAddress } from "./cipher";
import type { DeviceTrust } from "./engine";

export interface SignatureScheme {
  sign(privateKey: Uint8Array, message: Uint8Array): Uint8Array;
  verify(publicKey: Uint8Array, message: Uint8Array, sig: Uint8Array): boolean;
}

/** One device on a user's list: its id, identity key, and the primary's
 *  signature over that binding (`devices.cert`). */
export interface DeviceEntry {
  deviceId: string;
  identityKey: Uint8Array;
  cert: Uint8Array;
}

/** A user's signed device list (§5): every entry plus the whole list signed by
 *  the primary key, versioned so add/remove is ordered. */
export interface SignedDeviceList {
  userId: string;
  /** The key the list + certs verify against (public in production). */
  primaryKey: Uint8Array;
  devices: DeviceEntry[];
  version: number;
  signature: Uint8Array;
}

/** A device to be signed onto the list (its cert is filled in by signDeviceList). */
export interface DeviceInput {
  deviceId: string;
  identityKey: Uint8Array;
}

const enc = new TextEncoder();

/** certMessage is the canonical binding a device cert signs: (userId, deviceId,
 *  identityKey), length-framed so fields can't be confused. */
export function certMessage(userId: string, d: DeviceInput): Uint8Array {
  return frame([enc.encode("wa-devcert"), enc.encode(userId), enc.encode(d.deviceId), d.identityKey]);
}

/** listMessage is the canonical binding the whole list signs: version + every
 *  (deviceId, identityKey) in order. */
export function listMessage(userId: string, version: number, devices: DeviceInput[]): Uint8Array {
  const parts: Uint8Array[] = [enc.encode("wa-devlist"), enc.encode(userId), enc.encode(String(version))];
  for (const d of devices) {
    parts.push(enc.encode(d.deviceId), d.identityKey);
  }
  return frame(parts);
}

/** signDeviceList produces a SignedDeviceList: the primary signs each device cert
 *  and the list as a whole. */
export function signDeviceList(
  scheme: SignatureScheme,
  primaryPrivateKey: Uint8Array,
  primaryPublicKey: Uint8Array,
  userId: string,
  version: number,
  devices: DeviceInput[],
): SignedDeviceList {
  const entries: DeviceEntry[] = devices.map((d) => ({
    deviceId: d.deviceId,
    identityKey: d.identityKey,
    cert: scheme.sign(primaryPrivateKey, certMessage(userId, d)),
  }));
  return {
    userId,
    primaryKey: primaryPublicKey,
    devices: entries,
    version,
    signature: scheme.sign(primaryPrivateKey, listMessage(userId, version, devices)),
  };
}

/** verifyDeviceList checks the list signature AND every device cert against the
 *  carried primary key. Any single failure rejects the whole list. */
export function verifyDeviceList(scheme: SignatureScheme, list: SignedDeviceList): boolean {
  if (!scheme.verify(list.primaryKey, listMessage(list.userId, list.version, list.devices), list.signature)) {
    return false;
  }
  for (const d of list.devices) {
    if (!scheme.verify(list.primaryKey, certMessage(list.userId, d), d.cert)) return false;
  }
  return true;
}

/**
 * SignedDeviceListTrust is a DeviceTrust backed by verified signed device lists.
 * It PINS each user's primary key on first learn (TOFU); a later list under a
 * DIFFERENT primary key is rejected — the visible key-change event §5 describes
 * (surfaced to the user as a safety-number warning). A device is trusted only if
 * it is on the user's currently-verified list.
 */
export class SignedDeviceListTrust implements DeviceTrust {
  private readonly pinned = new Map<string, string>(); // userId → primaryKey (hex)
  private readonly devices = new Map<string, Set<string>>(); // userId → trusted deviceIds

  constructor(private readonly scheme: SignatureScheme) {}

  /** learn accepts a signed device list iff it verifies and its primary key
   *  matches the pinned one (or none is pinned yet). Returns false on rejection —
   *  the caller surfaces a key-change warning. */
  learn(list: SignedDeviceList): boolean {
    if (!verifyDeviceList(this.scheme, list)) return false;
    const key = toHex(list.primaryKey);
    const pinned = this.pinned.get(list.userId);
    if (pinned === undefined) this.pinned.set(list.userId, key);
    else if (pinned !== key) return false; // primary key changed → reject
    this.devices.set(list.userId, new Set(list.devices.map((d) => d.deviceId)));
    return true;
  }

  isTrusted(address: DeviceAddress): boolean {
    return this.devices.get(address.userId)?.has(address.deviceId) ?? false;
  }
}

/** createDevSignatureScheme — an INSECURE dev double (symmetric keyed hash; the
 *  "public" key equals the signing key). It models the API and drives the tests;
 *  production injects libsignal XEdDSA where verifiers hold only the public key. */
export function createDevSignatureScheme(): SignatureScheme {
  return {
    sign: (key, msg) => digest(cat(key, msg)),
    verify: (key, msg, sig) => eqBytes(digest(cat(key, msg)), sig),
  };
}

// ── self-contained helpers (the dev digest is NON-cryptographic) ─────────────

function frame(parts: Uint8Array[]): Uint8Array {
  let len = 0;
  for (const p of parts) len += 4 + p.length;
  const out = new Uint8Array(len);
  let o = 0;
  for (const p of parts) {
    out[o++] = (p.length >>> 24) & 0xff;
    out[o++] = (p.length >>> 16) & 0xff;
    out[o++] = (p.length >>> 8) & 0xff;
    out[o++] = p.length & 0xff;
    out.set(p, o);
    o += p.length;
  }
  return out;
}

function cat(a: Uint8Array, b: Uint8Array): Uint8Array {
  const o = new Uint8Array(a.length + b.length);
  o.set(a);
  o.set(b, a.length);
  return o;
}

function eqBytes(a: Uint8Array, b: Uint8Array): boolean {
  if (a.length !== b.length) return false;
  let d = 0;
  for (let i = 0; i < a.length; i++) d |= a[i]! ^ b[i]!;
  return d === 0;
}

function toHex(b: Uint8Array): string {
  let s = "";
  for (const x of b) s += x.toString(16).padStart(2, "0");
  return s;
}

/** digest — a 32-byte NON-cryptographic hash (FNV-1a per lane); dev double only. */
function digest(data: Uint8Array): Uint8Array {
  const out = new Uint8Array(32);
  for (let lane = 0; lane < 32; lane++) {
    let h = (0x811c9dc5 ^ (lane * 0x01000193)) >>> 0;
    for (let i = 0; i < data.length; i++) {
      h ^= data[i]!;
      h = Math.imul(h, 0x01000193);
      h ^= h >>> 15;
    }
    out[lane] = h & 0xff;
  }
  return out;
}

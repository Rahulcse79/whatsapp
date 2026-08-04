// The E2EE session contract the rest of the client depends on. Only this
// package touches libsignal (Docs/13-standards/coding-standards.md): UI and
// sync code see opaque sealed envelopes, never key material. The real
// implementation wraps libsignal (X3DH + Double Ratchet, e2ee-design §2); this
// file defines the interface and a test double.

/** A per-device identity — the unit a Signal session is keyed on. */
export interface DeviceAddress {
  userId: string;
  deviceId: string;
}

/** Public prekey material fetched to open a session (X3DH). No private keys. */
export interface PrekeyBundle {
  address: DeviceAddress;
  identityKey: Uint8Array;
  signedPrekey: Uint8Array;
  oneTimePrekey?: Uint8Array;
}

/**
 * SessionCipher establishes per-device sessions and seals/opens messages. A
 * session must be established before encrypt/decrypt; implementations reject
 * otherwise. Ciphertext is an opaque sealed envelope — the transport and
 * server never inspect it.
 */
export interface SessionCipher {
  establish(bundle: PrekeyBundle): Promise<void>;
  encrypt(address: DeviceAddress, plaintext: Uint8Array): Promise<Uint8Array>;
  decrypt(address: DeviceAddress, ciphertext: Uint8Array): Promise<Uint8Array>;
  hasSession(address: DeviceAddress): boolean;
}

export function addressKey(a: DeviceAddress): string {
  return `${a.userId}:${a.deviceId}`;
}

/**
 * MockSessionCipher is an INSECURE test double — a reversible XOR keyed on the
 * bundle's public identity key. It exercises the SessionCipher contract
 * (establish-before-use, opaque ciphertext, round-trip, per-session keys)
 * WITHOUT real cryptography. Never ship it; the production cipher wraps
 * libsignal.
 */
export class MockSessionCipher implements SessionCipher {
  private readonly keys = new Map<string, Uint8Array>();

  async establish(bundle: PrekeyBundle): Promise<void> {
    this.keys.set(addressKey(bundle.address), deriveKeystream(bundle));
  }

  async encrypt(address: DeviceAddress, plaintext: Uint8Array): Promise<Uint8Array> {
    return xor(plaintext, this.requireKey(address));
  }

  async decrypt(address: DeviceAddress, ciphertext: Uint8Array): Promise<Uint8Array> {
    return xor(ciphertext, this.requireKey(address));
  }

  hasSession(address: DeviceAddress): boolean {
    return this.keys.has(addressKey(address));
  }

  private requireKey(address: DeviceAddress): Uint8Array {
    const key = this.keys.get(addressKey(address));
    if (!key) {
      throw new Error(`no session established for ${addressKey(address)}`);
    }
    return key;
  }
}

function deriveKeystream(bundle: PrekeyBundle): Uint8Array {
  // Combine the public material so distinct bundles yield distinct keystreams.
  const parts = [bundle.identityKey, bundle.signedPrekey, bundle.oneTimePrekey ?? new Uint8Array()];
  const total = parts.reduce((n, p) => n + p.length, 0) || 1;
  const out = new Uint8Array(total);
  let offset = 0;
  for (const p of parts) {
    out.set(p, offset);
    offset += p.length;
  }
  return out.length ? out : new Uint8Array([0x5a]);
}

function xor(data: Uint8Array, key: Uint8Array): Uint8Array {
  const out = new Uint8Array(data.length);
  for (let i = 0; i < data.length; i++) {
    out[i] = data[i]! ^ key[i % key.length]!;
  }
  return out;
}

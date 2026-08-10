// E2EEEngine — the integration layer over a SessionCipher (e2ee-design.md §2).
// It owns what libsignal does NOT: fetching recipient bundles, establishing a
// session per recipient device, fanning a message out to every recipient device
// PLUS the sender's own other devices (self-sync copies), and — on receive —
// verifying the sender device is on a trusted signed device list (§5) before
// decrypting. The cipher stays swappable: DevSessionCipher in tests/dev, the
// libsignal-backed cipher in production.

import { addressKey, type DeviceAddress, type PrekeyBundle, type SessionCipher } from "./cipher";

/** KeyDirectory resolves a user's per-device public bundles
 *  (GET /keys/bundle/{user} — one bundle per device, e2ee-design §2). */
export interface KeyDirectory {
  bundlesFor(userId: string): Promise<PrekeyBundle[]>;
}

/** DeviceTrust answers whether a device is on the user's signed device list
 *  (§5). A server that inserts a rogue device fails this check. */
export interface DeviceTrust {
  isTrusted(address: DeviceAddress): boolean | Promise<boolean>;
}

/** One recipient device's sealed copy — what the client fans out over msg.send. */
export interface SealedForDevice {
  address: DeviceAddress;
  envelope: Uint8Array;
}

export class UntrustedDeviceError extends Error {
  constructor(readonly address: DeviceAddress) {
    super(`device not on a trusted device list: ${addressKey(address)}`);
    this.name = "UntrustedDeviceError";
  }
}

export class NoBundleError extends Error {
  constructor(readonly address: DeviceAddress) {
    super(`no key bundle for ${addressKey(address)}`);
    this.name = "NoBundleError";
  }
}

export class E2EEEngine {
  constructor(
    private readonly cipher: SessionCipher,
    private readonly directory: KeyDirectory,
    private readonly trust: DeviceTrust,
    private readonly self: DeviceAddress,
  ) {}

  /**
   * seal encrypts plaintext once per recipient device AND once per other device
   * of the sender (self-sync copies), establishing sessions on demand. Returns
   * one sealed envelope per target device; the server relays each opaquely.
   */
  async seal(recipientUserId: string, plaintext: Uint8Array): Promise<SealedForDevice[]> {
    const recipients = await this.directory.bundlesFor(recipientUserId);
    const ownOthers = (await this.directory.bundlesFor(this.self.userId)).filter(
      (b) => b.address.deviceId !== this.self.deviceId,
    );

    const out: SealedForDevice[] = [];
    for (const bundle of [...recipients, ...ownOthers]) {
      await this.ensureSession(bundle.address, () => Promise.resolve(bundle));
      out.push({ address: bundle.address, envelope: await this.cipher.encrypt(bundle.address, plaintext) });
    }
    return out;
  }

  /**
   * open decrypts an inbound envelope after checking the sender device is
   * trusted. The first message from a device establishes the inbound session
   * from its bundle (real libsignal parses the X3DH header from the envelope;
   * here we resolve the bundle from the directory).
   */
  async open(from: DeviceAddress, envelope: Uint8Array): Promise<Uint8Array> {
    if (!(await this.trust.isTrusted(from))) throw new UntrustedDeviceError(from);
    await this.ensureSession(from, async () => {
      const bundle = (await this.directory.bundlesFor(from.userId)).find(
        (b) => b.address.deviceId === from.deviceId,
      );
      if (!bundle) throw new NoBundleError(from);
      return bundle;
    });
    return this.cipher.decrypt(from, envelope);
  }

  private async ensureSession(address: DeviceAddress, resolve: () => Promise<PrekeyBundle>): Promise<void> {
    if (this.cipher.hasSession(address)) return;
    await this.cipher.establish(await resolve());
  }
}

/** InMemoryKeyDirectory is a KeyDirectory for tests/offline dev; production
 *  fetches bundles from the keys service over REST. */
export class InMemoryKeyDirectory implements KeyDirectory {
  private readonly byUser = new Map<string, PrekeyBundle[]>();

  add(bundle: PrekeyBundle): void {
    const list = this.byUser.get(bundle.address.userId) ?? [];
    list.push(bundle);
    this.byUser.set(bundle.address.userId, list);
  }

  bundlesFor(userId: string): Promise<PrekeyBundle[]> {
    return Promise.resolve(this.byUser.get(userId) ?? []);
  }
}

/** Trusts every device — the interim posture until signed device lists are
 *  wired (matches the server-side presence/authorizer AllowAll seams). */
export const trustAll: DeviceTrust = { isTrusted: () => true };

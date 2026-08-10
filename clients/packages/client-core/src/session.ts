// Session lifecycle: the tokens + device id + resume token, persisted in the
// SecureStore (Keychain / Android Keystore). WsClient reads these through a
// SessionProvider adapter (see appServices).

import type { SecureStore } from "./ports";

export interface Session {
  accessJwt: string;
  refreshToken: string;
  deviceId: string;
}

const KEYS = {
  access: "wa.access",
  refresh: "wa.refresh",
  device: "wa.device",
  resume: "wa.resume",
} as const;

export class SessionManager {
  private session: Session | null = null;
  private resume: string | undefined;

  constructor(private readonly store: SecureStore) {}

  /** load hydrates the session from secure storage on app start. */
  async load(): Promise<Session | null> {
    const [access, refresh, device, resume] = await Promise.all([
      this.store.get(KEYS.access),
      this.store.get(KEYS.refresh),
      this.store.get(KEYS.device),
      this.store.get(KEYS.resume),
    ]);
    this.resume = resume ?? undefined;
    this.session = access && refresh && device ? { accessJwt: access, refreshToken: refresh, deviceId: device } : null;
    return this.session;
  }

  current(): Session | null {
    return this.session;
  }

  async save(s: Session): Promise<void> {
    this.session = s;
    await Promise.all([
      this.store.set(KEYS.access, s.accessJwt),
      this.store.set(KEYS.refresh, s.refreshToken),
      this.store.set(KEYS.device, s.deviceId),
    ]);
  }

  /** updateTokens persists a rotated access/refresh pair after /refresh. */
  async updateTokens(accessJwt: string, refreshToken: string): Promise<void> {
    if (!this.session) return;
    this.session = { ...this.session, accessJwt, refreshToken };
    await Promise.all([this.store.set(KEYS.access, accessJwt), this.store.set(KEYS.refresh, refreshToken)]);
  }

  resumeToken(): string | undefined {
    return this.resume;
  }

  /** setResumeToken records the latest HelloAck resume token (fire-and-forget
   *  persistence — WsClient calls the sync SessionProvider wrapper). */
  async setResumeToken(token: string): Promise<void> {
    this.resume = token;
    await this.store.set(KEYS.resume, token);
  }

  async clear(): Promise<void> {
    this.session = null;
    this.resume = undefined;
    await Promise.all(Object.values(KEYS).map((k) => this.store.delete(k)));
  }
}

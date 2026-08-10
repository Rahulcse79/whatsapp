// AppServices assembles the framework-free core with the RN/Expo platform
// adapters into the single object the UI talks to. It owns the realtime
// connection, the local store, and the send path. Screens never touch ports,
// sockets, or SQL directly.

import { MockSessionCipher } from "@wa/crypto-wrapper";
import { Cursors } from "@wa/sync-engine";
import { MessageStore } from "../core/db/messageStore";
import { newId } from "../core/ids";
import { OtpClient, type VerifiedSession } from "../core/otpClient";
import type { HttpClient, SqliteDB } from "../core/ports";
import { SessionManager } from "../core/session";
import { WsClient, type SessionProvider } from "../core/wsClient";
import { createHttpClient } from "../platform/httpClient";
import { openDatabase } from "../platform/expoSqlite";
import { rnScheduler } from "../platform/scheduler";
import { secureStore } from "../platform/secureStore";
import { createWsTransportFactory } from "../platform/wsTransport";

export interface AppConfig {
  apiBaseUrl: string;
  wsUrl: string;
}

// Defaults point at a locally self-hosted stack (the project runs fully
// offline — HLD §17.5). Override for a remote deployment.
export const defaultConfig: AppConfig = {
  apiBaseUrl: "http://localhost:8080",
  wsUrl: "ws://localhost:8080/v1/ws",
};

export class AppServices {
  readonly otp: OtpClient;
  readonly sessions: SessionManager;

  private store!: MessageStore;
  private readonly cursors = new Cursors();
  private readonly cipher = new MockSessionCipher();
  private readonly encoder = new TextEncoder();
  private ws: WsClient | null = null;

  private constructor(
    private readonly cfg: AppConfig,
    http: HttpClient,
  ) {
    this.otp = new OtpClient(http);
    this.sessions = new SessionManager(secureStore);
  }

  static async create(cfg: AppConfig = defaultConfig): Promise<AppServices> {
    const svc = new AppServices(cfg, createHttpClient(cfg.apiBaseUrl));
    const db: SqliteDB = await openDatabase();
    svc.store = new MessageStore(db, svc.cursors);
    await svc.store.init();
    await svc.sessions.load();
    return svc;
  }

  get messages(): MessageStore {
    return this.store;
  }

  hasSession(): boolean {
    return this.sessions.current() !== null;
  }

  /** completeLogin persists a verified session and opens the realtime link. */
  async completeLogin(s: VerifiedSession): Promise<void> {
    await this.sessions.save({ accessJwt: s.accessJwt, refreshToken: s.refreshToken, deviceId: s.deviceId });
    this.startRealtime();
  }

  /** startRealtime connects the WS state machine (idempotent). */
  startRealtime(): void {
    if (this.ws || !this.sessions.current()) return;
    const sessions = this.sessions;
    const store = this.store;

    const provider: SessionProvider = {
      accessJwt: () => sessions.current()?.accessJwt ?? "",
      deviceId: () => sessions.current()?.deviceId ?? "",
      resumeToken: () => sessions.resumeToken(),
      setResumeToken: (t) => {
        void sessions.setResumeToken(t);
      },
      cursors: () => store.cursorSnapshot(),
    };

    this.ws = new WsClient({
      transportFactory: createWsTransportFactory(this.cfg.wsUrl),
      scheduler: rnScheduler,
      session: provider,
      handlers: {
        onInboxBatch: (b) => store.persistInboxBatch(b),
        onMsgAck: (a) => {
          void store.markSent(a.clientRef, a.seq);
        },
        pendingSends: () => store.pendingSends(),
        onAuthExpired: () => {
          void this.refreshToken();
        },
        onRevoked: () => {
          void this.logout();
        },
      },
    });
    this.ws.start();
  }

  /** sendText seals + enqueues a message and nudges the outbox flush. */
  async sendText(conversationId: string, text: string): Promise<void> {
    const clientRef = newId();
    const payload = await this.seal(conversationId, text);
    await this.store.enqueueOutgoing({ clientRef, conversationId, plaintext: text, payload, now: Date.now() });
    await this.ws?.flush();
  }

  async logout(): Promise<void> {
    this.ws?.stop();
    this.ws = null;
    await this.sessions.clear();
  }

  private async refreshToken(): Promise<void> {
    const cur = this.sessions.current();
    if (!cur) return;
    try {
      const pair = await this.otp.refresh(cur.refreshToken);
      await this.sessions.updateTokens(pair.accessJwt, pair.refreshToken);
    } catch {
      // A reused/expired refresh token is fatal — drop the session.
      await this.logout();
    }
  }

  // Placeholder E2EE seam: seals via the crypto-wrapper contract so the send
  // path already speaks ciphertext. Real X3DH / Double-Ratchet sessions land in
  // T0.20 — MockSessionCipher is INSECURE and self-addressed here.
  private async seal(conversationId: string, text: string): Promise<Uint8Array> {
    const address = { userId: conversationId, deviceId: "peer" };
    if (!this.cipher.hasSession(address)) {
      await this.cipher.establish({
        address,
        identityKey: this.encoder.encode(conversationId),
        signedPrekey: this.encoder.encode("spk"),
      });
    }
    return this.cipher.encrypt(address, this.encoder.encode(text));
  }
}

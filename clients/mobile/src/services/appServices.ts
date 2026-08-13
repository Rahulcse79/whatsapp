// AppServices assembles the framework-free core with the RN/Expo platform
// adapters into the single object the UI talks to. It owns the realtime
// connection, the local store, and the send path. Screens never touch ports,
// sockets, or SQL directly.

import { DevSessionCipher, generateDevIdentity } from "@wa/crypto-wrapper";
import { encodeTextMessage, generateLinkPreview } from "@wa/media-pipeline";
import { Cursors } from "@wa/sync-engine";
import {
  MessageStore,
  OtpClient,
  SessionManager,
  WsClient,
  newId,
  type HttpClient,
  type SessionProvider,
  type SqliteDB,
  type VerifiedSession,
} from "@wa/client-core";
import { createHttpClient } from "../platform/httpClient";
import { openDatabase } from "../platform/expoSqlite";
import { rnHtmlFetcher } from "../platform/linkPreview";
import { rnScheduler } from "../platform/scheduler";
import { secureStore } from "../platform/secureStore";
import { createWsTransportFactory } from "../platform/wsTransport";

export interface AppConfig {
  apiBaseUrl: string;
  wsUrl: string;
}

// Backend endpoints. Baked in at build time from EXPO_PUBLIC_* (Expo inlines
// these into the bundle), falling back to a locally self-hosted stack matching
// ./start.sh — core-api on :8080, ws-gateway on :8081. For an APK that runs on
// a device/emulator, build with the reachable host, e.g.
//   EXPO_PUBLIC_API_URL=http://10.0.2.2:8080  (Android emulator → host)
//   EXPO_PUBLIC_WS_URL=ws://10.0.2.2:8081/v1/ws
// The Android APK workflow (.github/workflows/android.yml) sets these.
export const defaultConfig: AppConfig = {
  apiBaseUrl: process.env.EXPO_PUBLIC_API_URL ?? "http://localhost:8080",
  wsUrl: process.env.EXPO_PUBLIC_WS_URL ?? "ws://localhost:8081/v1/ws",
};

export class AppServices {
  readonly otp: OtpClient;
  readonly sessions: SessionManager;

  private store!: MessageStore;
  private readonly cursors = new Cursors();
  private readonly cipher = new DevSessionCipher(generateDevIdentity({ userId: "self", deviceId: "mobile" }));
  private readonly encoder = new TextEncoder();
  private ws: WsClient | null = null;

  private constructor(
    private readonly cfg: AppConfig,
    http: HttpClient,
  ) {
    this.otp = new OtpClient(http);
    this.sessions = new SessionManager(secureStore);
  }

  /** The backend endpoints this instance is bound to (user-configured). */
  get config(): AppConfig {
    return this.cfg;
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

  /** search runs a full-text query over the local decrypted store (ADR-005). */
  search: MessageStore["search"] = (query, opts) => this.store.search(query, opts);

  /** sendText generates a sender-side link preview (FR-MSG-08), seals the encoded
   *  body, and enqueues it. The preview rides in the same envelope; a URL-free
   *  message skips the fetch and any failure just sends plain text. */
  async sendText(conversationId: string, text: string): Promise<void> {
    const clientRef = newId();
    const preview = await generateLinkPreview(text, rnHtmlFetcher).catch(() => null);
    const body = encodeTextMessage(text, preview ?? undefined);
    const payload = await this.seal(conversationId, body);
    await this.store.enqueueOutgoing({ clientRef, conversationId, plaintext: body, listText: text, payload, now: Date.now() });
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

  // Seals via the T0.20 DevSessionCipher (INSECURE dev double) so the send path
  // speaks ciphertext through the real E2EE contract. Live per-device fan-out
  // (E2EEEngine + keys directory + real libsignal) activates once the keys API
  // is wired; self-addressed placeholder peer until then.
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

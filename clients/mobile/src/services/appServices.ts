// AppServices assembles the framework-free core with the RN/Expo platform
// adapters into the single object the UI talks to. It owns the realtime
// connection, the local store, and the send path. Screens never touch ports,
// sockets, or SQL directly.

import { encodeTextMessage, generateLinkPreview } from "@wa/media-pipeline";
import { openForConversation, sealForConversation } from "./convCrypto";
import { Cursors } from "@wa/sync-engine";
import {
  MessageStore,
  OtpClient,
  SessionManager,
  WsClient,
  newId,
  type CallKind,
  type HttpClient,
  type RingState,
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
  livekitUrl: string;
}

/** CallSignalHandler receives WS call-signaling frames (dev.{id}.call),
 *  forwarded to the CallProvider which drives the CallSession. */
export interface CallSignalHandler {
  onOffer(callerUserId: string, roomId: string, ringId: string, kind: CallKind): void;
  onRing(state: RingState): void;
  onEnd(reason: string): void;
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
  livekitUrl: process.env.EXPO_PUBLIC_LIVEKIT_URL ?? "ws://localhost:7880",
};

export class AppServices {
  readonly otp: OtpClient;
  readonly sessions: SessionManager;

  private store!: MessageStore;
  private readonly cursors = new Cursors();
  private ws: WsClient | null = null;
  private readonly http: HttpClient;
  private callHandler: CallSignalHandler | null = null; // set by CallProvider

  private constructor(
    private readonly cfg: AppConfig,
    http: HttpClient,
  ) {
    this.http = http;
    this.otp = new OtpClient(http);
    this.sessions = new SessionManager(secureStore);
  }

  /** The backend endpoints this instance is bound to (user-configured). */
  get config(): AppConfig {
    return this.cfg;
  }

  /** onCallSignal registers the CallProvider's handler for WS call frames; pass
   *  null to detach. Offers/rings/ends arrive on dev.{id}.call and drive the
   *  CallSession (outgoing calls + answer/decline go via REST in callControl). */
  onCallSignal(handler: CallSignalHandler | null): void {
    this.callHandler = handler;
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
        onInboxBatch: (b) => {
          // Decrypt each inbound ciphertext with the conversation-shared key so
          // the recipient stores real text, not the "[encrypted]" placeholder.
          // A bad MAC (foreign/rotated key) just leaves that item unresolved.
          const bodies = new Map<string, string>();
          for (const it of b.items) {
            try {
              bodies.set(it.msgUuid, openForConversation(it.conversationId, it.ciphertext));
            } catch {
              /* leave as placeholder */
            }
          }
          return store.persistInboxBatch(b, bodies);
        },
        onMsgAck: (a) => {
          void store.markSent(a.clientRef, a.seq);
        },
        onCallOffer: (o) => this.callHandler?.onOffer(o.callerUserId, o.roomId, o.ringId, o.kind),
        onCallRing: (r) => this.callHandler?.onRing(r.state),
        onCallEnd: (e) => this.callHandler?.onEnd(e.reason),
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
    const payload = sealForConversation(conversationId, body);
    await this.store.enqueueOutgoing({ clientRef, conversationId, plaintext: body, listText: text, payload, now: Date.now() });
    await this.ws?.flush();
  }

  /** startDirectChat resolves a phone number to a registered user, then gets (or
   *  creates) the shared 1:1 conversation and returns its id. Throws a friendly
   *  message if no account exists for that number (mirrors the web client). */
  async startDirectChat(phone: string): Promise<string> {
    const sync = (await this.authedJson("/v1/contacts/sync", { handles: [phone.trim()] })) as {
      matched?: Array<{ user_id?: string }>;
    };
    const peer = sync.matched?.[0]?.user_id;
    if (!peer) throw new Error("No account is registered with that number yet.");
    const conv = (await this.authedJson("/v1/conversations/direct", { peer_user_id: peer })) as {
      conversation_id?: string;
    };
    if (!conv.conversation_id) throw new Error("Couldn't start the conversation.");
    return conv.conversation_id;
  }

  // authedJson POSTs with the bearer token, refreshing once on a 401 (the
  // 10-minute access token may have lapsed since sign-in).
  private async authedJson(path: string, body: unknown): Promise<unknown> {
    const hdr = () => ({ authorization: `Bearer ${this.sessions.current()?.accessJwt ?? ""}` });
    let res = await this.http.post(path, body, hdr());
    if (res.status === 401) {
      await this.refreshToken();
      res = await this.http.post(path, body, hdr());
    }
    if (res.status >= 400) throw new Error("Request failed — please try again.");
    return res.json();
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
}

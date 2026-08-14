// AppServices (main thread) wires the shared WsClient + OtpClient + SessionManager
// to the DB/crypto worker. The store lives in the worker, so the main thread keeps
// a cursor mirror (updated from each persisted batch) to answer WsClient's
// synchronous Hello.last_cursors.

import {
  OtpClient,
  SessionManager,
  WsClient,
  newId,
  type ChatSummary,
  type ConversationCursor,
  type SearchHit,
  type SessionProvider,
  type ThreadMessage,
  type VerifiedSession,
} from "@wa/client-core";
import { encodeTextMessage, generateLinkPreview } from "@wa/media-pipeline";
import { config } from "../config";
import { createHttpClient } from "../platform/httpClient";
import { webHtmlFetcher } from "../platform/linkPreview";
import { webScheduler } from "../platform/scheduler";
import { indexedDbSecureStore } from "../platform/secureStore";
import { createWsTransportFactory } from "../platform/wsTransport";
import { createDbClient, type DbApi } from "../worker/rpc";

export class AppServices {
  readonly otp: OtpClient;
  readonly sessions: SessionManager;
  readonly db: DbApi;

  private readonly http = createHttpClient(config.apiBaseUrl);
  private ws: WsClient | null = null;
  private cursorMirror: ConversationCursor[] = [];

  private constructor() {
    this.otp = new OtpClient(createHttpClient(config.apiBaseUrl));
    this.sessions = new SessionManager(indexedDbSecureStore);
    const worker = new Worker(new URL("../worker/db.worker.ts", import.meta.url), { type: "module" });
    this.db = createDbClient(worker);
  }

  static async create(): Promise<AppServices> {
    const svc = new AppServices();
    svc.cursorMirror = await svc.db.init();
    await svc.sessions.load();
    return svc;
  }

  hasSession(): boolean {
    return this.sessions.current() !== null;
  }

  async completeLogin(s: VerifiedSession): Promise<void> {
    await this.sessions.save({ accessJwt: s.accessJwt, refreshToken: s.refreshToken, deviceId: s.deviceId });
    this.startRealtime();
  }

  startRealtime(): void {
    if (this.ws || !this.sessions.current()) return;
    const sessions = this.sessions;

    const provider: SessionProvider = {
      accessJwt: () => sessions.current()?.accessJwt ?? "",
      deviceId: () => sessions.current()?.deviceId ?? "",
      resumeToken: () => sessions.resumeToken(),
      setResumeToken: (t) => {
        void sessions.setResumeToken(t);
      },
      cursors: () => this.cursorMirror,
    };

    this.ws = new WsClient({
      transportFactory: createWsTransportFactory(config.wsUrl),
      scheduler: webScheduler,
      session: provider,
      handlers: {
        onInboxBatch: async (b) => {
          const watermark = await this.db.persistInboxBatch(b);
          this.mergeCursors(watermark);
          return watermark;
        },
        onMsgAck: (a) => {
          void this.db.markSent({ clientRef: a.clientRef, seq: a.seq });
        },
        pendingSends: () => this.db.pendingSends(),
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

  async sendText(conversationId: string, text: string): Promise<void> {
    // Generate the link preview on THIS (sender) device and carry it in the
    // sealed body (FR-MSG-08). Best-effort: a URL-free message skips the fetch,
    // and any failure just sends plain text.
    const preview = await generateLinkPreview(text, webHtmlFetcher).catch(() => null);
    const body = encodeTextMessage(text, preview ?? undefined);
    await this.db.enqueueText({ conversationId, text: body, listText: text, clientRef: newId(), now: Date.now() });
    await this.ws?.flush();
  }

  /** startDirectChat resolves a phone number to a registered user, then gets (or
   *  creates) the shared 1:1 conversation and returns its id. Throws a friendly
   *  message if no account exists for that number. */
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

  // authedJson POSTs with the bearer token, transparently refreshing once on a
  // 401 — the 10-minute access token may have lapsed since sign-in.
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

  conversations(): Promise<ChatSummary[]> {
    return this.db.conversations();
  }

  thread(conversationId: string): Promise<ThreadMessage[]> {
    return this.db.thread(conversationId);
  }

  /** Full-text search over the local decrypted store (runs in the DB worker). */
  search(query: string, opts?: { conversationId?: string; limit?: number }): Promise<SearchHit[]> {
    return this.db.search({ query, conversationId: opts?.conversationId, limit: opts?.limit });
  }

  async logout(): Promise<void> {
    this.ws?.stop();
    this.ws = null;
    await this.sessions.clear();
  }

  private mergeCursors(watermark: ConversationCursor[]): void {
    const map = new Map<string, number>();
    for (const c of this.cursorMirror) map.set(c.conversationId, c.lastSeq);
    for (const c of watermark) map.set(c.conversationId, Math.max(map.get(c.conversationId) ?? 0, c.lastSeq));
    this.cursorMirror = [...map.entries()].map(([conversationId, lastSeq]) => ({ conversationId, lastSeq }));
  }

  private async refreshToken(): Promise<void> {
    const cur = this.sessions.current();
    if (!cur) return;
    try {
      const pair = await this.otp.refresh(cur.refreshToken);
      await this.sessions.updateTokens(pair.accessJwt, pair.refreshToken);
    } catch {
      await this.logout();
    }
  }
}

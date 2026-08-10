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
  type SessionProvider,
  type ThreadMessage,
  type VerifiedSession,
} from "@wa/client-core";
import { config } from "../config";
import { createHttpClient } from "../platform/httpClient";
import { webScheduler } from "../platform/scheduler";
import { indexedDbSecureStore } from "../platform/secureStore";
import { createWsTransportFactory } from "../platform/wsTransport";
import { createDbClient, type DbApi } from "../worker/rpc";

export class AppServices {
  readonly otp: OtpClient;
  readonly sessions: SessionManager;
  readonly db: DbApi;

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
    await this.db.enqueueText({ conversationId, text, clientRef: newId(), now: Date.now() });
    await this.ws?.flush();
  }

  conversations(): Promise<ChatSummary[]> {
    return this.db.conversations();
  }

  thread(conversationId: string): Promise<ThreadMessage[]> {
    return this.db.thread(conversationId);
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

// AppServices (main thread) wires the shared WsClient + OtpClient + SessionManager
// to the DB/crypto worker. The store lives in the worker, so the main thread keeps
// a cursor mirror (updated from each persisted batch) to answer WsClient's
// synchronous Hello.last_cursors.

import {
  AiRuntime,
  DEFAULT_AI_SETTINGS,
  OtpClient,
  SessionManager,
  WsClient,
  b64urlToBytes,
  bytesToB64url,
  newId,
  type AiMode,
  type AiSettings,
  type CallKind,
  type ChatSummary,
  type ConversationCursor,
  type RingState,
  type SearchHit,
  type SessionProvider,
  type ThreadMessage,
  type VerifiedSession,
} from "@wa/client-core";
import { MediaPipeline, ResumableUploader, encodeContactCard, encodeLiveLocation, encodeLocation, encodeMediaMessage, encodePoll, encodeReaction, encodeSticker, encodeTextMessage, generateLinkPreview, parseMediaMessage, parseTextMessage, type QuotedRef } from "@wa/media-pipeline";
import { config } from "../config";
import { createHttpClient } from "../platform/httpClient";
import { webHtmlFetcher } from "../platform/linkPreview";
import { webUploadTransport } from "../platform/mediaUpload";
import { webScheduler } from "../platform/scheduler";
import { indexedDbSecureStore } from "../platform/secureStore";
import { createWsTransportFactory } from "../platform/wsTransport";
import { createDbClient, type DbApi } from "../worker/rpc";

export interface PublicProfile {
  username: string;
  displayName: string;
  about: string;
}

/** PasskeyInfo is a registered WebAuthn credential (T10.02). */
export interface PasskeyInfo {
  id: string;
  name: string;
  created_at_ms: number;
  last_used_at_ms?: number;
}

/** LoginInfo is one row of the recent-logins security surface (T10.02). */
export interface LoginInfo {
  device_id?: string;
  ip: string;
  user_agent?: string;
  at_ms: number;
  suspicious: boolean;
}

/** MyProfile is the self view — the public fields plus per-field privacy. */
export interface MyProfile extends PublicProfile {
  privacy: Record<string, string>;
}

/** CallHistoryItem is one metadata-only call record (FR-CALL-06). */
export interface CallHistoryItem {
  id: string;
  roomId: string;
  kind: number; // 1 voice · 2 video (wire enum)
  initiator: string;
  participants: string[];
  startedAt?: string;
  endedAt?: string;
  outcome: string;
}

/** UserRef is a metadata-only user reference (search hit / favorite). */
export interface UserRef {
  userId: string;
  username: string;
}

/** MatchedContact is a phone-sync hit, echoing the caller's handle. */
export interface MatchedContact extends UserRef {
  handle: string;
}

/** Invite is a personal invite-a-friend link. */
export interface Invite {
  token: string;
  url: string;
  expiresAtMs: number;
  maxUses: number;
}

/** GroupSettings mirrors domain.Settings (who-can-post / edit-info / announcements). */
export interface GroupSettings {
  who_can_post: string;
  who_can_edit_info: string;
  announcements: boolean;
}

/** GroupInfo is the client view of a group (GET/POST /v1/groups). */
export interface GroupInfo {
  id: string;
  name: string;
  description: string;
  settings: GroupSettings;
  version: number;
  myRole: string; // owner | admin | member
}

/** GroupMember is one member row (user id + role name). */
export interface GroupMember {
  userId: string;
  role: string;
}

/** GroupInviteLink is a group invite (token + shareable URL; qr === url for now). */
export interface GroupInviteLink {
  token: string;
  url: string;
  qr: string;
}

/** StoryFeedItem is one entry from GET /v1/stories/feed (metadata only — the
 *  content itself is E2EE and rides the STORY_KEY channel). */
export interface StoryFeedItem {
  storyId: string;
  author: string;
  expiresAtMs: number;
  keyAvailable: boolean;
}

/** StoryViewer is one row from GET /v1/stories/{id}/viewers (author-only). */
export interface StoryViewer {
  userId: string;
  viewedAtMs: number;
}

/** StoryContent is the displayable payload. On this dev build it's cached
 *  locally for the author's own stories; cross-device delivery is the (unwired)
 *  STORY_KEY seam, so a viewer without it sees the encrypted placeholder. */
export interface StoryContent {
  kind: "text" | "image";
  text?: string;
  bg?: string; // text-story background color
  dataUrl?: string; // image data URL
}

/** LinkedDevice is one row from GET /v1/devices. */
export interface LinkedDevice {
  id: string;
  isPrimary: boolean;
  platform: string;
  name: string;
  lastActiveMs: number;
}

/** ChannelInfo is the client view of a broadcast channel. */
/** CommunityInfo mirrors GET /v1/communities/{id} (T8.02). */
export interface CommunityInfo {
  id: string;
  name: string;
  description: string;
  kind: string; // public | private
  announcementGroupId: string;
  memberCount: number;
  groupCount: number;
  myRole: string; // owner | admin | member | "" (not a member)
}

/** CommunitySummary is one discover/search row. */
export interface CommunitySummary {
  id: string;
  name: string;
  description: string;
  memberCount: number;
}

/** CommunityMember is one membership row. */
export interface CommunityMember {
  userId: string;
  role: string;
}

/** CommunityEvent is one shared-calendar entry. */
export interface CommunityEvent {
  id: string;
  title: string;
  description: string;
  startsAtMs: number;
  createdBy: string;
}

export interface ChannelInfo {
  id: string;
  handle: string;
  name: string;
  description: string;
  kind: string; // public | private
  verified: boolean;
  followers: number;
  myRole: string; // owner | admin | follower | "" (not a member)
  premium: boolean;
  priceCents: number;
  mySubscribed: boolean;
  createdAtMs: number;
}

/** ChannelInsights is a channel's aggregate analytics (admin-only). */
export interface ChannelInsights {
  followers: number;
  subscribers: number;
  posts: number;
  views: number;
  reactions: number;
  comments: number;
  premium: boolean;
  priceCents: number;
}

/** ChannelPost is one channel feed entry (broadcast — content is server-visible). */
export interface ChannelPost {
  id: string;
  channelId: string;
  body: string;
  mediaRef: string;
  scheduled: boolean;
  publishAtMs: number;
  reactions: Record<string, number>;
  comments: number;
  createdAtMs: number;
}

/** NotificationEntry is one in-app notification (a new inbound message in a
 *  conversation you weren't viewing). Content is the E2EE placeholder in dev. */
export interface NotificationEntry {
  id: string;
  conversationId: string;
  title: string;
  preview: string;
  ts: number;
}

/** GifResult mirrors the IP-hiding GIF proxy (GET /v1/media/gif/search). */
export interface GifResult {
  id: string;
  url: string;
  previewUrl: string;
  width: number;
  height: number;
}

/** A sticker within a pack (object_key is a public, non-E2EE asset). */
export interface StickerItem {
  id: string;
  emoji: string;
  objectKey: string;
}

/** A sticker pack from the catalog (stickers present on the detail fetch). */
export interface StickerPackInfo {
  id: string;
  title: string;
  author: string;
  animated: boolean;
  stickers?: StickerItem[];
}

/** ScheduledMessage is a client-held message queued to send at sendAtMs (T6.04). */
export interface ScheduledMessage {
  id: string;
  conversationId: string;
  text: string;
  sendAtMs: number;
}

/** MessageTemplate is a saved reply the user can insert into the composer. */
export interface MessageTemplate {
  id: string;
  title: string;
  text: string;
}

/** PollResults mirrors GET /v1/polls/{id} — the index-based tally the server
 *  keeps (option TEXTS stay client-side/E2EE). */
export interface PollResults {
  pollId: string;
  closed: boolean;
  optionCount: number;
  multi: boolean;
  totalVoters: number;
  tallies: number[]; // voters per option index
  myVotes: number[]; // the caller's chosen indices
}

/** CallSignalHandler receives the WS call-signaling frames (dev.{id}.call),
 *  forwarded to the CallProvider which drives the CallSession. */
export interface CallSignalHandler {
  onOffer(callerUserId: string, roomId: string, ringId: string, kind: CallKind): void;
  onRing(state: RingState): void;
  onEnd(reason: string): void;
}

export class AppServices {
  readonly otp: OtpClient;
  readonly sessions: SessionManager;
  readonly db: DbApi;

  private readonly http = createHttpClient(config.apiBaseUrl);
  private ws: WsClient | null = null;
  private cursorMirror: ConversationCursor[] = [];
  private readonly changeListeners = new Set<() => void>();
  private readonly authListeners = new Set<(authed: boolean) => void>(); // session gained/lost
  private readonly peerByConv = new Map<string, string>(); // conversationId → peer userId
  private readonly profileCache = new Map<string, PublicProfile>(); // userId → public profile
  private callHandler: CallSignalHandler | null = null; // set by CallProvider
  private readonly channelEventListeners = new Set<(channelId: string, postId: string) => void>(); // T7.04
  private readonly groupCache = new Map<string, GroupInfo>(); // conversationId → group info
  private readonly notGroup = new Set<string>(); // conversationIds confirmed to be direct chats
  // ── notifications (T5.13, client-local) ──
  private readonly unreadByConv = new Map<string, number>(); // conversationId → unread count
  private activeConv: string | null = null; // the conversation currently on screen
  private readonly mutedConvs = new Set<string>(); // per-chat mute (localStorage)
  private globalMuted = false; // global mute (localStorage)
  private readonly notifLog: NotificationEntry[] = []; // recent in-app notifications (newest first)
  private readonly toastListeners = new Set<(n: NotificationEntry) => void>();
  private readonly typingByConv = new Map<string, number>(); // conversationId → typing-expiry ts
  private readonly presenceByUser = new Map<string, { online: boolean; lastSeenMs: number }>();
  // ── chat conveniences (T5.15, client-local) ──
  private readonly favoriteConvs = new Set<string>(); // pinned/favorite chats (localStorage)
  private readonly archivedConvs = new Set<string>(); // archived chats (localStorage)
  private readonly wallpaperByConv = new Map<string, string>(); // conversationId → wallpaper key
  private readonly draftByConv = new Map<string, string>(); // conversationId → composer draft
  // ── secret chats + disappearing (T10.01) ──
  private readonly disappearingTtl = new Map<string, number>(); // conversationId → ttl seconds (0/absent = off)
  private readonly lockedConvs = new Set<string>(); // locked chats (require unlock to open) — client-local
  private readonly hiddenConvs = new Set<string>(); // hidden chats (kept out of the main list) — client-local
  private readonly screenshotProtected = new Set<string>(); // screenshot-protection signalled — client-local
  // ── on-device AI runtime (T11.01) ──
  private aiSettings: AiSettings = { mode: DEFAULT_AI_SETTINGS.mode, consent: { ...DEFAULT_AI_SETTINGS.consent } };
  private aiConfig = { enabled: true, serverEndpointAvailable: false }; // from GET /v1/ai/config
  // undo-send: a just-sent message is held for a short window before dispatch.
  private pendingSend: { conversationId: string; text: string; reply?: QuotedRef; timer: ReturnType<typeof setTimeout> } | null = null;
  // ── scheduling + templates + auto-reply (T6.04, client-local) ──
  private scheduledMsgs: ScheduledMessage[] = []; // wa.scheduled
  private msgTemplates: MessageTemplate[] = []; // wa.templates
  private autoReplyCfg = { enabled: false, text: "" }; // wa.autoreply
  private readonly autoRepliedAt = new Map<string, number>(); // conversationId → last auto-reply (cooldown)
  private scheduleTimer: ReturnType<typeof setInterval> | null = null;

  /** onChange fires whenever the local store changes (inbound messages, send
   *  acks, or a fresh send). Screens re-fetch on it so the open thread and the
   *  chat list update live, with no navigation. Returns an unsubscribe fn. */
  onChange(cb: () => void): () => void {
    this.changeListeners.add(cb);
    return () => this.changeListeners.delete(cb);
  }
  private notifyChange(): void {
    for (const cb of this.changeListeners) cb();
  }

  /** onAuthChange fires when the session is gained (login) or lost (logout /
   *  failed refresh). The UI uses it to route to the chat list or the login
   *  screen — so an expired session bounces to login instead of leaving the user
   *  stuck on cryptic 401s. Returns an unsubscribe. */
  onAuthChange(cb: (authed: boolean) => void): () => void {
    this.authListeners.add(cb);
    return () => this.authListeners.delete(cb);
  }
  private notifyAuth(authed: boolean): void {
    for (const cb of this.authListeners) cb(authed);
  }

  private constructor() {
    this.otp = new OtpClient(createHttpClient(config.apiBaseUrl));
    this.sessions = new SessionManager(indexedDbSecureStore);
    const worker = new Worker(new URL("../worker/db.worker.ts", import.meta.url), { type: "module" });
    this.db = createDbClient(worker);
    this.loadMuteState();
    this.loadChatPrefs();
  }

  private loadMuteState(): void {
    try {
      const convs = localStorage.getItem("wa.mute.convs");
      if (convs) for (const id of JSON.parse(convs) as string[]) this.mutedConvs.add(id);
      this.globalMuted = localStorage.getItem("wa.mute.global") === "1";
    } catch {
      /* no persisted prefs — defaults (unmuted) */
    }
  }

  private loadChatPrefs(): void {
    try {
      const fav = localStorage.getItem("wa.fav.convs");
      if (fav) for (const id of JSON.parse(fav) as string[]) this.favoriteConvs.add(id);
      const arch = localStorage.getItem("wa.archive.convs");
      if (arch) for (const id of JSON.parse(arch) as string[]) this.archivedConvs.add(id);
      const wp = localStorage.getItem("wa.wallpaper");
      if (wp) for (const [id, key] of Object.entries(JSON.parse(wp) as Record<string, string>)) this.wallpaperByConv.set(id, key);
      const dr = localStorage.getItem("wa.drafts");
      if (dr) for (const [id, text] of Object.entries(JSON.parse(dr) as Record<string, string>)) this.draftByConv.set(id, text);
      const sch = localStorage.getItem("wa.scheduled");
      if (sch) this.scheduledMsgs = JSON.parse(sch) as ScheduledMessage[];
      const tpl = localStorage.getItem("wa.templates");
      if (tpl) this.msgTemplates = JSON.parse(tpl) as MessageTemplate[];
      const ar = localStorage.getItem("wa.autoreply");
      if (ar) this.autoReplyCfg = JSON.parse(ar) as { enabled: boolean; text: string };
      const dis = localStorage.getItem("wa.disappearing");
      if (dis) for (const [id, ttl] of Object.entries(JSON.parse(dis) as Record<string, number>)) this.disappearingTtl.set(id, ttl);
      const lk = localStorage.getItem("wa.locked");
      if (lk) for (const id of JSON.parse(lk) as string[]) this.lockedConvs.add(id);
      const hd = localStorage.getItem("wa.hidden");
      if (hd) for (const id of JSON.parse(hd) as string[]) this.hiddenConvs.add(id);
      const ss = localStorage.getItem("wa.ssprotect");
      if (ss) for (const id of JSON.parse(ss) as string[]) this.screenshotProtected.add(id);
      const aiRaw = localStorage.getItem("wa.ai");
      if (aiRaw) {
        const parsed = JSON.parse(aiRaw) as Partial<AiSettings>;
        this.aiSettings = {
          mode: parsed.mode ?? "off",
          consent: { onDevice: parsed.consent?.onDevice ?? false, server: parsed.consent?.server ?? false },
        };
      }
    } catch {
      /* no persisted prefs — defaults */
    }
  }

  private persistSet(key: string, set: Set<string>): void {
    try {
      localStorage.setItem(key, JSON.stringify([...set]));
    } catch {
      /* ignore */
    }
  }
  private persistMap(key: string, map: Map<string, string>): void {
    try {
      localStorage.setItem(key, JSON.stringify(Object.fromEntries(map)));
    } catch {
      /* ignore */
    }
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
    this.notifyAuth(true);
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
          // Inbound items are always from the peer (I never receive my own), so
          // their sender is this conversation's other participant.
          for (const it of b.items) this.peerByConv.set(it.conversationId, it.senderUserId);
          const watermark = await this.db.persistInboxBatch(b);
          this.mergeCursors(watermark);
          // Raise unread + an in-app notification for real messages (not overlay
          // edits/reactions/receipts) in conversations I'm not currently viewing.
          for (const it of b.items) {
            if (it.overlayTarget || it.conversationId === this.activeConv) continue;
            this.unreadByConv.set(it.conversationId, (this.unreadByConv.get(it.conversationId) ?? 0) + 1);
            const entry: NotificationEntry = {
              id: it.msgUuid,
              conversationId: it.conversationId,
              title: this.groupNameOf(it.conversationId) || this.nameForUser(it.senderUserId),
              preview: "New message", // content is E2EE (decrypt seam) — no plaintext preview in dev
              ts: it.acceptedAtMs || Date.now(),
            };
            this.notifLog.unshift(entry);
            if (this.notifLog.length > 50) this.notifLog.pop();
            if (!this.globalMuted && !this.mutedConvs.has(it.conversationId)) {
              for (const cb of this.toastListeners) cb(entry);
            }
          }
          this.maybeAutoReply(b.items); // T6.04 away auto-responder
          this.notifyChange(); // new inbound message(s) → refresh open screens
          // Tell each sender their message reached this device (drives ✓✓).
          const maxByConv = new Map<string, number>();
          for (const it of b.items) {
            maxByConv.set(it.conversationId, Math.max(maxByConv.get(it.conversationId) ?? 0, it.seq));
          }
          for (const [conversationId, upToSeq] of maxByConv) {
            this.ws?.send({ t: "receipt", conversationId, kind: "DELIVERED", upToSeq });
          }
          return watermark;
        },
        onMsgAck: (a) => {
          void this.db.markSent({ clientRef: a.clientRef, seq: a.seq }).then(() => this.notifyChange());
        },
        onReceipt: (r) => {
          void this.db
            .markReceipt({ conversationId: r.conversationId, kind: r.kind, upToSeq: r.upToSeq })
            .then(() => this.notifyChange());
        },
        onTyping: (t) => {
          if (t.recording) {
            this.typingByConv.set(t.conversationId, Date.now() + 6000);
            setTimeout(() => this.notifyChange(), 6100); // hide the indicator once it lapses
          } else {
            this.typingByConv.delete(t.conversationId);
          }
          this.notifyChange();
        },
        onPresence: (p) => {
          this.presenceByUser.set(p.userId, { online: p.online, lastSeenMs: p.lastSeenMs });
          this.notifyChange();
        },
        onCallOffer: (o) => this.callHandler?.onOffer(o.callerUserId, o.roomId, o.ringId, o.kind),
        onCallRing: (r) => this.callHandler?.onRing(r.state),
        onCallEnd: (e) => this.callHandler?.onEnd(e.reason),
        onChannelEvent: (e) => {
          for (const cb of this.channelEventListeners) cb(e.channelId, e.postId);
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
    this.startScheduleTicker(); // fire due scheduled messages while connected (T6.04)
  }

  async sendText(conversationId: string, text: string, reply?: QuotedRef): Promise<void> {
    // Generate the link preview on THIS (sender) device and carry it in the
    // sealed body (FR-MSG-08). Best-effort: a URL-free message skips the fetch,
    // and any failure just sends plain text. A reply carries a quoted ref too.
    const preview = await generateLinkPreview(text, webHtmlFetcher).catch(() => null);
    const body = encodeTextMessage(text, preview ?? undefined, reply);
    await this.db.enqueueText({ conversationId, text: body, listText: text, clientRef: newId(), now: Date.now() });
    this.notifyChange(); // optimistic: show my message immediately
    await this.ws?.flush();
  }

  /** sendMedia compresses (no-op today), encrypts, and resumably uploads a file to
   *  media-svc, then sends a media message whose sealed body carries the E2EE
   *  envelope (the file key never leaves the client). Render already exists. */
  async sendMedia(conversationId: string, file: File, caption?: string): Promise<void> {
    const bytes = new Uint8Array(await file.arrayBuffer());
    const envelope = await this.mediaPipeline().prepare({ bytes, mime: file.type || "application/octet-stream" });
    const body = encodeMediaMessage([envelope], caption);
    await this.db.enqueueText({ conversationId, text: body, listText: `📎 ${file.name}`, clientRef: newId(), now: Date.now() });
    this.notifyChange();
    await this.ws?.flush();
  }

  // ── location + contact sharing (T6.03) ────────────────────────────────────

  private readonly liveShares = new Map<string, ReturnType<typeof setInterval>>();

  /** sendLocation shares a one-off place as an E2EE message. */
  async sendLocation(conversationId: string, lat: number, lng: number, label?: string): Promise<void> {
    const body = encodeLocation(lat, lng, label);
    await this.db.enqueueText({ conversationId, text: body, listText: "📍 Location", clientRef: newId(), now: Date.now() });
    this.notifyChange();
    await this.ws?.flush();
  }

  /** sendContactCard shares a contact (name + optional phone/userId) as a card. */
  async sendContactCard(conversationId: string, name: string, phone: string, userId?: string): Promise<void> {
    const body = encodeContactCard(name, phone, userId);
    await this.db.enqueueText({ conversationId, text: body, listText: `👤 ${name}`, clientRef: newId(), now: Date.now() });
    this.notifyChange();
    await this.ws?.flush();
  }

  /** startLiveLocation begins a time-boxed live share: it sends one sample now,
   *  then every 15s until `durationMs` elapses, each riding the ordinary E2EE
   *  message relay (recipients render the latest sample per share as one live
   *  pin). `position` yields the current coordinates. Returns the share id. */
  startLiveLocation(conversationId: string, durationMs: number, position: () => Promise<{ lat: number; lng: number }>): string {
    const shareId = newId();
    const untilMs = Date.now() + durationMs;
    let seq = 0;
    const send = async (): Promise<void> => {
      if (Date.now() > untilMs) {
        this.stopLiveLocation(shareId);
        return;
      }
      try {
        const { lat, lng } = await position();
        const body = encodeLiveLocation(shareId, lat, lng, untilMs, seq++);
        await this.db.enqueueText({ conversationId, text: body, listText: "📍 Live location", clientRef: newId(), now: Date.now() });
        this.notifyChange();
        await this.ws?.flush();
      } catch {
        /* a dropped sample is fine — the next tick retries */
      }
    };
    void send();
    this.liveShares.set(shareId, setInterval(() => void send(), 15_000));
    return shareId;
  }

  /** stopLiveLocation ends a live share early (also fires at expiry). */
  stopLiveLocation(shareId: string): void {
    const t = this.liveShares.get(shareId);
    if (t) {
      clearInterval(t);
      this.liveShares.delete(shareId);
    }
  }

  /** isLiveSharing reports whether a live share is still ticking on this device. */
  isLiveSharing(shareId: string): boolean {
    return this.liveShares.has(shareId);
  }

  // ── scheduled messages + templates + auto-reply (T6.04) ────────────────────

  private startScheduleTicker(): void {
    if (this.scheduleTimer) return;
    this.scheduleTimer = setInterval(() => void this.fireDueScheduled(), 15_000);
    void this.fireDueScheduled(); // catch any already due at connect
  }

  private async fireDueScheduled(): Promise<void> {
    const now = Date.now();
    const due = this.scheduledMsgs.filter((m) => m.sendAtMs <= now);
    if (due.length === 0) return;
    this.scheduledMsgs = this.scheduledMsgs.filter((m) => m.sendAtMs > now);
    this.persistScheduled();
    // Enqueue via the normal send path — if offline, it sits in the durable
    // outbox and flushes on reconnect (the client-held "offline" fallback; a
    // server scheduler for a fully-closed app is a documented seam).
    for (const m of due) await this.sendText(m.conversationId, m.text).catch(() => {});
    this.notifyChange();
  }

  /** scheduleMessage queues a message to send at sendAtMs (client-held). */
  scheduleMessage(conversationId: string, text: string, sendAtMs: number): void {
    this.scheduledMsgs.push({ id: newId(), conversationId, text, sendAtMs });
    this.persistScheduled();
    this.notifyChange();
  }

  /** scheduledMessages lists pending scheduled messages (optionally per chat). */
  scheduledMessages(conversationId?: string): ScheduledMessage[] {
    return this.scheduledMsgs
      .filter((m) => conversationId === undefined || m.conversationId === conversationId)
      .sort((a, b) => a.sendAtMs - b.sendAtMs);
  }

  cancelScheduled(id: string): void {
    this.scheduledMsgs = this.scheduledMsgs.filter((m) => m.id !== id);
    this.persistScheduled();
    this.notifyChange();
  }

  /** listTemplates returns the saved-reply templates. */
  listTemplates(): MessageTemplate[] {
    return [...this.msgTemplates];
  }
  addTemplate(title: string, text: string): void {
    this.msgTemplates.push({ id: newId(), title: title.trim(), text });
    this.persistTemplates();
    this.notifyChange();
  }
  removeTemplate(id: string): void {
    this.msgTemplates = this.msgTemplates.filter((t) => t.id !== id);
    this.persistTemplates();
    this.notifyChange();
  }

  /** getAutoReply / setAutoReply drive the away auto-responder. */
  getAutoReply(): { enabled: boolean; text: string } {
    return { ...this.autoReplyCfg };
  }
  setAutoReply(enabled: boolean, text: string): void {
    this.autoReplyCfg = { enabled, text };
    try {
      localStorage.setItem("wa.autoreply", JSON.stringify(this.autoReplyCfg));
    } catch {
      /* ignore */
    }
    this.notifyChange();
  }

  // maybeAutoReply sends the away message once per conversation per hour, only
  // for real inbound (not overlays) in a conversation I'm not viewing. The
  // per-conversation cooldown keeps two away-repliers from ping-ponging tightly.
  private maybeAutoReply(items: { conversationId: string; overlayTarget?: string }[]): void {
    if (!this.autoReplyCfg.enabled || !this.autoReplyCfg.text) return;
    const cooldownMs = 60 * 60 * 1000;
    const now = Date.now();
    const handled = new Set<string>();
    for (const it of items) {
      if (it.overlayTarget || it.conversationId === this.activeConv || handled.has(it.conversationId)) continue;
      if (now - (this.autoRepliedAt.get(it.conversationId) ?? 0) < cooldownMs) continue;
      handled.add(it.conversationId);
      this.autoRepliedAt.set(it.conversationId, now);
      void this.sendText(it.conversationId, this.autoReplyCfg.text).catch(() => {});
    }
  }

  private persistScheduled(): void {
    try {
      localStorage.setItem("wa.scheduled", JSON.stringify(this.scheduledMsgs));
    } catch {
      /* ignore */
    }
  }
  private persistTemplates(): void {
    try {
      localStorage.setItem("wa.templates", JSON.stringify(this.msgTemplates));
    } catch {
      /* ignore */
    }
  }

  // ── polls (T6.02) ─────────────────────────────────────────────────────────

  /** createPoll registers the poll's lifecycle server-side (option count + multi
   *  only — E2EE keeps the question/options off the server), then sends the
   *  sealed poll message carrying poll_id + the question and options. */
  async createPoll(conversationId: string, question: string, options: string[], multi: boolean): Promise<void> {
    const res = (await this.authedJson("/v1/polls", {
      conversation_id: conversationId,
      option_count: options.length,
      multi,
    })) as { poll_id: string };
    const body = encodePoll(res.poll_id, question, options, multi);
    await this.db.enqueueText({ conversationId, text: body, listText: `📊 ${question}`, clientRef: newId(), now: Date.now() });
    this.notifyChange();
    await this.ws?.flush();
  }

  /** votePoll records my chosen option indices and returns the fresh tally. */
  async votePoll(pollId: string, indices: number[]): Promise<PollResults> {
    await this.authedJson(`/v1/polls/${encodeURIComponent(pollId)}/vote`, { option_indices: indices });
    return this.pollResults(pollId);
  }

  /** closePoll ends voting (creator-only, enforced server-side). */
  async closePoll(pollId: string): Promise<void> {
    const res = await this.authedRequest("POST", `/v1/polls/${encodeURIComponent(pollId)}/close`);
    if (!res.ok) throw new Error("Couldn't close the poll.");
  }

  /** pollResults fetches the current tally + my selection. */
  async pollResults(pollId: string): Promise<PollResults> {
    const res = await this.authedRequest("GET", `/v1/polls/${encodeURIComponent(pollId)}`);
    if (!res.ok) throw new Error("Couldn't load the poll.");
    const b = (await res.json()) as {
      poll_id: string;
      closed: boolean;
      option_count: number;
      multi: boolean;
      total_voters: number;
      tallies: number[];
      my_votes: number[];
    };
    return {
      pollId: b.poll_id,
      closed: b.closed,
      optionCount: b.option_count,
      multi: b.multi,
      totalVoters: b.total_voters,
      tallies: b.tallies ?? [],
      myVotes: b.my_votes ?? [],
    };
  }

  // ── rich composer: GIF proxy + stickers (T6.01) ───────────────────────────

  private async mediaGet(path: string): Promise<Response> {
    const go = (): Promise<Response> =>
      fetch(`${config.mediaBaseUrl}${path}`, { headers: { authorization: `Bearer ${this.sessions.current()?.accessJwt ?? ""}` } });
    let res = await go();
    if (res.status === 401) {
      await this.refreshToken();
      res = await go();
    }
    return res;
  }

  /** searchGifs queries the server-side IP-hiding GIF proxy (Tenor). Returns []
   *  when the feature is disabled (no server Tenor key) or on any error, so the
   *  picker degrades gracefully. */
  async searchGifs(query: string): Promise<GifResult[]> {
    try {
      const res = await this.mediaGet(`/v1/media/gif/search?q=${encodeURIComponent(query)}&limit=24`);
      if (!res.ok) return [];
      const body = (await res.json()) as { results?: { id: string; url: string; preview_url: string; width: number; height: number }[] };
      return (body.results ?? []).map((r) => ({ id: r.id, url: r.url, previewUrl: r.preview_url, width: r.width, height: r.height }));
    } catch {
      return [];
    }
  }

  /** sendGif fetches the chosen GIF and sends it as an ordinary E2EE media
   *  message (encrypted + uploaded via the pipeline — the file key never leaves
   *  the client), so the SFU/CDN never sees who received it. */
  async sendGif(conversationId: string, gif: GifResult): Promise<void> {
    const resp = await fetch(gif.url);
    if (!resp.ok) throw new Error("Couldn't fetch the GIF.");
    const file = new File([await resp.blob()], "gif.gif", { type: "image/gif" });
    await this.sendMedia(conversationId, file);
  }

  /** stickerPacks lists the sticker catalog (T6.01). */
  async stickerPacks(): Promise<StickerPackInfo[]> {
    const res = await this.mediaGet("/v1/media/stickers/packs");
    if (!res.ok) return [];
    const body = (await res.json()) as { packs?: StickerPackInfo[] };
    return body.packs ?? [];
  }

  /** stickerPack fetches one pack's stickers (T6.01). */
  async stickerPack(id: string): Promise<StickerPackInfo | null> {
    const res = await this.mediaGet(`/v1/media/stickers/packs/${encodeURIComponent(id)}`);
    if (!res.ok) return null;
    return (await res.json()) as StickerPackInfo;
  }

  /** sendSticker sends a sticker message (public pack asset key + emoji). The
   *  recipient renders the image from the key, or the emoji glyph as a fallback. */
  async sendSticker(conversationId: string, sticker: StickerItem): Promise<void> {
    const body = encodeSticker(sticker.objectKey, sticker.emoji);
    await this.db.enqueueText({ conversationId, text: body, listText: `${sticker.emoji} Sticker`, clientRef: newId(), now: Date.now() });
    this.notifyChange();
    await this.ws?.flush();
  }

  private media: MediaPipeline | null = null;
  private mediaPipeline(): MediaPipeline {
    if (!this.media) {
      const token = (): string => this.sessions.current()?.accessJwt ?? "";
      const refresh = (): Promise<void> => this.refreshToken();
      // No compressor/thumbnailer yet — upload the source bytes as-is (the ports
      // are optional; codec + blurhash are a later refinement).
      this.media = new MediaPipeline(new ResumableUploader(webUploadTransport(config.mediaBaseUrl, token, refresh)));
    }
    return this.media;
  }

  /** editMessage sends an OVERLAY_EDIT (server enforces the 15-min window); the
   *  recipient's bubble rewrites to the new text. */
  async editMessage(conversationId: string, msgUuid: string, newText: string): Promise<void> {
    const body = encodeTextMessage(newText);
    await this.db.enqueueOverlay({ conversationId, targetMsgUuid: msgUuid, kind: "edit", text: body, clientRef: newId(), now: Date.now() });
    this.notifyChange();
    await this.ws?.flush();
  }

  /** deleteForEveryone sends an OVERLAY_DELETE (server enforces the 48-h window);
   *  the recipient's bubble becomes "This message was deleted". */
  async deleteForEveryone(conversationId: string, msgUuid: string): Promise<void> {
    await this.db.enqueueOverlay({ conversationId, targetMsgUuid: msgUuid, kind: "delete", text: "", clientRef: newId(), now: Date.now() });
    this.notifyChange();
    await this.ws?.flush();
  }

  /** react toggles my emoji reaction on a message via a REACTION overlay (T5.05b).
   *  The caller passes op (add/remove) from the emoji's current mine-state; both
   *  ends fold it into the target's tally. */
  async react(conversationId: string, msgUuid: string, emoji: string, op: "add" | "remove"): Promise<void> {
    const body = encodeReaction(emoji, op);
    await this.db.enqueueOverlay({ conversationId, targetMsgUuid: msgUuid, kind: "react", text: body, clientRef: newId(), now: Date.now() });
    this.notifyChange();
    await this.ws?.flush();
  }

  /** forwardMessage re-sends a message's content to another conversation (T5.05b).
   *  Text (with any link preview) and media (the same uploaded object, re-sealed
   *  under the target conversation's key) are supported; the reply quote is dropped. */
  async forwardMessage(sourceConversationId: string, msgUuid: string, targetConversationId: string): Promise<void> {
    const src = (await this.db.thread(sourceConversationId)).find((m) => m.msgUuid === msgUuid);
    if (!src || src.deleted) return;
    const media = parseMediaMessage(src.body);
    if (media) {
      const body = encodeMediaMessage(media.attachments, media.caption);
      await this.db.enqueueText({ conversationId: targetConversationId, text: body, listText: media.caption || "📎 Media", clientRef: newId(), now: Date.now() });
    } else {
      const text = parseTextMessage(src.body).text;
      const body = encodeTextMessage(text);
      await this.db.enqueueText({ conversationId: targetConversationId, text: body, listText: text, clientRef: newId(), now: Date.now() });
    }
    this.notifyChange();
    await this.ws?.flush();
  }

  /** deleteForMe hides the message locally only — no overlay leaves the device. */
  async deleteForMe(msgUuid: string): Promise<void> {
    await this.db.deleteForMe({ msgUuid });
    this.notifyChange();
  }

  // ── Secret chats + disappearing messages (T10.01) ──────────────────────────
  // The per-chat timer is authoritative client-side; the server keeps a coarse
  // copy (shared between members + drives its purge backstop). Locked/hidden/
  // screenshot-protection are client-local signals.

  /** disappearingSeconds is the chat's disappearing timer (0 = off). */
  disappearingSeconds(conversationId: string): number {
    return this.disappearingTtl.get(conversationId) ?? 0;
  }

  /** loadDisappearing pulls the server's timer copy (members share it) into the
   *  local map — call on opening a chat. */
  async loadDisappearing(conversationId: string): Promise<void> {
    try {
      const res = await this.authedRequest("GET", `/v1/conversations/${encodeURIComponent(conversationId)}/disappearing`);
      if (!res.ok) return;
      const { ttl_seconds } = (await res.json()) as { ttl_seconds: number };
      if (ttl_seconds > 0) this.disappearingTtl.set(conversationId, ttl_seconds);
      else this.disappearingTtl.delete(conversationId);
      this.persistDisappearing();
      this.notifyChange();
    } catch {
      /* offline — keep the local copy */
    }
  }

  /** setDisappearing sets the chat's timer (0 = off): persists the server copy
   *  (shared with members + purge backstop) and applies it locally. */
  async setDisappearing(conversationId: string, seconds: number): Promise<void> {
    const res = await this.authedRequest("PUT", `/v1/conversations/${encodeURIComponent(conversationId)}/disappearing`, { ttl_seconds: seconds });
    if (!res.ok) throw new Error(`set disappearing failed: HTTP ${res.status}`);
    if (seconds > 0) this.disappearingTtl.set(conversationId, seconds);
    else this.disappearingTtl.delete(conversationId);
    this.persistDisappearing();
    this.notifyChange();
  }

  private persistDisappearing(): void {
    this.persistMap("wa.disappearing", new Map([...this.disappearingTtl].map(([k, v]) => [k, String(v)])));
  }

  isLocked(conversationId: string): boolean {
    return this.lockedConvs.has(conversationId);
  }
  toggleLock(conversationId: string): void {
    this.toggleLocal(this.lockedConvs, "wa.locked", conversationId);
  }
  isHidden(conversationId: string): boolean {
    return this.hiddenConvs.has(conversationId);
  }
  toggleHidden(conversationId: string): void {
    this.toggleLocal(this.hiddenConvs, "wa.hidden", conversationId);
  }
  isScreenshotProtected(conversationId: string): boolean {
    return this.screenshotProtected.has(conversationId);
  }
  toggleScreenshotProtection(conversationId: string): void {
    this.toggleLocal(this.screenshotProtected, "wa.ssprotect", conversationId);
  }

  private toggleLocal(set: Set<string>, key: string, id: string): void {
    if (set.has(id)) set.delete(id);
    else set.add(id);
    this.persistSet(key, set);
    this.notifyChange();
  }

  // ── Device auth hardening: passkeys + login audit (T10.02) ──────────────────
  // WebAuthn passkeys are a 2FA / step-up factor and back the biometric app/chat
  // lock (platform authenticator = Touch ID / Windows Hello / Android biometric).
  // The server verifies assertions; secret key material never leaves the device.

  passkeysSupported(): boolean {
    return typeof window !== "undefined" && typeof window.PublicKeyCredential !== "undefined";
  }

  /** registerPasskey runs the WebAuthn create ceremony and enrolls the credential. */
  async registerPasskey(): Promise<void> {
    const begin = await this.authedRequest("POST", "/v1/auth/passkeys/register/begin");
    if (!begin.ok) throw new Error(`passkey enrol failed: HTTP ${begin.status}`);
    const opts = (await begin.json()) as { challenge: string; rp_id: string; user_id: string; algs: number[] };
    const cred = (await navigator.credentials.create({
      publicKey: {
        challenge: b64urlToBytes(opts.challenge),
        rp: { id: opts.rp_id, name: "WhatsApp V2" },
        user: { id: new TextEncoder().encode(opts.user_id), name: opts.user_id, displayName: "You" },
        pubKeyCredParams: opts.algs.map((alg) => ({ type: "public-key", alg })),
        authenticatorSelection: { userVerification: "preferred", residentKey: "preferred" },
        timeout: 60_000,
      },
    })) as PublicKeyCredential | null;
    if (!cred) throw new Error("passkey creation was cancelled");
    const resp = cred.response as AuthenticatorAttestationResponse;
    const alg = resp.getPublicKeyAlgorithm();
    const spki = resp.getPublicKey();
    if (!spki) throw new Error("this authenticator did not return a public key");
    const publicKey = await this.rawPublicKey(alg, new Uint8Array(spki));
    const res = await this.authedRequest("POST", "/v1/auth/passkeys/register/finish", {
      credential_id: bytesToB64url(new Uint8Array(cred.rawId)),
      alg,
      public_key: bytesToB64url(publicKey),
      client_data_json: bytesToB64url(new Uint8Array(resp.clientDataJSON)),
      name: this.deviceLabel(),
    });
    if (!res.ok) throw new Error(`passkey enrol failed: HTTP ${res.status}`);
    this.notifyChange();
  }

  /** rawPublicKey turns the authenticator's SPKI DER into the raw key the server
   *  stores: EdDSA = the trailing 32 bytes; ES256 = x||y (drop the 0x04 tag). */
  private async rawPublicKey(alg: number, spki: Uint8Array): Promise<Uint8Array> {
    if (alg === -8) return spki.slice(spki.length - 32);
    const key = await crypto.subtle.importKey("spki", spki, { name: "ECDSA", namedCurve: "P-256" }, true, []);
    const raw = new Uint8Array(await crypto.subtle.exportKey("raw", key));
    return raw.slice(1, 65);
  }

  /** loginPasskey runs the WebAuthn get ceremony (biometric prompt) and verifies
   *  the assertion server-side. Returns true on success — used for the app/chat
   *  lock and as a 2FA step-up. */
  async loginPasskey(): Promise<boolean> {
    const begin = await this.authedRequest("POST", "/v1/auth/passkeys/login/begin");
    if (!begin.ok) return false;
    const opts = (await begin.json()) as { challenge: string; rp_id: string; allow_credentials: string[] };
    if (opts.allow_credentials.length === 0) return false;
    const assertion = (await navigator.credentials.get({
      publicKey: {
        challenge: b64urlToBytes(opts.challenge),
        rpId: opts.rp_id,
        allowCredentials: opts.allow_credentials.map((id) => ({ type: "public-key" as const, id: b64urlToBytes(id) })),
        userVerification: "preferred",
        timeout: 60_000,
      },
    })) as PublicKeyCredential | null;
    if (!assertion) return false;
    const resp = assertion.response as AuthenticatorAssertionResponse;
    const res = await this.authedRequest("POST", "/v1/auth/passkeys/login/finish", {
      credential_id: bytesToB64url(new Uint8Array(assertion.rawId)),
      authenticator_data: bytesToB64url(new Uint8Array(resp.authenticatorData)),
      client_data_json: bytesToB64url(new Uint8Array(resp.clientDataJSON)),
      signature: bytesToB64url(new Uint8Array(resp.signature)),
    });
    return res.ok;
  }

  async listPasskeys(): Promise<PasskeyInfo[]> {
    const res = await this.authedRequest("GET", "/v1/auth/passkeys");
    if (!res.ok) return [];
    return ((await res.json()) as { passkeys: PasskeyInfo[] }).passkeys ?? [];
  }

  async deletePasskey(id: string): Promise<void> {
    await this.authedRequest("DELETE", `/v1/auth/passkeys/${encodeURIComponent(id)}`);
    this.notifyChange();
  }

  async recentLogins(): Promise<LoginInfo[]> {
    const res = await this.authedRequest("GET", "/v1/auth/logins");
    if (!res.ok) return [];
    return ((await res.json()) as { logins: LoginInfo[] }).logins ?? [];
  }

  private deviceLabel(): string {
    const ua = typeof navigator !== "undefined" ? navigator.userAgent : "";
    if (/Mac/.test(ua)) return "Mac passkey";
    if (/Windows/.test(ua)) return "Windows passkey";
    if (/iPhone|iPad/.test(ua)) return "iOS passkey";
    if (/Android/.test(ua)) return "Android passkey";
    return "Passkey";
  }

  async togglePin(msgUuid: string, pinned: boolean): Promise<void> {
    await this.db.setPinned({ msgUuid, pinned });
    this.notifyChange();
  }

  async toggleStar(msgUuid: string, starred: boolean): Promise<void> {
    await this.db.setStarred({ msgUuid, starred });
    this.notifyChange();
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
    return this.openDirectWithUser(peer);
  }

  /** openDirectWithUser gets (or creates) the shared 1:1 conversation with a known
   *  user id and returns its id — used from search/favorites where the id is known. */
  async openDirectWithUser(userId: string): Promise<string> {
    const conv = (await this.authedJson("/v1/conversations/direct", { peer_user_id: userId })) as {
      conversation_id?: string;
    };
    if (!conv.conversation_id) throw new Error("Couldn't start the conversation.");
    this.peerByConv.set(conv.conversation_id, userId); // for presence/typing subscription
    return conv.conversation_id;
  }

  /** peerOf returns the 1:1 conversation's other participant user id, if known. */
  peerOf(conversationId: string): string | undefined {
    return this.peerByConv.get(conversationId);
  }

  // ── profile & privacy (T5.07) ──────────────────────────────────────────────

  private async authedRequest(method: string, path: string, body?: unknown): Promise<Response> {
    const headers = (): Record<string, string> => ({
      authorization: `Bearer ${this.sessions.current()?.accessJwt ?? ""}`,
      ...(body === undefined ? {} : { "content-type": "application/json" }),
    });
    const go = (): Promise<Response> =>
      fetch(`${config.apiBaseUrl}${path}`, {
        method,
        headers: headers(),
        body: body === undefined ? undefined : JSON.stringify(body),
      });
    let res = await go();
    if (res.status === 401) {
      await this.refreshToken();
      res = await go();
    }
    return res;
  }

  /** getMyProfile loads my editable profile (username/display name/about) plus
   *  the per-field privacy settings. */
  async getMyProfile(): Promise<MyProfile> {
    const res = await this.authedRequest("GET", "/v1/me");
    if (!res.ok) throw new Error("Couldn't load your profile.");
    const p = (await res.json()) as {
      username?: string;
      display_name?: string;
      about?: string;
      privacy?: Record<string, string>;
    };
    return {
      username: p.username ?? "",
      displayName: p.display_name ?? "",
      about: p.about ?? "",
      privacy: p.privacy ?? {},
    };
  }

  /** saveMyPrivacy persists the per-field visibility map (last_seen/avatar/
   *  about/read_receipts → everyone|contacts|nobody). */
  async saveMyPrivacy(privacy: Record<string, string>): Promise<void> {
    const res = await this.authedRequest("PUT", "/v1/me/privacy", { privacy });
    if (!res.ok) throw new Error("Couldn't save your privacy settings.");
  }

  /** updateMyProfile saves display name / username / about. Throws a friendly
   *  message when the username is taken. */
  async updateMyProfile(fields: PublicProfile): Promise<void> {
    const res = await this.authedRequest("PUT", "/v1/me", {
      display_name: fields.displayName,
      username: fields.username,
      about: fields.about,
    });
    if (res.status === 409) throw new Error("That username is already taken.");
    if (!res.ok) throw new Error("Couldn't save your profile — check the fields and try again.");
  }

  /** callHistory loads recent call records (metadata only, 90-day window). */
  async callHistory(limit = 50): Promise<CallHistoryItem[]> {
    const res = await this.authedRequest("GET", `/v1/calls/history?limit=${limit}`);
    if (!res.ok) throw new Error("Couldn't load call history.");
    const body = (await res.json()) as {
      calls?: Array<{
        id: string;
        room_id: string;
        kind: number;
        initiator: string;
        participants?: string[];
        started_at?: string;
        ended_at?: string;
        outcome: string;
      }>;
    };
    return (body.calls ?? []).map((c) => ({
      id: c.id,
      roomId: c.room_id,
      kind: c.kind,
      initiator: c.initiator,
      participants: c.participants ?? [],
      startedAt: c.started_at,
      endedAt: c.ended_at,
      outcome: c.outcome,
    }));
  }

  /** loadUserProfile fetches (and caches) a user's public profile; re-renders
   *  screens once it lands so peer names replace ids. */
  async loadUserProfile(userId: string): Promise<void> {
    if (this.profileCache.has(userId)) return;
    try {
      const res = await this.authedRequest("GET", `/v1/users/${userId}`);
      if (!res.ok) return;
      const p = (await res.json()) as { username?: string; display_name?: string; about?: string };
      this.profileCache.set(userId, { username: p.username ?? "", displayName: p.display_name ?? "", about: p.about ?? "" });
      this.notifyChange();
    } catch {
      /* leave uncached; name falls back to the id */
    }
  }

  /** peerNameOf returns a cached, human name for a conversation's peer (display
   *  name, else @username), or "" if not yet loaded. */
  peerNameOf(conversationId: string): string {
    const peer = this.peerByConv.get(conversationId);
    const p = peer ? this.profileCache.get(peer) : undefined;
    if (!p) return "";
    return p.displayName || (p.username ? `@${p.username}` : "");
  }

  async blockUser(userId: string): Promise<void> {
    const res = await this.authedRequest("POST", "/v1/blocks", { user_id: userId });
    if (!res.ok) throw new Error("Couldn't block that user.");
  }
  async unblockUser(userId: string): Promise<void> {
    await this.authedRequest("DELETE", `/v1/blocks/${userId}`);
  }
  async getBlocked(): Promise<string[]> {
    const res = await this.authedRequest("GET", "/v1/blocks");
    if (!res.ok) return [];
    const b = (await res.json()) as { blocked?: string[] };
    return b.blocked ?? [];
  }

  /** reportUser files a trust-and-safety report against a user into the admin
   *  queue (T10.03). reason: 0 spam · 1 harassment · 2 scam · 3 impersonation ·
   *  4 other. The message stays E2EE — nothing is disclosed unless the caller
   *  opts to attach ciphertext (not wired here). */
  async reportUser(targetUserId: string, reason: number, note?: string): Promise<void> {
    const res = await this.authedRequest("POST", "/v1/reports", { target_user_id: targetUserId, reason, note: note ?? "" });
    if (!res.ok) throw new Error(`Couldn't file the report (HTTP ${res.status}).`);
  }

  // ── On-device AI runtime (T11.01) ───────────────────────────────────────────
  // AI is OFF by default. On-device mode keeps everything local (E2EE-safe); an
  // opt-in server mode would disclose the specific content it processes. An
  // operator kill-switch (GET /v1/ai/config) can disable all of it.

  /** loadAiConfig pulls the operator kill-switch + endpoint availability. */
  async loadAiConfig(): Promise<void> {
    try {
      const res = await this.authedRequest("GET", "/v1/ai/config");
      if (!res.ok) return;
      const c = (await res.json()) as { enabled: boolean; server_endpoint_available: boolean };
      this.aiConfig = { enabled: c.enabled, serverEndpointAvailable: c.server_endpoint_available };
      this.notifyChange();
    } catch {
      /* offline — keep the cached config (default enabled, on-device only) */
    }
  }

  aiKillSwitchOn(): boolean {
    return this.aiConfig.enabled;
  }
  aiServerAvailable(): boolean {
    return this.aiConfig.serverEndpointAvailable;
  }
  getAiSettings(): AiSettings {
    return { mode: this.aiSettings.mode, consent: { ...this.aiSettings.consent } };
  }

  /** setAiMode switches AI mode (off / on-device / server). */
  setAiMode(mode: AiMode): void {
    this.aiSettings = { ...this.aiSettings, mode };
    this.persistAi();
  }
  /** setAiConsent records the user's consent for a mode (server mode's consent is
   *  the disclosure acknowledgement that content leaves the device). */
  setAiConsent(kind: "onDevice" | "server", ok: boolean): void {
    this.aiSettings = { ...this.aiSettings, consent: { ...this.aiSettings.consent, [kind]: ok } };
    this.persistAi();
  }
  private persistAi(): void {
    try {
      localStorage.setItem("wa.ai", JSON.stringify(this.aiSettings));
    } catch {
      /* ignore */
    }
    this.notifyChange();
  }

  /** aiRuntime builds the gated runtime T11.02/T11.03 execute tasks through. The
   *  on-device model provider is injected later (a documented seam) — until then
   *  the runtime gates correctly and reports "no-provider". */
  aiRuntime(): AiRuntime {
    return new AiRuntime({
      killSwitchOn: () => this.aiConfig.enabled,
      serverEndpointAvailable: () => this.aiConfig.serverEndpointAvailable,
      settings: () => this.getAiSettings(),
      // onDevice / server providers wire in with T11.02 (real models/endpoint).
    });
  }
  /** nameForUser returns a cached human name for any user id, falling back to a
   *  short id when the profile hasn't been loaded yet. */
  nameForUser(userId: string): string {
    const p = this.profileCache.get(userId);
    if (p) return p.displayName || (p.username ? `@${p.username}` : userId.slice(0, 8));
    return userId.slice(0, 8);
  }

  // ── contacts (T5.08) ────────────────────────────────────────────────────────

  /** searchContacts finds registered users by username (server-side, rate-limited,
   *  ≥2 chars, caller excluded). Returns [] for short/blank queries. */
  async searchContacts(query: string): Promise<UserRef[]> {
    const q = query.trim();
    if (q.length < 2) return [];
    const res = await this.authedRequest("GET", `/v1/contacts/search?u=${encodeURIComponent(q)}`);
    if (!res.ok) return [];
    const b = (await res.json()) as { results?: Array<{ user_id: string; username: string }> };
    return (b.results ?? []).map((r) => ({ userId: r.user_id, username: r.username }));
  }

  /** listFavorites returns the caller's starred contacts. */
  async listFavorites(): Promise<UserRef[]> {
    const res = await this.authedRequest("GET", "/v1/contacts/favorites");
    if (!res.ok) return [];
    const b = (await res.json()) as { favorites?: Array<{ user_id: string; username: string }> };
    return (b.favorites ?? []).map((r) => ({ userId: r.user_id, username: r.username }));
  }

  async addFavorite(userId: string): Promise<void> {
    const res = await this.authedRequest("PUT", `/v1/contacts/favorites/${userId}`);
    if (!res.ok) throw new Error("Couldn't add that favorite.");
  }
  async removeFavorite(userId: string): Promise<void> {
    await this.authedRequest("DELETE", `/v1/contacts/favorites/${userId}`);
  }

  /** syncPhones checks which of the given phone numbers are registered, returning
   *  the matched contacts (server peppers+hashes; only hashes are persisted). */
  async syncPhones(handles: string[]): Promise<MatchedContact[]> {
    const clean = handles.map((h) => h.trim()).filter(Boolean);
    if (clean.length === 0) return [];
    const res = await this.authedRequest("POST", "/v1/contacts/sync", { handles: clean });
    if (res.status === 429) throw new Error("Contact sync is limited to 4×/day — try again tomorrow.");
    if (!res.ok) throw new Error("Couldn't sync those numbers.");
    const b = (await res.json()) as {
      matched?: Array<{ handle: string; user_id: string; username: string }>;
    };
    return (b.matched ?? []).map((m) => ({ handle: m.handle, userId: m.user_id, username: m.username }));
  }

  /** createInvite mints a personal invite-a-friend link (expiry + max-uses). */
  async createInvite(): Promise<Invite> {
    const res = await this.authedRequest("POST", "/v1/contacts/invite", {});
    if (!res.ok) throw new Error("Couldn't create an invite link.");
    const b = (await res.json()) as { token: string; url: string; expires_at_ms: number; max_uses: number };
    return { token: b.token, url: b.url, expiresAtMs: b.expires_at_ms, maxUses: b.max_uses };
  }

  // ── groups (T5.09) ───────────────────────────────────────────────────────────

  private toGroupInfo(g: {
    id: string;
    name: string;
    description?: string;
    settings: GroupSettings;
    version: number;
    my_role?: string;
  }): GroupInfo {
    return {
      id: g.id,
      name: g.name,
      description: g.description ?? "",
      settings: g.settings,
      version: g.version,
      myRole: g.my_role ?? "member",
    };
  }

  /** createGroup makes a group (owner = me) with the given members and returns
   *  its conversation id (== group id). Navigates the caller into the thread. */
  async createGroup(name: string, description: string, memberIds: string[]): Promise<string> {
    const res = await this.authedRequest("POST", "/v1/groups", {
      name: name.trim(),
      description: description.trim(),
      member_ids: memberIds,
    });
    if (!res.ok) throw new Error("Couldn't create the group.");
    const b = (await res.json()) as { conversation_id: string; group: Parameters<AppServices["toGroupInfo"]>[0] };
    this.groupCache.set(b.conversation_id, this.toGroupInfo(b.group));
    this.notifyChange();
    return b.conversation_id;
  }

  /** loadGroup fetches (and caches) group info for a conversation. Returns null
   *  when the conversation isn't a group (404) — so callers can tell 1:1 apart. */
  async loadGroup(conversationId: string): Promise<GroupInfo | null> {
    const res = await this.authedRequest("GET", `/v1/groups/${conversationId}`);
    if (res.status === 404) {
      this.notGroup.add(conversationId);
      return null;
    }
    if (!res.ok) return this.groupCache.get(conversationId) ?? null;
    const g = this.toGroupInfo((await res.json()) as Parameters<AppServices["toGroupInfo"]>[0]);
    this.groupCache.set(conversationId, g);
    return g;
  }

  /** groupOf returns cached group info for a conversation, if known. */
  groupOf(conversationId: string): GroupInfo | undefined {
    return this.groupCache.get(conversationId);
  }

  /** ensureConversationKind lazily classifies a conversation (group vs direct)
   *  for the chat list, caching both outcomes so it fetches each at most once.
   *  Note: a recipient's inbox sets peerByConv even for groups, so we can't use
   *  that as a "direct" signal — only a confirmed 404 (notGroup) means direct. */
  ensureConversationKind(conversationId: string): void {
    if (this.groupCache.has(conversationId) || this.notGroup.has(conversationId)) return;
    void this.loadGroup(conversationId).then(() => this.notifyChange());
  }

  /** groupNameOf returns a cached group's name for a conversation, or "". */
  groupNameOf(conversationId: string): string {
    return this.groupCache.get(conversationId)?.name ?? "";
  }

  async listGroupMembers(conversationId: string): Promise<GroupMember[]> {
    const res = await this.authedRequest("GET", `/v1/groups/${conversationId}/members?limit=256`);
    if (!res.ok) return [];
    const b = (await res.json()) as { members?: Array<{ user_id: string; role: string }> };
    const members = (b.members ?? []).map((m) => ({ userId: m.user_id, role: m.role }));
    for (const m of members) void this.loadUserProfile(m.userId); // resolve names
    return members;
  }

  async addGroupMembers(conversationId: string, userIds: string[]): Promise<void> {
    const res = await this.authedRequest("POST", `/v1/groups/${conversationId}/members`, { user_ids: userIds });
    if (!res.ok) throw new Error("Couldn't add those members.");
  }
  async removeGroupMember(conversationId: string, userId: string): Promise<void> {
    const res = await this.authedRequest("DELETE", `/v1/groups/${conversationId}/members/${userId}`);
    if (!res.ok) throw new Error("Couldn't remove that member.");
  }
  /** setGroupRole promotes/demotes a member. role: 0=member, 1=admin (owner is
   *  not assignable — server rejects it). */
  async setGroupRole(conversationId: string, userId: string, role: number): Promise<void> {
    const res = await this.authedRequest("PUT", `/v1/groups/${conversationId}/members/${userId}/role`, { role });
    if (!res.ok) throw new Error("Couldn't change that member's role.");
  }
  async updateGroupInfo(conversationId: string, name: string, description: string): Promise<void> {
    const res = await this.authedRequest("PATCH", `/v1/groups/${conversationId}`, { name, description });
    if (!res.ok) throw new Error("Couldn't update the group.");
    const cached = this.groupCache.get(conversationId);
    if (cached) this.groupCache.set(conversationId, { ...cached, name, description });
    this.notifyChange();
  }
  async setGroupSettings(conversationId: string, settings: GroupSettings): Promise<void> {
    const res = await this.authedRequest("PUT", `/v1/groups/${conversationId}/settings`, settings);
    if (!res.ok) throw new Error("Couldn't save group settings.");
    const cached = this.groupCache.get(conversationId);
    if (cached) this.groupCache.set(conversationId, { ...cached, settings });
    this.notifyChange();
  }
  async leaveGroup(conversationId: string): Promise<void> {
    const res = await this.authedRequest("POST", `/v1/groups/${conversationId}/leave`);
    if (!res.ok) throw new Error("Couldn't leave the group.");
    this.groupCache.delete(conversationId);
  }
  async deleteGroup(conversationId: string): Promise<void> {
    const res = await this.authedRequest("DELETE", `/v1/groups/${conversationId}`);
    if (!res.ok) throw new Error("Couldn't delete the group.");
    this.groupCache.delete(conversationId);
  }
  async createGroupInvite(conversationId: string): Promise<GroupInviteLink> {
    const res = await this.authedRequest("POST", `/v1/groups/${conversationId}/invite-links`, {});
    if (!res.ok) throw new Error("Couldn't create a group invite link.");
    const b = (await res.json()) as { token: string; url: string; qr: string };
    return { token: b.token, url: b.url, qr: b.qr };
  }
  /** joinGroup consumes an invite token and returns the joined group's id. */
  async joinGroup(token: string): Promise<string> {
    const res = await this.authedRequest("POST", "/v1/groups/join", { token: token.trim() });
    if (!res.ok) throw new Error("That invite is invalid, expired, or full.");
    const b = (await res.json()) as { group: Parameters<AppServices["toGroupInfo"]>[0] };
    const g = this.toGroupInfo(b.group);
    this.groupCache.set(g.id, g);
    this.notifyChange();
    return g.id;
  }

  // ── stories / status (T5.11) ──────────────────────────────────────────────

  /** myUserId decodes the current access token's subject (the caller's user id),
   *  or "" when signed out. Used to tell own stories from contacts'. */
  myUserId(): string {
    const jwt = this.sessions.current()?.accessJwt;
    if (!jwt) return "";
    try {
      const payload = jwt.split(".")[1] ?? "";
      const json = atob(payload.replace(/-/g, "+").replace(/_/g, "/"));
      return (JSON.parse(json) as { sub?: string }).sub ?? "";
    } catch {
      return "";
    }
  }

  /** postStory publishes a status. kind is text|image|video; mediaRef is the
   *  encrypted blob id (null for text); audienceOverride restricts the viewers
   *  (null = the author's contacts). Content is cached locally for own-view. */
  async postStory(
    kind: string,
    mediaRef: string | null,
    audienceOverride: string[] | null,
    content: StoryContent,
  ): Promise<string> {
    const res = await this.authedRequest("POST", "/v1/stories", {
      kind,
      media_ref: mediaRef,
      audience_override: audienceOverride,
    });
    if (!res.ok) throw new Error("Couldn't post your status.");
    const b = (await res.json()) as { story_id: string; expires_at_ms: number };
    this.saveStoryContent(b.story_id, content);
    this.notifyChange();
    return b.story_id;
  }

  /** uploadStoryMedia encrypts + uploads an image/video and returns its media
   *  object id (the story's media_ref). Reuses the chat media pipeline; the
   *  per-file key would ride the STORY_KEY channel to viewers (dev seam). */
  async uploadStoryMedia(bytes: Uint8Array, mime: string): Promise<string> {
    const envelope = await this.mediaPipeline().prepare({ bytes, mime });
    return envelope.objectKey;
  }

  /** storyFeed returns the active stories the caller may view (own + contacts'). */
  async storyFeed(): Promise<StoryFeedItem[]> {
    const res = await this.authedRequest("GET", "/v1/stories/feed");
    if (!res.ok) return [];
    const b = (await res.json()) as {
      stories?: Array<{ story_id: string; author: string; expires_at_ms: number; key_available: boolean }>;
    };
    return (b.stories ?? []).map((s) => ({
      storyId: s.story_id,
      author: s.author,
      expiresAtMs: s.expires_at_ms,
      keyAvailable: s.key_available,
    }));
  }

  /** viewStory records that the caller viewed a story (drives view receipts). */
  async viewStory(storyId: string): Promise<void> {
    await this.authedRequest("POST", `/v1/stories/${storyId}/view`);
  }

  /** storyViewers returns who viewed a story (author-only; 403/404 → []). */
  async storyViewers(storyId: string): Promise<StoryViewer[]> {
    const res = await this.authedRequest("GET", `/v1/stories/${storyId}/viewers`);
    if (!res.ok) return [];
    const b = (await res.json()) as { viewers?: Array<{ user_id: string; viewed_at_ms: number }> };
    return (b.viewers ?? []).map((v) => ({ userId: v.user_id, viewedAtMs: v.viewed_at_ms }));
  }

  async deleteStory(storyId: string): Promise<void> {
    await this.authedRequest("DELETE", `/v1/stories/${storyId}`);
    try {
      localStorage.removeItem(`wa.story.${storyId}`);
    } catch {
      /* ignore */
    }
    this.notifyChange();
  }

  /** saveStoryContent stashes a story's displayable payload locally (dev seam —
   *  real content rides the E2EE STORY_KEY channel to each viewer). */
  saveStoryContent(storyId: string, content: StoryContent): void {
    try {
      localStorage.setItem(`wa.story.${storyId}`, JSON.stringify(content));
    } catch {
      /* quota/full — the placeholder renders instead */
    }
  }

  /** loadStoryContent returns a locally-cached story payload, or null (viewer
   *  without the STORY_KEY sees the encrypted placeholder). */
  loadStoryContent(storyId: string): StoryContent | null {
    try {
      const raw = localStorage.getItem(`wa.story.${storyId}`);
      return raw ? (JSON.parse(raw) as StoryContent) : null;
    } catch {
      return null;
    }
  }

  // ── devices / settings (T5.12) ────────────────────────────────────────────

  /** myDeviceId is this session's device id (to mark "This device" in the list). */
  myDeviceId(): string {
    return this.sessions.current()?.deviceId ?? "";
  }

  /** listDevices returns the account's linked devices (primary + linked). */
  async listDevices(): Promise<LinkedDevice[]> {
    const res = await this.authedRequest("GET", "/v1/devices");
    if (!res.ok) return [];
    const b = (await res.json()) as {
      devices?: Array<{ id: string; is_primary: boolean; platform: string; name: string; last_active_ms?: number }>;
    };
    return (b.devices ?? []).map((d) => ({
      id: d.id,
      isPrimary: d.is_primary,
      platform: d.platform,
      name: d.name,
      lastActiveMs: d.last_active_ms ?? 0,
    }));
  }

  async renameDevice(deviceId: string, name: string): Promise<void> {
    const res = await this.authedRequest("PATCH", `/v1/devices/${deviceId}`, { name });
    if (!res.ok) throw new Error("Couldn't rename that device.");
  }

  /** revokeDevice unlinks a device (revokes its sessions). Revoking your own
   *  device signs this session out. */
  async revokeDevice(deviceId: string): Promise<void> {
    const res = await this.authedRequest("DELETE", `/v1/devices/${deviceId}`);
    if (!res.ok) throw new Error("Couldn't revoke that device.");
  }

  // ── notifications / mute / badges (T5.13) ─────────────────────────────────

  /** setActiveConversation marks a conversation as on-screen: its unread count
   *  clears and inbound messages there raise no notification. Pass null on close. */
  setActiveConversation(conversationId: string | null): void {
    this.activeConv = conversationId;
    if (conversationId && (this.unreadByConv.get(conversationId) ?? 0) > 0) {
      this.unreadByConv.set(conversationId, 0);
      this.notifyChange();
    }
  }

  /** unreadCount is the number of unread inbound messages in a conversation. */
  unreadCount(conversationId: string): number {
    return this.unreadByConv.get(conversationId) ?? 0;
  }

  /** totalUnread is the sum of unread across all conversations (topbar badge). */
  totalUnread(): number {
    let n = 0;
    for (const v of this.unreadByConv.values()) n += v;
    return n;
  }

  isMuted(conversationId: string): boolean {
    return this.mutedConvs.has(conversationId);
  }

  toggleMute(conversationId: string): void {
    if (this.mutedConvs.has(conversationId)) this.mutedConvs.delete(conversationId);
    else this.mutedConvs.add(conversationId);
    try {
      localStorage.setItem("wa.mute.convs", JSON.stringify([...this.mutedConvs]));
    } catch {
      /* ignore */
    }
    this.notifyChange();
  }

  isGlobalMute(): boolean {
    return this.globalMuted;
  }

  setGlobalMute(on: boolean): void {
    this.globalMuted = on;
    try {
      localStorage.setItem("wa.mute.global", on ? "1" : "0");
    } catch {
      /* ignore */
    }
    this.notifyChange();
  }

  // ── chat conveniences (T5.15) ─────────────────────────────────────────────

  isFavorite(conversationId: string): boolean {
    return this.favoriteConvs.has(conversationId);
  }
  toggleFavorite(conversationId: string): void {
    if (this.favoriteConvs.has(conversationId)) this.favoriteConvs.delete(conversationId);
    else this.favoriteConvs.add(conversationId);
    this.persistSet("wa.fav.convs", this.favoriteConvs);
    this.notifyChange();
  }

  isArchived(conversationId: string): boolean {
    return this.archivedConvs.has(conversationId);
  }
  toggleArchive(conversationId: string): void {
    if (this.archivedConvs.has(conversationId)) this.archivedConvs.delete(conversationId);
    else this.archivedConvs.add(conversationId);
    this.persistSet("wa.archive.convs", this.archivedConvs);
    this.notifyChange();
  }

  /** chatWallpaper returns the per-chat wallpaper key (a preset name), or null. */
  chatWallpaper(conversationId: string): string | null {
    return this.wallpaperByConv.get(conversationId) ?? null;
  }
  setChatWallpaper(conversationId: string, key: string | null): void {
    if (key) this.wallpaperByConv.set(conversationId, key);
    else this.wallpaperByConv.delete(conversationId);
    this.persistMap("wa.wallpaper", this.wallpaperByConv);
    this.notifyChange();
  }

  /** draft returns the persisted composer draft for a conversation. */
  draft(conversationId: string): string {
    return this.draftByConv.get(conversationId) ?? "";
  }
  setDraft(conversationId: string, text: string): void {
    if (text) this.draftByConv.set(conversationId, text);
    else this.draftByConv.delete(conversationId);
    this.persistMap("wa.drafts", this.draftByConv);
  }

  /** exportChat renders a conversation's decrypted messages as plain text for a
   *  local download ("[time] Me/Peer: message"). Media/overlay rows are labelled. */
  async exportChat(conversationId: string): Promise<string> {
    const msgs = await this.thread(conversationId);
    const title = this.groupOf(conversationId)?.name ?? this.profileCache.get(this.peerByConv.get(conversationId) ?? "")?.displayName ?? conversationId;
    const lines = [`WhatsApp V2 — chat export: ${title}`, `Exported ${new Date().toISOString()}`, ""];
    for (const m of msgs) {
      const when = new Date(m.createdAt).toLocaleString();
      const who = m.mine ? "Me" : title;
      const text = m.deleted ? "(deleted)" : parseTextMessage(m.body).text || "(media)";
      lines.push(`[${when}] ${who}: ${text}`);
    }
    return lines.join("\n");
  }

  // ── undo-send (T5.15): a sent message is buffered for `windowMs`; the UI shows
  // an Undo bar. If not undone, it dispatches through the normal send path. Only
  // one message is buffered — starting another (or navigating) flushes the prior.
  sendTextWithUndo(conversationId: string, text: string, reply?: QuotedRef, windowMs = 5000): void {
    this.flushPendingSend();
    const timer = setTimeout(() => {
      this.pendingSend = null;
      void this.sendText(conversationId, text, reply);
      this.notifyChange();
    }, windowMs);
    this.pendingSend = { conversationId, text, reply, timer };
    this.notifyChange();
  }
  hasPendingSend(conversationId?: string): boolean {
    return this.pendingSend !== null && (conversationId === undefined || this.pendingSend.conversationId === conversationId);
  }
  /** undoSend cancels the buffered message and returns its text (to restore to
   *  the composer), or null if nothing was pending. */
  undoSend(): string | null {
    if (!this.pendingSend) return null;
    clearTimeout(this.pendingSend.timer);
    const { text } = this.pendingSend;
    this.pendingSend = null;
    this.notifyChange();
    return text;
  }
  private flushPendingSend(): void {
    if (!this.pendingSend) return;
    clearTimeout(this.pendingSend.timer);
    const { conversationId, text, reply } = this.pendingSend;
    this.pendingSend = null;
    void this.sendText(conversationId, text, reply);
  }

  /** onToast subscribes to in-app notification toasts (returns an unsubscribe). */
  onToast(cb: (n: NotificationEntry) => void): () => void {
    this.toastListeners.add(cb);
    return () => this.toastListeners.delete(cb);
  }

  /** notificationHistory returns recent in-app notifications (newest first). */
  notificationHistory(): NotificationEntry[] {
    return [...this.notifLog];
  }

  clearNotifications(): void {
    this.notifLog.length = 0;
    this.notifyChange();
  }

  // ── channels (T7.02) ──────────────────────────────────────────────────────

  // ── channel real-time push (T7.04) ──
  /** subscribeChannel asks the gateway to push post nudges for a channel (call on
   *  ChannelScreen open); unsubscribeChannel stops them (on close). */
  subscribeChannel(channelId: string): void {
    this.ws?.send({ t: "channel_sub", subscribe: [channelId], unsubscribe: [] });
  }
  unsubscribeChannel(channelId: string): void {
    this.ws?.send({ t: "channel_sub", subscribe: [], unsubscribe: [channelId] });
  }
  /** onChannelEvent fires when a followed/open channel gets a new post (the UI
   *  pulls the feed). Returns an unsubscribe. */
  onChannelEvent(cb: (channelId: string, postId: string) => void): () => void {
    this.channelEventListeners.add(cb);
    return () => this.channelEventListeners.delete(cb);
  }

  private toChannel(c: {
    id: string;
    handle: string;
    name: string;
    description?: string;
    kind: string;
    verified: boolean;
    followers: number;
    my_role?: string;
    premium?: boolean;
    price_cents?: number;
    my_subscribed?: boolean;
    created_at_ms: number;
  }): ChannelInfo {
    return {
      id: c.id,
      handle: c.handle,
      name: c.name,
      description: c.description ?? "",
      kind: c.kind,
      verified: c.verified,
      followers: c.followers,
      myRole: c.my_role ?? "",
      premium: !!c.premium,
      priceCents: c.price_cents ?? 0,
      mySubscribed: !!c.my_subscribed,
      createdAtMs: c.created_at_ms,
    };
  }

  private toPost(p: {
    id: string;
    channel_id: string;
    body: string;
    media_ref?: string;
    scheduled?: boolean;
    publish_at_ms: number;
    reactions?: Record<string, number>;
    comments: number;
    created_at_ms: number;
  }): ChannelPost {
    return {
      id: p.id,
      channelId: p.channel_id,
      body: p.body,
      mediaRef: p.media_ref ?? "",
      scheduled: !!p.scheduled,
      publishAtMs: p.publish_at_ms,
      reactions: p.reactions ?? {},
      comments: p.comments,
      createdAtMs: p.created_at_ms,
    };
  }

  async createChannel(handle: string, name: string, description: string, kind: "public" | "private"): Promise<string> {
    const res = await this.authedRequest("POST", "/v1/channels", { handle, name, description, kind });
    if (res.status === 409) throw new Error("That channel handle is already taken.");
    if (!res.ok) throw new Error("Couldn't create the channel — check the fields.");
    const c = this.toChannel((await res.json()) as Parameters<AppServices["toChannel"]>[0]);
    this.notifyChange();
    return c.id;
  }

  async getChannel(channelId: string): Promise<ChannelInfo | null> {
    const res = await this.authedRequest("GET", `/v1/channels/${channelId}`);
    if (!res.ok) return null;
    return this.toChannel((await res.json()) as Parameters<AppServices["toChannel"]>[0]);
  }

  async discoverChannels(): Promise<ChannelInfo[]> {
    const res = await this.authedRequest("GET", "/v1/channels/discover?limit=50");
    if (!res.ok) return [];
    const b = (await res.json()) as { channels?: Parameters<AppServices["toChannel"]>[0][] };
    return (b.channels ?? []).map((c) => this.toChannel(c));
  }

  async searchChannels(query: string): Promise<ChannelInfo[]> {
    const q = query.trim();
    if (q.length < 2) return [];
    const res = await this.authedRequest("GET", `/v1/channels/search?q=${encodeURIComponent(q)}&limit=50`);
    if (!res.ok) return [];
    const b = (await res.json()) as { channels?: Parameters<AppServices["toChannel"]>[0][] };
    return (b.channels ?? []).map((c) => this.toChannel(c));
  }

  async followChannel(channelId: string): Promise<void> {
    const res = await this.authedRequest("POST", `/v1/channels/${channelId}/follow`);
    if (!res.ok) throw new Error("Couldn't follow that channel.");
    this.notifyChange();
  }
  async unfollowChannel(channelId: string): Promise<void> {
    await this.authedRequest("DELETE", `/v1/channels/${channelId}/follow`);
    this.notifyChange();
  }
  async deleteChannel(channelId: string): Promise<void> {
    const res = await this.authedRequest("DELETE", `/v1/channels/${channelId}`);
    if (!res.ok) throw new Error("Couldn't delete the channel.");
    this.notifyChange();
  }

  async channelPosts(channelId: string): Promise<ChannelPost[]> {
    const res = await this.authedRequest("GET", `/v1/channels/${channelId}/posts?limit=100`);
    if (!res.ok) return [];
    const b = (await res.json()) as { posts?: Parameters<AppServices["toPost"]>[0][] };
    return (b.posts ?? []).map((p) => this.toPost(p));
  }

  /** postToChannel publishes (publishAtMs omitted/0) or schedules (future ms). */
  async postToChannel(channelId: string, body: string, publishAtMs?: number): Promise<void> {
    const res = await this.authedRequest("POST", `/v1/channels/${channelId}/posts`, {
      body,
      publish_at_ms: publishAtMs ?? 0,
    });
    if (!res.ok) throw new Error("Couldn't publish the post.");
    this.notifyChange();
  }
  async deleteChannelPost(postId: string): Promise<void> {
    await this.authedRequest("DELETE", `/v1/channel-posts/${postId}`);
    this.notifyChange();
  }
  async reactToPost(postId: string, emoji: string, on: boolean): Promise<void> {
    await this.authedRequest("POST", `/v1/channel-posts/${postId}/react`, { emoji, on });
  }
  async postComments(postId: string): Promise<Array<{ id: string; authorId: string; body: string; createdAtMs: number }>> {
    const res = await this.authedRequest("GET", `/v1/channel-posts/${postId}/comments?limit=100`);
    if (!res.ok) return [];
    const b = (await res.json()) as { comments?: Array<{ id: string; author_id: string; body: string; created_at_ms: number }> };
    return (b.comments ?? []).map((c) => ({ id: c.id, authorId: c.author_id, body: c.body, createdAtMs: c.created_at_ms }));
  }
  async commentOnPost(postId: string, body: string): Promise<void> {
    const res = await this.authedRequest("POST", `/v1/channel-posts/${postId}/comments`, { body });
    if (!res.ok) throw new Error("Couldn't post the comment.");
  }

  /** recordPostView bumps a channel post's aggregate view counter (best-effort). */
  async recordPostView(postId: string): Promise<void> {
    await this.authedRequest("POST", `/v1/channel-posts/${postId}/view`).catch(() => {});
  }

  /** channelInsights returns a channel's aggregate analytics (admin-only → null). */
  async channelInsights(channelId: string): Promise<ChannelInsights | null> {
    const res = await this.authedRequest("GET", `/v1/channels/${channelId}/insights`);
    if (!res.ok) return null;
    const i = (await res.json()) as {
      followers: number;
      subscribers: number;
      posts: number;
      views: number;
      reactions: number;
      comments: number;
      premium: boolean;
      price_cents: number;
    };
    return {
      followers: i.followers,
      subscribers: i.subscribers,
      posts: i.posts,
      views: i.views,
      reactions: i.reactions,
      comments: i.comments,
      premium: i.premium,
      priceCents: i.price_cents,
    };
  }

  /** setChannelPremium toggles the premium gate + monthly price in cents (owner). */
  async setChannelPremium(channelId: string, premium: boolean, priceCents: number): Promise<void> {
    const res = await this.authedRequest("PATCH", `/v1/channels/${channelId}/premium`, { premium, price_cents: priceCents });
    if (!res.ok) throw new Error("Couldn't update premium settings.");
    this.notifyChange();
  }

  /** subscribeToChannel runs the (seam) payment + grants access. Returns the
   *  processor reference (a noop placeholder in this dev build). */
  async subscribeToChannel(channelId: string): Promise<string> {
    const res = await this.authedRequest("POST", `/v1/channels/${channelId}/subscribe`);
    if (res.status === 402) throw new Error("The payment could not be completed.");
    if (!res.ok) throw new Error("Couldn't subscribe.");
    const b = (await res.json()) as { payment_ref: string };
    this.notifyChange();
    return b.payment_ref;
  }

  // ── communities (T8.02) ───────────────────────────────────────────────────

  private toCommunity(c: {
    id: string;
    name: string;
    description?: string;
    kind: string;
    announcement_group_id: string;
    member_count: number;
    group_count: number;
    my_role?: string;
  }): CommunityInfo {
    return {
      id: c.id,
      name: c.name,
      description: c.description ?? "",
      kind: c.kind,
      announcementGroupId: c.announcement_group_id,
      memberCount: c.member_count,
      groupCount: c.group_count,
      myRole: c.my_role ?? "",
    };
  }

  private toCommunitySummary(c: { id: string; name: string; description?: string; member_count: number }): CommunitySummary {
    return { id: c.id, name: c.name, description: c.description ?? "", memberCount: c.member_count };
  }

  /** createCommunity registers a community (+ its announcement group). Returns id. */
  async createCommunity(name: string, description: string, kind: "public" | "private"): Promise<string> {
    const res = await this.authedRequest("POST", "/v1/communities", { name, description, kind });
    if (!res.ok) throw new Error("Couldn't create the community — check the fields.");
    const b = (await res.json()) as { id: string };
    this.notifyChange();
    return b.id;
  }

  async getCommunity(id: string): Promise<CommunityInfo> {
    const res = await this.authedRequest("GET", `/v1/communities/${id}`);
    if (!res.ok) throw new Error("Couldn't load the community.");
    return this.toCommunity((await res.json()) as Parameters<AppServices["toCommunity"]>[0]);
  }

  async discoverCommunities(): Promise<CommunitySummary[]> {
    const res = await this.authedRequest("GET", "/v1/communities/discover?limit=50");
    if (!res.ok) return [];
    const b = (await res.json()) as { communities?: Parameters<AppServices["toCommunitySummary"]>[0][] };
    return (b.communities ?? []).map((c) => this.toCommunitySummary(c));
  }

  async searchCommunities(query: string): Promise<CommunitySummary[]> {
    const res = await this.authedRequest("GET", `/v1/communities/search?q=${encodeURIComponent(query)}`);
    if (!res.ok) return [];
    const b = (await res.json()) as { communities?: Parameters<AppServices["toCommunitySummary"]>[0][] };
    return (b.communities ?? []).map((c) => this.toCommunitySummary(c));
  }

  async joinCommunity(id: string): Promise<void> {
    const res = await this.authedRequest("POST", `/v1/communities/${id}/join`);
    if (!res.ok) throw new Error("Couldn't join the community.");
    this.notifyChange();
  }
  async leaveCommunity(id: string): Promise<void> {
    const res = await this.authedRequest("POST", `/v1/communities/${id}/leave`);
    if (!res.ok) throw new Error("Couldn't leave the community.");
    this.notifyChange();
  }
  async deleteCommunity(id: string): Promise<void> {
    const res = await this.authedRequest("DELETE", `/v1/communities/${id}`);
    if (!res.ok) throw new Error("Couldn't delete the community.");
    this.notifyChange();
  }

  async communityMembers(id: string): Promise<CommunityMember[]> {
    const res = await this.authedRequest("GET", `/v1/communities/${id}/members`);
    if (!res.ok) return [];
    const b = (await res.json()) as { members?: { user_id: string; role: string }[] };
    return (b.members ?? []).map((m) => ({ userId: m.user_id, role: m.role }));
  }
  async setCommunityRole(id: string, userId: string, role: "member" | "admin"): Promise<void> {
    const res = await this.authedRequest("PUT", `/v1/communities/${id}/members/${userId}/role`, { role });
    if (!res.ok) throw new Error("Couldn't change the role.");
    this.notifyChange();
  }
  async removeCommunityMember(id: string, userId: string): Promise<void> {
    const res = await this.authedRequest("DELETE", `/v1/communities/${id}/members/${userId}`);
    if (!res.ok) throw new Error("Couldn't remove the member.");
    this.notifyChange();
  }

  async communityGroupIds(id: string): Promise<string[]> {
    const res = await this.authedRequest("GET", `/v1/communities/${id}/groups`);
    if (!res.ok) return [];
    const b = (await res.json()) as { group_ids?: string[] };
    return b.group_ids ?? [];
  }
  async addCommunityGroup(id: string, groupId: string): Promise<void> {
    const res = await this.authedRequest("POST", `/v1/communities/${id}/groups`, { group_id: groupId });
    if (!res.ok) throw new Error("Couldn't add the group.");
    this.notifyChange();
  }
  async removeCommunityGroup(id: string, groupId: string): Promise<void> {
    const res = await this.authedRequest("DELETE", `/v1/communities/${id}/groups/${groupId}`);
    if (!res.ok) throw new Error("Couldn't remove the group.");
    this.notifyChange();
  }

  async communityEvents(id: string): Promise<CommunityEvent[]> {
    const res = await this.authedRequest("GET", `/v1/communities/${id}/events`);
    if (!res.ok) return [];
    const b = (await res.json()) as { events?: { id: string; title: string; description?: string; starts_at_ms: number; created_by: string }[] };
    return (b.events ?? []).map((e) => ({ id: e.id, title: e.title, description: e.description ?? "", startsAtMs: e.starts_at_ms, createdBy: e.created_by }));
  }
  async createCommunityEvent(id: string, title: string, description: string, startsAtMs: number): Promise<void> {
    const res = await this.authedRequest("POST", `/v1/communities/${id}/events`, { title, description, starts_at_ms: startsAtMs });
    if (!res.ok) throw new Error("Couldn't create the event.");
    this.notifyChange();
  }
  async deleteCommunityEvent(id: string, eventId: string): Promise<void> {
    const res = await this.authedRequest("DELETE", `/v1/communities/${id}/events/${eventId}`);
    if (!res.ok) throw new Error("Couldn't delete the event.");
    this.notifyChange();
  }

  /** isPeerTyping — the peer sent a typing signal within the last few seconds. */
  isPeerTyping(conversationId: string): boolean {
    const exp = this.typingByConv.get(conversationId);
    return exp !== undefined && exp > Date.now();
  }
  /** presenceOf returns a tracked user's online/last-seen, if known. */
  presenceOf(userId: string): { online: boolean; lastSeenMs: number } | undefined {
    return this.presenceByUser.get(userId);
  }
  /** subscribePresence starts tracking a peer's online-state + typing (call on thread open). */
  subscribePresence(userId: string): void {
    this.ws?.send({ t: "presence_sub", subscribe: [userId], unsubscribe: [] });
  }
  /** sendTyping relays my typing state for a conversation (throttle at the caller). */
  sendTyping(conversationId: string, recording: boolean): void {
    this.ws?.send({ t: "typing", conversationId, recording });
  }
  /** onCallSignal registers the CallProvider's handler for WS call frames; pass
   *  null to detach. Offers/rings/ends arrive on dev.{id}.call and drive the
   *  CallSession (outgoing calls + answer/decline go via REST in callControl). */
  onCallSignal(handler: CallSignalHandler | null): void {
    this.callHandler = handler;
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

  /** markRead tells the sender their messages in this conversation were read
   *  (✓✓ blue) up to upToSeq. Call when the thread is on screen. */
  markRead(conversationId: string, upToSeq: number): void {
    if (upToSeq > 0) this.ws?.send({ t: "receipt", conversationId, kind: "READ", upToSeq });
  }

  conversations(): Promise<ChatSummary[]> {
    return this.db.conversations();
  }

  thread(conversationId: string): Promise<ThreadMessage[]> {
    return this.db.thread(conversationId);
  }

  /** Full-text search over the local decrypted store (runs in the DB worker). */
  search(
    query: string,
    opts?: {
      conversationId?: string;
      limit?: number;
      fromMe?: boolean;
      after?: number;
      before?: number;
      mediaOnly?: boolean;
      hashtag?: string;
    },
  ): Promise<SearchHit[]> {
    return this.db.search({
      query,
      conversationId: opts?.conversationId,
      limit: opts?.limit,
      fromMe: opts?.fromMe,
      after: opts?.after,
      before: opts?.before,
      mediaOnly: opts?.mediaOnly,
      hashtag: opts?.hashtag,
    });
  }

  async logout(): Promise<void> {
    this.ws?.stop();
    this.ws = null;
    await this.sessions.clear();
    this.notifyAuth(false); // route the UI to login (covers a failed-refresh logout)
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

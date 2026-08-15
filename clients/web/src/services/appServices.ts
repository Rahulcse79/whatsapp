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
import { MediaPipeline, ResumableUploader, encodeMediaMessage, encodeTextMessage, generateLinkPreview, type QuotedRef } from "@wa/media-pipeline";
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

export class AppServices {
  readonly otp: OtpClient;
  readonly sessions: SessionManager;
  readonly db: DbApi;

  private readonly http = createHttpClient(config.apiBaseUrl);
  private ws: WsClient | null = null;
  private cursorMirror: ConversationCursor[] = [];
  private readonly changeListeners = new Set<() => void>();
  private readonly peerByConv = new Map<string, string>(); // conversationId → peer userId
  private readonly profileCache = new Map<string, PublicProfile>(); // userId → public profile
  private readonly groupCache = new Map<string, GroupInfo>(); // conversationId → group info
  private readonly notGroup = new Set<string>(); // conversationIds confirmed to be direct chats
  private readonly typingByConv = new Map<string, number>(); // conversationId → typing-expiry ts
  private readonly presenceByUser = new Map<string, { online: boolean; lastSeenMs: number }>();

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
          // Inbound items are always from the peer (I never receive my own), so
          // their sender is this conversation's other participant.
          for (const it of b.items) this.peerByConv.set(it.conversationId, it.senderUserId);
          const watermark = await this.db.persistInboxBatch(b);
          this.mergeCursors(watermark);
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

  /** deleteForMe hides the message locally only — no overlay leaves the device. */
  async deleteForMe(msgUuid: string): Promise<void> {
    await this.db.deleteForMe({ msgUuid });
    this.notifyChange();
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

import {
  SNIPPET_CLOSE,
  SNIPPET_OPEN,
  isValidPhone,
  type ChatSummary,
  type SearchHit,
  type ThreadMessage,
} from "@wa/client-core";
import {
  classifyMedia,
  parseMediaMessage,
  parseTextMessage,
  type LinkPreview,
  type MediaEnvelope,
  type QuotedRef,
} from "@wa/media-pipeline";
import { useEffect, useRef, useState, type ChangeEvent, type CSSProperties, type FormEvent, type KeyboardEvent, type ReactNode } from "react";
import { registerWebPush } from "../push";
import { messageOf } from "./errors";
import { useCall } from "../call/CallContext";
import { DownloadsPanel } from "./media/DownloadsPanel";
import { Gallery } from "./media/Gallery";
import { MediaMessage } from "./media/MediaMessage";
import { useServices } from "./ServicesContext";

/** onActivate makes a non-<button> clickable element keyboard-operable — Enter or
 *  Space fires it, matching native button behaviour (a11y: interactive controls
 *  must be focusable and keyboard-operable, axe rule interactive-supports-focus). */
function onActivate(handler: () => void) {
  return (e: KeyboardEvent) => {
    if (e.key === "Enter" || e.key === " ") {
      e.preventDefault();
      handler();
    }
  };
}

export function NewChat({
  onStarted,
  onBack,
}: {
  onStarted: (conversationId: string) => void;
  onBack: () => void;
}) {
  const { services } = useServices();
  const [phone, setPhone] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function submit(e: FormEvent): Promise<void> {
    e.preventDefault();
    const trimmed = phone.trim();
    if (!isValidPhone(trimmed)) {
      setError("Enter the number in international format, e.g. +14155550123.");
      return;
    }
    setBusy(true);
    setError(null);
    try {
      onStarted(await services.startDirectChat(trimmed));
    } catch (err) {
      setError(messageOf(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <form className="card" onSubmit={submit}>
      <button type="button" className="btn small" onClick={onBack}>
        ‹ Back
      </button>
      <h1>New chat</h1>
      <p className="muted">Enter the phone number of someone who has an account.</p>
      <input
        className="input"
        value={phone}
        onChange={(e) => setPhone(e.target.value)}
        placeholder="+14155550123"
        aria-label="Contact phone number in international format"
        inputMode="tel"
        autoFocus
        disabled={busy}
      />
      {error && (
        <p className="error" role="alert">
          {error}
        </p>
      )}
      <button className="btn" type="submit" disabled={busy}>
        {busy ? "Starting…" : "Start chat"}
      </button>
    </form>
  );
}

const PRIVACY_FIELDS: { key: string; label: string }[] = [
  { key: "last_seen", label: "Last seen & online" },
  { key: "avatar", label: "Profile photo" },
  { key: "about", label: "About" },
  { key: "read_receipts", label: "Read receipts" },
];
const PRIVACY_OPTIONS = ["everyone", "contacts", "nobody"];
const fieldLabel: CSSProperties = { display: "block", fontSize: "0.8rem" };
const fieldWrap: CSSProperties = { display: "block", marginBottom: "0.6rem" };

export function Profile({ onBack }: { onBack: () => void }) {
  const { services } = useServices();
  const [displayName, setDisplayName] = useState("");
  const [username, setUsername] = useState("");
  const [about, setAbout] = useState("");
  const [privacy, setPrivacy] = useState<Record<string, string>>({});
  const [blocked, setBlocked] = useState<string[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [saved, setSaved] = useState(false);

  useEffect(() => {
    let alive = true;
    services
      .getMyProfile()
      .then((p) => {
        if (!alive) return;
        setDisplayName(p.displayName);
        setUsername(p.username);
        setAbout(p.about);
        setPrivacy(p.privacy);
      })
      .catch(() => {});
    services
      .getBlocked()
      .then((ids) => {
        if (!alive) return;
        setBlocked(ids);
        for (const id of ids) void services.loadUserProfile(id); // resolve names
      })
      .catch(() => {});
    const unsub = services.onChange(() => {
      if (alive) setBlocked((b) => [...b]); // re-render as peer names arrive
    });
    return () => {
      alive = false;
      unsub();
    };
  }, [services]);

  async function save(e: FormEvent): Promise<void> {
    e.preventDefault();
    setBusy(true);
    setError(null);
    setSaved(false);
    try {
      await services.updateMyProfile({
        displayName: displayName.trim(),
        username: username.trim(),
        about: about.trim(),
      });
      setSaved(true);
    } catch (err) {
      setError(messageOf(err));
    } finally {
      setBusy(false);
    }
  }

  async function changePrivacy(key: string, value: string): Promise<void> {
    const next = { ...privacy, [key]: value };
    setPrivacy(next); // optimistic
    try {
      await services.saveMyPrivacy(next);
    } catch (err) {
      setError(messageOf(err));
    }
  }

  async function unblock(userId: string): Promise<void> {
    await services.unblockUser(userId);
    setBlocked((b) => b.filter((x) => x !== userId));
  }

  return (
    <div className="card">
      <button type="button" className="btn small" onClick={onBack}>
        ‹ Back
      </button>
      <h1>Your profile</h1>
      <form onSubmit={save}>
        <label style={fieldWrap}>
          <span className="muted" style={fieldLabel}>
            Display name
          </span>
          <input
            className="input"
            value={displayName}
            onChange={(e) => setDisplayName(e.target.value)}
            placeholder="Your name"
            aria-label="Display name"
            maxLength={100}
            disabled={busy}
          />
        </label>
        <label style={fieldWrap}>
          <span className="muted" style={fieldLabel}>
            Username (a–z, 0–9, _ .)
          </span>
          <input
            className="input"
            value={username}
            onChange={(e) => setUsername(e.target.value)}
            placeholder="username"
            aria-label="Username"
            maxLength={30}
            disabled={busy}
          />
        </label>
        <label style={fieldWrap}>
          <span className="muted" style={fieldLabel}>
            About
          </span>
          <input
            className="input"
            value={about}
            onChange={(e) => setAbout(e.target.value)}
            placeholder="Hey there! I use WhatsApp V2"
            aria-label="About"
            maxLength={200}
            disabled={busy}
          />
        </label>
        {error && (
          <p className="error" role="alert">
            {error}
          </p>
        )}
        {saved && (
          <p className="muted" role="status">
            Saved ✓
          </p>
        )}
        <button className="btn" type="submit" disabled={busy}>
          {busy ? "Saving…" : "Save"}
        </button>
      </form>

      <h2 style={{ marginTop: "1.5rem" }}>Privacy</h2>
      {PRIVACY_FIELDS.map((f) => (
        <label key={f.key} style={fieldWrap}>
          <span className="muted" style={fieldLabel}>
            {f.label}
          </span>
          <select
            className="input"
            value={privacy[f.key] ?? "everyone"}
            aria-label={f.label}
            onChange={(e) => void changePrivacy(f.key, e.target.value)}
          >
            {PRIVACY_OPTIONS.map((o) => (
              <option key={o} value={o}>
                {o.charAt(0).toUpperCase() + o.slice(1)}
              </option>
            ))}
          </select>
        </label>
      ))}

      <h2 style={{ marginTop: "1.5rem" }}>Blocked ({blocked.length})</h2>
      {blocked.length === 0 ? (
        <p className="muted">You haven't blocked anyone.</p>
      ) : (
        <ul className="list">
          {blocked.map((id) => (
            <li key={id} className="row" style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
              <span>{services.nameForUser(id)}</span>
              <button className="btn small ghost" onClick={() => void unblock(id)}>
                Unblock
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

export function Login({ onRequested }: { onRequested: (challengeId: string, phone: string) => void }) {
  const { services } = useServices();
  const [phone, setPhone] = useState("");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function submit(e: FormEvent): Promise<void> {
    e.preventDefault();
    const trimmed = phone.trim();
    if (!isValidPhone(trimmed)) {
      setError("Enter your number in international format, e.g. +14155550123.");
      return;
    }
    setBusy(true);
    setError(null);
    try {
      const ch = await services.otp.requestOtp(trimmed);
      onRequested(ch.challengeId, trimmed);
    } catch (err) {
      setError(messageOf(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <form className="card" onSubmit={submit}>
      <h1>Your phone number</h1>
      <p className="muted">We&apos;ll send a one-time code to confirm it&apos;s you.</p>
      <input
        className="input"
        value={phone}
        onChange={(e) => setPhone(e.target.value)}
        placeholder="+14155550123"
        aria-label="Phone number in international format"
        inputMode="tel"
        autoComplete="tel"
        autoFocus
        disabled={busy}
      />
      {error ? <p className="error">{error}</p> : null}
      <button className="btn" type="submit" disabled={busy}>
        {busy ? "Sending…" : "Send code"}
      </button>
    </form>
  );
}

export function Verify({
  challengeId,
  phone,
  onDone,
}: {
  challengeId: string;
  phone: string;
  onDone: () => void;
}) {
  const { services, setAuthed } = useServices();
  const [code, setCode] = useState("");
  const [pin, setPin] = useState("");
  const [needsPin, setNeedsPin] = useState(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  async function submit(e: FormEvent): Promise<void> {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const s = needsPin
        ? await services.otp.verifyPin(challengeId, pin)
        : await services.otp.verifyOtp(challengeId, code, { name: "WhatsApp V2 Web", platform: "web" });
      if (s.requiresPin) {
        setNeedsPin(true);
        return;
      }
      await services.completeLogin(s);
      setAuthed(true);
      void registerWebPush();
      onDone();
    } catch (err) {
      setError(messageOf(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <form className="card" onSubmit={submit}>
      <h1>{needsPin ? "Enter your PIN" : "Enter the code"}</h1>
      <p className="muted">{needsPin ? "This device needs your 2-step PIN." : `Sent to ${phone}`}</p>
      {needsPin ? (
        <input
          className="input code"
          value={pin}
          onChange={(e) => setPin(e.target.value)}
          placeholder="••••••"
          aria-label="2-step verification PIN"
          inputMode="numeric"
          type="password"
          autoComplete="one-time-code"
          autoFocus
          disabled={busy}
        />
      ) : (
        <input
          className="input code"
          value={code}
          onChange={(e) => setCode(e.target.value)}
          placeholder="123456"
          aria-label="One-time verification code"
          inputMode="numeric"
          autoComplete="one-time-code"
          autoFocus
          disabled={busy}
        />
      )}
      {error ? <p className="error">{error}</p> : null}
      <button className="btn" type="submit" disabled={busy}>
        {busy ? "Verifying…" : "Verify"}
      </button>
    </form>
  );
}

export function ChatList({
  onOpen,
  onNew,
  onSearch,
  onProfile,
}: {
  onOpen: (id: string) => void;
  onNew: () => void;
  onSearch: () => void;
  onProfile: () => void;
}) {
  const { services } = useServices();
  const [items, setItems] = useState<ChatSummary[]>([]);

  useEffect(() => {
    let alive = true;
    const tick = (): void => {
      services
        .conversations()
        .then((c) => {
          if (!alive) return;
          setItems(c);
          for (const conv of c) {
            const peer = services.peerOf(conv.conversationId);
            if (peer) void services.loadUserProfile(peer); // resolve names for rows
          }
        })
        .catch(() => {});
    };
    tick();
    const unsub = services.onChange(tick); // instant refresh on inbound/ack/send
    const handle = setInterval(tick, 5000); // safety net for missed signals
    return () => {
      alive = false;
      unsub();
      clearInterval(handle);
    };
  }, [services]);

  return (
    <div className="pane">
      <div className="pane-head">
        <span>Chats</span>
        <span className="head-actions">
          <button className="btn small ghost" onClick={onProfile} aria-label="Your profile">
            👤 You
          </button>
          <button className="btn small ghost" onClick={onSearch} aria-label="Search messages">
            🔍 Search
          </button>
          <button className="btn small" onClick={onNew}>
            ＋ New
          </button>
        </span>
      </div>
      {items.length === 0 ? (
        <p className="muted center">No conversations yet. Start one with ＋ New.</p>
      ) : (
        <ul className="list">
          {items.map((c) => (
            <li
              key={c.conversationId}
              className="row"
              role="button"
              tabIndex={0}
              onClick={() => onOpen(c.conversationId)}
              onKeyDown={onActivate(() => onOpen(c.conversationId))}
            >
              <div className="row-title">{services.peerNameOf(c.conversationId) || c.title}</div>
              <div className="row-sub">{c.lastPreview || "No messages yet"}</div>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

/** Search screen: debounced full-text search over the local decrypted store
 *  (ADR-005). A result opens its conversation. */
export function Search({ onOpen, onBack }: { onOpen: (id: string) => void; onBack: () => void }) {
  const { services } = useServices();
  const [query, setQuery] = useState("");
  const [hits, setHits] = useState<SearchHit[]>([]);

  useEffect(() => {
    const q = query.trim();
    if (!q) {
      setHits([]);
      return;
    }
    let alive = true;
    const handle = setTimeout(() => {
      services
        .search(q)
        .then((r) => {
          if (alive) setHits(r);
        })
        .catch(() => {});
    }, 150); // debounce keystrokes
    return () => {
      alive = false;
      clearTimeout(handle);
    };
  }, [query, services]);

  return (
    <div className="pane">
      <div className="pane-head">
        <button className="btn small ghost" onClick={onBack}>
          ‹ Back
        </button>
        <input
          className="input"
          type="search"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="Search messages"
          aria-label="Search messages"
          autoFocus
        />
      </div>
      {query.trim() && hits.length === 0 ? <p className="muted center">No matches.</p> : null}
      <ul className="list">
        {hits.map((h) => (
          <li
            key={h.msgUuid}
            className="row"
            role="button"
            tabIndex={0}
            onClick={() => onOpen(h.conversationId)}
            onKeyDown={onActivate(() => onOpen(h.conversationId))}
          >
            <div className="row-title">{h.conversationTitle}</div>
            <div className="row-sub">{highlightSnippet(h.snippet)}</div>
          </li>
        ))}
      </ul>
    </div>
  );
}

/** highlightSnippet turns the SNIPPET_OPEN/CLOSE-delimited excerpt into React
 *  nodes, wrapping matched terms in <mark>. */
function highlightSnippet(snippet: string): ReactNode {
  const parts: ReactNode[] = [];
  let rest = snippet;
  let key = 0;
  for (;;) {
    const open = rest.indexOf(SNIPPET_OPEN);
    const close = open === -1 ? -1 : rest.indexOf(SNIPPET_CLOSE, open + SNIPPET_OPEN.length);
    if (open === -1 || close === -1) {
      parts.push(rest);
      break;
    }
    if (open > 0) parts.push(rest.slice(0, open));
    parts.push(<mark key={key++}>{rest.slice(open + SNIPPET_OPEN.length, close)}</mark>);
    rest = rest.slice(close + SNIPPET_CLOSE.length);
  }
  return parts;
}

export function Thread({ conversationId, onBack }: { conversationId: string; onBack: () => void }) {
  const { services } = useServices();
  const call = useCall();
  const [messages, setMessages] = useState<ThreadMessage[]>([]);
  const [draft, setDraft] = useState("");
  const [gallery, setGallery] = useState<{ items: MediaEnvelope[]; startKey: string } | null>(null);
  const [replyingTo, setReplyingTo] = useState<QuotedRef | null>(null);
  const [editing, setEditing] = useState<string | null>(null); // msgUuid being edited
  const [sendingMedia, setSendingMedia] = useState(false);
  const fileRef = useRef<HTMLInputElement>(null);

  const lastReadRef = useRef(0);
  const subscribedRef = useRef(false);
  const lastTypingRef = useRef(0);
  useEffect(() => {
    let alive = true;
    lastReadRef.current = 0; // reset the read watermark per conversation
    subscribedRef.current = false;
    const tick = (): void => {
      services
        .thread(conversationId)
        .then((m) => {
          if (!alive) return;
          setMessages(m);
          // Thread is on screen → tell the sender their messages were read (✓✓ blue).
          const maxRecv = m.reduce((mx, x) => (!x.mine && x.seq > mx ? x.seq : mx), 0);
          if (maxRecv > lastReadRef.current) {
            lastReadRef.current = maxRecv;
            services.markRead(conversationId, maxRecv);
          }
          // Once the peer is known, subscribe to their presence + typing (once).
          const peerId = services.peerOf(conversationId);
          if (peerId && !subscribedRef.current) {
            subscribedRef.current = true;
            services.subscribePresence(peerId);
            void services.loadUserProfile(peerId); // resolve a display name for the header
          }
        })
        .catch(() => {});
    };
    tick();
    const unsub = services.onChange(tick); // instant refresh on inbound/ack/send/typing/presence
    const handle = setInterval(tick, 5000); // safety net for missed signals
    return () => {
      alive = false;
      unsub();
      clearInterval(handle);
    };
  }, [services, conversationId]);

  const peerId = services.peerOf(conversationId);
  const presence = peerId ? services.presenceOf(peerId) : undefined;
  const statusLine = services.isPeerTyping(conversationId)
    ? "typing…"
    : presence
      ? presence.online
        ? "online"
        : presence.lastSeenMs
          ? `last seen ${formatLastSeen(presence.lastSeenMs)}`
          : ""
      : "";

  function onDraftChange(v: string): void {
    setDraft(v);
    const now = Date.now();
    if (v && now - lastTypingRef.current > 3000) {
      lastTypingRef.current = now;
      services.sendTyping(conversationId, true); // throttled 1/3s
    }
  }

  async function send(e: FormEvent): Promise<void> {
    e.preventDefault();
    const text = draft.trim();
    if (!text) return;
    setDraft("");
    lastTypingRef.current = 0;
    services.sendTyping(conversationId, false); // stop the typing indicator on send
    if (editing) {
      const target = editing;
      setEditing(null);
      await services.editMessage(conversationId, target, text);
    } else {
      const reply = replyingTo ?? undefined;
      setReplyingTo(null);
      await services.sendText(conversationId, text, reply);
    }
    const next = await services.thread(conversationId);
    setMessages(next);
  }

  async function onPickFile(e: ChangeEvent<HTMLInputElement>): Promise<void> {
    const file = e.target.files?.[0];
    e.target.value = ""; // allow re-picking the same file
    if (!file) return;
    setSendingMedia(true);
    try {
      await services.sendMedia(conversationId, file);
      setMessages(await services.thread(conversationId));
    } catch (err) {
      window.alert(`Couldn't send the file: ${err instanceof Error ? err.message : "upload failed"}`);
    } finally {
      setSendingMedia(false);
    }
  }

  // Message-action handlers passed down to each bubble.
  const actions: MessageActions = {
    reply: (m) => {
      setEditing(null);
      setReplyingTo({ msgUuid: m.msgUuid, snippet: snippetOf(m), mine: m.mine });
    },
    edit: (m) => {
      setReplyingTo(null);
      setEditing(m.msgUuid);
      setDraft(parseTextMessage(m.body).text);
    },
    copy: (m) => void navigator.clipboard?.writeText(parseTextMessage(m.body).text).catch(() => {}),
    deleteForEveryone: (m) => void services.deleteForEveryone(conversationId, m.msgUuid),
    deleteForMe: (m) => void services.deleteForMe(m.msgUuid),
    togglePin: (m) => void services.togglePin(m.msgUuid, !m.pinned),
    toggleStar: (m) => void services.toggleStar(m.msgUuid, !m.starred),
  };

  // Every image/video in the thread, so the lightbox can page across them.
  const visuals: MediaEnvelope[] = [];
  for (const m of messages) {
    if (m.deleted) continue;
    const media = parseMediaMessage(m.body);
    if (media) for (const a of media.attachments) if (isVisual(a)) visuals.push(a);
  }

  return (
    <div className="pane">
      <div className="pane-head">
        <button className="btn small ghost" onClick={onBack}>
          ‹ Back
        </button>
        <div className="thread-title">
          <span className={services.peerNameOf(conversationId) ? "" : "mono"}>
            {services.peerNameOf(conversationId) || conversationId.slice(0, 12)}
          </span>
          {statusLine ? (
            <span className="thread-status" style={{ fontSize: "0.72rem", opacity: 0.7 }}>
              {statusLine}
            </span>
          ) : null}
        </div>
        <button
          className="btn small ghost call-btn"
          title="Voice call"
          aria-label="Start voice call"
          onClick={() => void call.startCall(conversationId, "voice")}
        >
          <span aria-hidden>📞</span>
        </button>
      </div>
      <div className="messages">
        {messages.length === 0 ? <p className="muted center">Say hello 👋</p> : null}
        {messages.map((m) => (
          <MessageBubble
            key={m.msgUuid}
            message={m}
            actions={actions}
            onOpen={(env) => setGallery({ items: visuals, startKey: env.objectKey })}
          />
        ))}
      </div>
      <DownloadsPanel />
      {replyingTo || editing ? (
        <div className="reply-bar" style={{ display: "flex", alignItems: "center", gap: 8, padding: "6px 10px", borderTop: "1px solid var(--border, #e2e2e2)", fontSize: "0.82rem" }}>
          <span style={{ flex: 1, opacity: 0.8 }}>
            {editing ? "✎ Editing message" : `↩ Replying: ${replyingTo?.snippet}`}
          </span>
          <button
            className="btn small ghost"
            type="button"
            aria-label="Cancel"
            onClick={() => {
              setReplyingTo(null);
              setEditing(null);
              setDraft("");
            }}
          >
            ×
          </button>
        </div>
      ) : null}
      <form className="composer" onSubmit={send}>
        <input ref={fileRef} type="file" hidden onChange={onPickFile} aria-hidden />
        <button
          className="btn small ghost"
          type="button"
          aria-label="Attach file"
          title="Attach a photo, video, or document"
          disabled={sendingMedia || !!editing}
          onClick={() => fileRef.current?.click()}
        >
          {sendingMedia ? "…" : "📎"}
        </button>
        <input
          className="input"
          value={draft}
          onChange={(e) => onDraftChange(e.target.value)}
          placeholder={editing ? "Edit message" : sendingMedia ? "Uploading…" : "Message"}
          aria-label="Type a message"
        />
        <button className="btn" type="submit">
          {editing ? "Save" : "Send"}
        </button>
      </form>
      {gallery ? <Gallery items={gallery.items} startKey={gallery.startKey} onClose={() => setGallery(null)} /> : null}
    </div>
  );
}

function isVisual(env: MediaEnvelope): boolean {
  const kind = classifyMedia(env.mime);
  return kind === "image" || kind === "video";
}

/** formatLastSeen renders a peer's last-seen timestamp as a short relative label. */
function formatLastSeen(ms: number): string {
  const diff = Date.now() - ms;
  if (diff < 60_000) return "just now";
  if (diff < 3_600_000) return `${Math.floor(diff / 60_000)}m ago`;
  if (diff < 86_400_000) return `${Math.floor(diff / 3_600_000)}h ago`;
  return new Date(ms).toLocaleDateString();
}

/** Callbacks a bubble's action menu invokes (FR-MSG-04..07). */
interface MessageActions {
  reply(m: ThreadMessage): void;
  edit(m: ThreadMessage): void;
  copy(m: ThreadMessage): void;
  deleteForEveryone(m: ThreadMessage): void;
  deleteForMe(m: ThreadMessage): void;
  togglePin(m: ThreadMessage): void;
  toggleStar(m: ThreadMessage): void;
}

const EDIT_WINDOW_MS = 15 * 60 * 1000; // FR-MSG-06
const DELETE_WINDOW_MS = 48 * 60 * 60 * 1000; // FR-MSG-05

/** snippetOf gives a short preview of a message for a reply quote. */
function snippetOf(m: ThreadMessage): string {
  if (m.deleted) return "deleted message";
  if (parseMediaMessage(m.body)) return "📎 Media";
  return parseTextMessage(m.body).text.slice(0, 80);
}

/** MessageBubble renders a text/media message with its quoted reply, edited/
 *  star/pin state, and a hover action menu (reply/copy/edit/delete/star/pin). */
function MessageBubble({
  message,
  actions,
  onOpen,
}: {
  message: ThreadMessage;
  actions: MessageActions;
  onOpen: (env: MediaEnvelope) => void;
}) {
  const [menu, setMenu] = useState(false);
  const media = message.deleted ? null : parseMediaMessage(message.body);
  const text = media || message.deleted ? null : parseTextMessage(message.body);
  const age = Date.now() - message.createdAt;
  const canEdit = message.mine && !message.deleted && !media && age < EDIT_WINDOW_MS;
  const canDeleteAll = message.mine && !message.deleted && age < DELETE_WINDOW_MS;

  const run = (fn: (m: ThreadMessage) => void) => () => {
    setMenu(false);
    fn(message);
  };

  return (
    <div className={message.mine ? "bubble mine" : "bubble theirs"} style={{ position: "relative" }}>
      {message.starred ? <span title="Starred" style={{ position: "absolute", top: -8, left: -6 }}>⭐</span> : null}
      {message.pinned ? <span title="Pinned" style={{ position: "absolute", top: -8, right: 14 }}>📌</span> : null}
      {!message.deleted ? (
        <button
          className="btn small ghost"
          aria-label="Message actions"
          onClick={() => setMenu((v) => !v)}
          style={{ position: "absolute", top: 2, right: 2, padding: "0 4px", lineHeight: 1, opacity: 0.6 }}
        >
          ⋯
        </button>
      ) : null}
      {menu ? (
        <div className="msg-menu" style={{ position: "absolute", top: 20, right: 2, zIndex: 5, background: "var(--panel, #fff)", border: "1px solid var(--border, #ccc)", borderRadius: 8, boxShadow: "0 4px 16px rgba(0,0,0,0.18)", display: "flex", flexDirection: "column", minWidth: 150 }}>
          <button className="menu-item" onClick={run(actions.reply)}>↩ Reply</button>
          {text ? <button className="menu-item" onClick={run(actions.copy)}>⧉ Copy</button> : null}
          <button className="menu-item" onClick={run(actions.toggleStar)}>{message.starred ? "☆ Unstar" : "⭐ Star"}</button>
          <button className="menu-item" onClick={run(actions.togglePin)}>{message.pinned ? "📌 Unpin" : "📌 Pin"}</button>
          {canEdit ? <button className="menu-item" onClick={run(actions.edit)}>✎ Edit</button> : null}
          {canDeleteAll ? <button className="menu-item danger" onClick={run(actions.deleteForEveryone)}>🗑 Delete for everyone</button> : null}
          <button className="menu-item danger" onClick={run(actions.deleteForMe)}>🗑 Delete for me</button>
        </div>
      ) : null}

      {text?.reply ? (
        <div className="reply-quote" style={{ borderLeft: "3px solid #128C7E", padding: "2px 8px", margin: "0 0 4px", background: "rgba(0,0,0,0.06)", borderRadius: 4, fontSize: "0.8rem", opacity: 0.85 }}>
          {text.reply.snippet}
        </div>
      ) : null}

      {media ? (
        <>
          <div className="bubble-media">
            {media.attachments.map((env) => (
              <MediaMessage key={env.objectKey} env={env} onOpen={onOpen} />
            ))}
          </div>
          {media.caption ? <span className="bubble-caption">{media.caption}</span> : null}
        </>
      ) : (
        <>
          <span>{message.deleted ? <em style={{ opacity: 0.7 }}>This message was deleted</em> : text?.text}</span>
          {message.edited && !message.deleted ? <span style={{ fontSize: "0.68rem", opacity: 0.6, marginLeft: 4 }}>(edited)</span> : null}
          {text?.linkPreview ? <LinkPreviewCard preview={text.linkPreview} /> : null}
        </>
      )}
      {message.mine ? <StatusTicks state={message.state} /> : null}
    </div>
  );
}

/** StatusTicks renders WhatsApp-style delivery state on my own bubbles:
 *  🕓 sending · ✓ sent · ✓✓ delivered · ✓✓ (blue) read. */
function StatusTicks({ state }: { state: string }) {
  if (state === "sending") return <span className="state" title="Sending" aria-label="Sending">🕓</span>;
  if (state === "delivered") return <span className="state" title="Delivered" aria-label="Delivered">✓✓</span>;
  if (state === "read")
    return (
      <span className="state" title="Read" aria-label="Read" style={{ color: "#34b7f1" }}>
        ✓✓
      </span>
    );
  return <span className="state" title="Sent" aria-label="Sent">✓</span>; // sent (default)
}

/** LinkPreviewCard renders a sender-generated preview (FR-MSG-08). The image is
 *  a self-contained data URI carried in the envelope — nothing is fetched here. */
function LinkPreviewCard({ preview }: { preview: LinkPreview }) {
  return (
    <a className="link-preview" href={preview.url} target="_blank" rel="noreferrer noopener">
      {preview.image ? <img className="link-preview-img" src={preview.image} alt="" /> : null}
      <span className="link-preview-body">
        {preview.siteName ? <span className="link-preview-site">{preview.siteName}</span> : null}
        <span className="link-preview-title">{preview.title}</span>
        {preview.description ? <span className="link-preview-desc">{preview.description}</span> : null}
      </span>
    </a>
  );
}

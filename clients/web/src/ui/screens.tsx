import {
  SNIPPET_CLOSE,
  SNIPPET_OPEN,
  extractHashtags,
  isValidPhone,
  type ChatSummary,
  type SearchHit,
  type ThreadMessage,
} from "@wa/client-core";
import {
  classifyMedia,
  parseContactCard,
  parseLiveLocation,
  parseLocation,
  parseMediaMessage,
  parsePoll,
  parseSticker,
  parseTextMessage,
  type ContactCardBody,
  type LinkPreview,
  type LiveLocationBody,
  type LocationBody,
  type MediaEnvelope,
  type PollBody,
  type QuotedRef,
} from "@wa/media-pipeline";
import { useCallback, useEffect, useRef, useState, type ChangeEvent, type CSSProperties, type FormEvent, type KeyboardEvent, type ReactNode } from "react";
import { registerWebPush } from "../push";
import { getTheme, setTheme, type ThemeChoice } from "../theme";
import { RichText } from "./RichText";
import { EmojiPicker, GifPicker, StickerPicker } from "./composerPickers";
import { messageOf } from "./errors";
import { useCall } from "../call/CallContext";
import { DownloadsPanel } from "./media/DownloadsPanel";
import { Gallery } from "./media/Gallery";
import { MediaMessage } from "./media/MediaMessage";
import { useServices } from "./ServicesContext";
import type { CallHistoryItem, ChannelInfo, ChannelInsights, ChannelPost, GroupInfo, GroupMember, Invite, LinkedDevice, MatchedContact, NotificationEntry, PollResults, StoryFeedItem, StoryViewer, UserRef } from "../services/appServices";

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

export function Profile({ onBack, onSettings }: { onBack: () => void; onSettings: () => void }) {
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
      <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
        <button type="button" className="btn small" onClick={onBack}>
          ‹ Back
        </button>
        <button type="button" className="btn small ghost" onClick={onSettings}>
          ⚙️ Settings & devices
        </button>
      </div>
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

/** CallHistory lists recent calls (FR-CALL-06, metadata only). Placing a call
 *  happens from a thread; this is the read-only log. */
export function CallHistory({ onBack }: { onBack: () => void }) {
  const { services } = useServices();
  const [calls, setCalls] = useState<CallHistoryItem[]>([]);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let alive = true;
    services
      .callHistory()
      .then((c) => {
        if (!alive) return;
        setCalls(c);
        for (const call of c) for (const p of call.participants) void services.loadUserProfile(p);
      })
      .catch(() => setError("Couldn't load call history."));
    return () => {
      alive = false;
    };
  }, [services]);

  return (
    <div className="pane">
      <div className="pane-head">
        <button className="btn small ghost" onClick={onBack}>
          ‹ Back
        </button>
        <span>Calls</span>
        <span />
      </div>
      <div className="messages">
        {error ? <p className="muted center">{error}</p> : null}
        {!error && calls.length === 0 ? <p className="muted center">No calls yet.</p> : null}
        {calls.map((c) => (
          <div
            key={c.id}
            style={{ display: "flex", alignItems: "center", gap: 8, padding: "10px 14px", borderBottom: "1px solid var(--border, #eee)" }}
          >
            <span aria-hidden>{c.kind === 2 ? "📹" : "📞"}</span>
            <span className="mono" style={{ flex: 1 }}>
              {(c.participants[0] ?? c.initiator).slice(0, 12)}
            </span>
            <span style={{ opacity: 0.7, fontSize: "0.78rem" }}>
              {c.outcome}
              {c.startedAt ? ` · ${new Date(c.startedAt).toLocaleString()}` : ""}
            </span>
          </div>
        ))}
      </div>
    </div>
  );
}

export function ChatList({
  onOpen,
  onNew,
  onSearch,
  onProfile,
  onContacts,
  onNewGroup,
  onCalls,
  onStatus,
  onChannels,
}: {
  onOpen: (id: string) => void;
  onNew: () => void;
  onSearch: () => void;
  onProfile: () => void;
  onContacts: () => void;
  onNewGroup: () => void;
  onCalls: () => void;
  onStatus: () => void;
  onChannels: () => void;
}) {
  const { services } = useServices();
  const [items, setItems] = useState<ChatSummary[]>([]);
  const [showArchived, setShowArchived] = useState(false);

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
            services.ensureConversationKind(conv.conversationId); // group vs direct → group names
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

  const renderRow = (c: ChatSummary): ReactNode => {
    const id = c.conversationId;
    const unread = services.unreadCount(id);
    const muted = services.isMuted(id);
    const fav = services.isFavorite(id);
    const archived = services.isArchived(id);
    const stop = (fn: () => void) => (e: { stopPropagation: () => void }) => {
      e.stopPropagation();
      fn();
    };
    return (
      <li
        key={id}
        className="row"
        role="button"
        tabIndex={0}
        onClick={() => onOpen(id)}
        onKeyDown={onActivate(() => onOpen(id))}
        style={{ display: "flex", alignItems: "center", gap: "0.5rem" }}
      >
        <div style={{ flex: 1, minWidth: 0 }}>
          <div className="row-title">
            {fav && <span title="Favorite" style={{ marginRight: 4 }}>⭐</span>}
            {services.groupNameOf(id) ? `👥 ${services.groupNameOf(id)}` : services.peerNameOf(id) || c.title}
            {muted && <span title="Muted" style={{ marginLeft: 6, opacity: 0.6 }}>🔇</span>}
          </div>
          <div className="row-sub">{c.lastPreview || "No messages yet"}</div>
        </div>
        {unread > 0 && (
          <span
            aria-label={`${unread} unread`}
            style={{
              background: muted ? "#9aa0a6" : "#25D366",
              color: "#fff",
              borderRadius: 999,
              minWidth: 20,
              height: 20,
              padding: "0 6px",
              fontSize: "0.72rem",
              fontWeight: 700,
              display: "inline-flex",
              alignItems: "center",
              justifyContent: "center",
            }}
          >
            {unread > 99 ? "99+" : unread}
          </span>
        )}
        <span className="row-actions">
          <button className={`icon-btn${fav ? " on" : ""}`} title={fav ? "Unfavorite" : "Favorite"} aria-label="Toggle favorite" onClick={stop(() => services.toggleFavorite(id))}>
            {fav ? "★" : "☆"}
          </button>
          <button className={`icon-btn${archived ? " on" : ""}`} title={archived ? "Unarchive" : "Archive"} aria-label="Toggle archive" onClick={stop(() => services.toggleArchive(id))}>
            🗄
          </button>
        </span>
      </li>
    );
  };

  const favorites = items.filter((c) => services.isFavorite(c.conversationId) && !services.isArchived(c.conversationId));
  const archived = items.filter((c) => services.isArchived(c.conversationId));
  const normal = items.filter((c) => !services.isFavorite(c.conversationId) && !services.isArchived(c.conversationId));

  return (
    <div className="pane">
      <div className="pane-head">
        <span>Chats</span>
        <span className="head-actions">
          <button className="btn small ghost" onClick={onProfile} aria-label="Your profile">
            👤 You
          </button>
          <button className="btn small ghost" onClick={onContacts} aria-label="Contacts">
            👥 Contacts
          </button>
          <button className="btn small ghost" onClick={onCalls} aria-label="Call history">
            📞 Calls
          </button>
          <button className="btn small ghost" onClick={onChannels} aria-label="Channels">
            📢 Channels
          </button>
          <button className="btn small ghost" onClick={onStatus} aria-label="Status updates">
            ⭕ Status
          </button>
          <button className="btn small ghost" onClick={onSearch} aria-label="Search messages">
            🔍 Search
          </button>
          <button className="btn small ghost" onClick={onNewGroup} aria-label="New group">
            ＋👥 Group
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
          {favorites.length > 0 && <li className="list-section">Favorites</li>}
          {favorites.map(renderRow)}
          {favorites.length > 0 && normal.length > 0 && <li className="list-section">Chats</li>}
          {normal.map(renderRow)}
          {archived.length > 0 && (
            <li className="list-section" role="button" tabIndex={0} onClick={() => setShowArchived((v) => !v)} onKeyDown={onActivate(() => setShowArchived((v) => !v))}>
              <span>🗄 Archived ({archived.length})</span>
              <span>{showArchived ? "▲" : "▼"}</span>
            </li>
          )}
          {showArchived && archived.map(renderRow)}
        </ul>
      )}
    </div>
  );
}

/** Search screen: debounced full-text search over the local decrypted store
 *  (ADR-005). A result opens its conversation. */
function sinceMs(when: "any" | "today" | "7d" | "30d"): number | undefined {
  const now = Date.now();
  switch (when) {
    case "today": {
      const d = new Date();
      d.setHours(0, 0, 0, 0);
      return d.getTime();
    }
    case "7d":
      return now - 7 * 86400_000;
    case "30d":
      return now - 30 * 86400_000;
    default:
      return undefined;
  }
}

export function Search({
  onOpen,
  onBack,
  conversationId,
  conversationTitle,
}: {
  onOpen: (conversationId: string, msgUuid?: string) => void;
  onBack: () => void;
  conversationId?: string;
  conversationTitle?: string;
}) {
  const { services } = useServices();
  const [query, setQuery] = useState("");
  const [from, setFrom] = useState<"any" | "me" | "others">("any");
  const [when, setWhen] = useState<"any" | "today" | "7d" | "30d">("any");
  const [type, setType] = useState<"all" | "media">("all");
  const [hits, setHits] = useState<SearchHit[]>([]);
  const [searched, setSearched] = useState(false);

  useEffect(() => {
    const raw = query.trim();
    const hashtags = extractHashtags(raw);
    const text = raw.replace(/#[\p{L}\p{N}_]+/gu, "").trim();
    const mediaOnly = type === "media";
    // Need a term, a hashtag, or the media filter — a date/sender filter alone
    // would dump the whole store, so we don't run it.
    if (!text && hashtags.length === 0 && !mediaOnly) {
      setHits([]);
      setSearched(false);
      return;
    }
    let alive = true;
    const handle = setTimeout(() => {
      services
        .search(text, {
          conversationId,
          fromMe: from === "any" ? undefined : from === "me",
          after: sinceMs(when),
          mediaOnly,
          hashtag: hashtags[0],
        })
        .then((r) => {
          if (!alive) return;
          setHits(r);
          setSearched(true);
        })
        .catch(() => {});
    }, 150); // debounce keystrokes
    return () => {
      alive = false;
      clearTimeout(handle);
    };
  }, [query, from, when, type, conversationId, services]);

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
          placeholder={conversationTitle ? `Search in ${conversationTitle}` : "Search messages (try #hashtag)"}
          aria-label="Search messages"
          autoFocus
        />
      </div>
      <div className="search-filters" style={{ display: "flex", gap: "0.4rem", flexWrap: "wrap", padding: "0.4rem 0.6rem", alignItems: "center" }}>
        {conversationTitle && <span className="muted" style={{ fontSize: "0.78rem" }}>in {conversationTitle} ·</span>}
        <label className="muted" style={{ fontSize: "0.78rem" }}>
          From{" "}
          <select value={from} onChange={(e) => setFrom(e.target.value as typeof from)}>
            <option value="any">anyone</option>
            <option value="me">me</option>
            <option value="others">others</option>
          </select>
        </label>
        <label className="muted" style={{ fontSize: "0.78rem" }}>
          When{" "}
          <select value={when} onChange={(e) => setWhen(e.target.value as typeof when)}>
            <option value="any">any time</option>
            <option value="today">today</option>
            <option value="7d">last 7 days</option>
            <option value="30d">last 30 days</option>
          </select>
        </label>
        <label className="muted" style={{ fontSize: "0.78rem" }}>
          Type{" "}
          <select value={type} onChange={(e) => setType(e.target.value as typeof type)}>
            <option value="all">all</option>
            <option value="media">files &amp; media</option>
          </select>
        </label>
      </div>
      {searched && hits.length === 0 ? <p className="muted center">No matches.</p> : null}
      <ul className="list">
        {hits.map((h) => (
          <li
            key={h.msgUuid}
            className="row"
            role="button"
            tabIndex={0}
            onClick={() => onOpen(h.conversationId, h.msgUuid)}
            onKeyDown={onActivate(() => onOpen(h.conversationId, h.msgUuid))}
          >
            <div className="row-title">
              {services.groupNameOf(h.conversationId) ? `👥 ${services.groupNameOf(h.conversationId)}` : services.peerNameOf(h.conversationId) || h.conversationTitle}
              <span className="muted" style={{ fontWeight: 400, fontSize: "0.72rem" }}> · {new Date(h.createdAt).toLocaleDateString()}</span>
            </div>
            <div className="row-sub">{highlightSnippet(h.snippet)}</div>
          </li>
        ))}
      </ul>
    </div>
  );
}

/** Contacts screen (T5.08): find registered users by username, star favorites,
 *  discover contacts by phone number, and share a personal invite link. */
export function Contacts({ onOpen, onBack }: { onOpen: (id: string) => void; onBack: () => void }) {
  const { services } = useServices();
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<UserRef[]>([]);
  const [favorites, setFavorites] = useState<UserRef[]>([]);
  const [favIds, setFavIds] = useState<Set<string>>(new Set());
  const [phones, setPhones] = useState("");
  const [matched, setMatched] = useState<MatchedContact[] | null>(null);
  const [invite, setInvite] = useState<Invite | null>(null);
  const [copied, setCopied] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  useEffect(() => {
    let alive = true;
    services
      .listFavorites()
      .then((f) => {
        if (!alive) return;
        setFavorites(f);
        setFavIds(new Set(f.map((x) => x.userId)));
      })
      .catch(() => {});
    return () => {
      alive = false;
    };
  }, [services]);

  useEffect(() => {
    const q = query.trim();
    if (q.length < 2) {
      setResults([]);
      return;
    }
    let alive = true;
    const h = setTimeout(() => {
      services
        .searchContacts(q)
        .then((r) => {
          if (alive) setResults(r);
        })
        .catch(() => {});
    }, 200); // debounce keystrokes (search is server rate-limited)
    return () => {
      alive = false;
      clearTimeout(h);
    };
  }, [query, services]);

  async function toggleFav(u: UserRef): Promise<void> {
    try {
      if (favIds.has(u.userId)) {
        await services.removeFavorite(u.userId);
        setFavIds((s) => {
          const n = new Set(s);
          n.delete(u.userId);
          return n;
        });
        setFavorites((f) => f.filter((x) => x.userId !== u.userId));
      } else {
        await services.addFavorite(u.userId);
        setFavIds((s) => new Set(s).add(u.userId));
        setFavorites((f) => (f.some((x) => x.userId === u.userId) ? f : [...f, u]));
      }
    } catch (err) {
      setError(messageOf(err));
    }
  }

  async function message(userId: string): Promise<void> {
    try {
      onOpen(await services.openDirectWithUser(userId));
    } catch (err) {
      setError(messageOf(err));
    }
  }

  async function checkPhones(): Promise<void> {
    setBusy(true);
    setError(null);
    try {
      setMatched(await services.syncPhones(phones.split(/[\n,]+/)));
    } catch (err) {
      setError(messageOf(err));
    } finally {
      setBusy(false);
    }
  }

  async function makeInvite(): Promise<void> {
    setError(null);
    try {
      setInvite(await services.createInvite());
    } catch (err) {
      setError(messageOf(err));
    }
  }

  async function copyInvite(): Promise<void> {
    if (!invite) return;
    try {
      await navigator.clipboard.writeText(invite.url);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      /* clipboard blocked; the URL stays visible for manual copy */
    }
  }

  const userRow = (u: UserRef): ReactNode => (
    <li key={u.userId} className="row" style={{ display: "flex", justifyContent: "space-between", alignItems: "center", gap: "0.5rem" }}>
      <span>@{u.username}</span>
      <span style={{ display: "flex", gap: "0.4rem" }}>
        <button
          className="btn small ghost"
          onClick={() => void toggleFav(u)}
          aria-label={favIds.has(u.userId) ? "Remove favorite" : "Add favorite"}
          title={favIds.has(u.userId) ? "Unfavorite" : "Favorite"}
        >
          {favIds.has(u.userId) ? "★" : "☆"}
        </button>
        <button className="btn small" onClick={() => void message(u.userId)}>
          Message
        </button>
      </span>
    </li>
  );

  return (
    <div className="card">
      <button type="button" className="btn small" onClick={onBack}>
        ‹ Back
      </button>
      <h1>Contacts</h1>

      <h2>Find people</h2>
      <input
        className="input"
        value={query}
        onChange={(e) => setQuery(e.target.value)}
        placeholder="Search by username…"
        aria-label="Search by username"
      />
      {query.trim().length >= 2 &&
        (results.length === 0 ? (
          <p className="muted">No users found.</p>
        ) : (
          <ul className="list">{results.map(userRow)}</ul>
        ))}

      <h2 style={{ marginTop: "1.5rem" }}>Favorites ({favorites.length})</h2>
      {favorites.length === 0 ? (
        <p className="muted">Star someone to keep them handy here.</p>
      ) : (
        <ul className="list">{favorites.map(userRow)}</ul>
      )}

      <h2 style={{ marginTop: "1.5rem" }}>Find by phone</h2>
      <p className="muted" style={{ fontSize: "0.8rem" }}>
        Paste numbers in international format (one per line). Only the peppered hash is stored.
      </p>
      <textarea
        className="input"
        rows={3}
        value={phones}
        onChange={(e) => setPhones(e.target.value)}
        placeholder={"+14155550123\n+919876543210"}
        aria-label="Phone numbers"
      />
      <button className="btn small" onClick={() => void checkPhones()} disabled={busy || phones.trim() === ""}>
        {busy ? "Checking…" : "Check numbers"}
      </button>
      {matched !== null &&
        (matched.length === 0 ? (
          <p className="muted">None of those numbers are on WhatsApp V2 yet.</p>
        ) : (
          <ul className="list">{matched.map((m) => userRow({ userId: m.userId, username: m.username }))}</ul>
        ))}

      <h2 style={{ marginTop: "1.5rem" }}>Invite a friend</h2>
      {invite ? (
        <div>
          <input className="input mono" readOnly value={invite.url} aria-label="Invite link" onFocus={(e) => e.currentTarget.select()} />
          <button className="btn small" onClick={() => void copyInvite()}>
            {copied ? "Copied ✓" : "Copy link"}
          </button>
        </div>
      ) : (
        <button className="btn small" onClick={() => void makeInvite()}>
          Create invite link
        </button>
      )}

      {error && (
        <p className="error" role="alert" style={{ marginTop: "1rem" }}>
          {error}
        </p>
      )}
    </div>
  );
}

/** CreateGroup screen (T5.09): name the group, pick members by username, create. */
export function CreateGroup({ onCreated, onBack }: { onCreated: (id: string) => void; onBack: () => void }) {
  const { services } = useServices();
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<UserRef[]>([]);
  const [picked, setPicked] = useState<UserRef[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const pickedIds = new Set(picked.map((p) => p.userId));

  useEffect(() => {
    const q = query.trim();
    if (q.length < 2) {
      setResults([]);
      return;
    }
    let alive = true;
    const h = setTimeout(() => {
      services
        .searchContacts(q)
        .then((r) => {
          if (alive) setResults(r);
        })
        .catch(() => {});
    }, 200);
    return () => {
      alive = false;
      clearTimeout(h);
    };
  }, [query, services]);

  async function create(): Promise<void> {
    if (name.trim() === "") {
      setError("Give the group a name.");
      return;
    }
    setBusy(true);
    setError(null);
    try {
      onCreated(await services.createGroup(name, description, picked.map((p) => p.userId)));
    } catch (err) {
      setError(messageOf(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="card">
      <button type="button" className="btn small" onClick={onBack}>
        ‹ Back
      </button>
      <h1>New group</h1>
      <label style={fieldWrap}>
        <span className="muted" style={fieldLabel}>
          Group name
        </span>
        <input className="input" value={name} onChange={(e) => setName(e.target.value)} placeholder="Weekend Trip" aria-label="Group name" maxLength={100} disabled={busy} />
      </label>
      <label style={fieldWrap}>
        <span className="muted" style={fieldLabel}>
          Description (optional)
        </span>
        <input className="input" value={description} onChange={(e) => setDescription(e.target.value)} placeholder="What's this group about?" aria-label="Group description" maxLength={500} disabled={busy} />
      </label>

      <h2 style={{ marginTop: "1rem" }}>Members ({picked.length})</h2>
      {picked.length > 0 && (
        <div style={{ display: "flex", flexWrap: "wrap", gap: "0.4rem", marginBottom: "0.6rem" }}>
          {picked.map((p) => (
            <button key={p.userId} className="btn small ghost" onClick={() => setPicked((s) => s.filter((x) => x.userId !== p.userId))} title="Remove">
              @{p.username} ✕
            </button>
          ))}
        </div>
      )}
      <input className="input" value={query} onChange={(e) => setQuery(e.target.value)} placeholder="Add members by username…" aria-label="Add members" />
      {query.trim().length >= 2 && (
        <ul className="list">
          {results
            .filter((r) => !pickedIds.has(r.userId))
            .map((r) => (
              <li key={r.userId} className="row" style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
                <span>@{r.username}</span>
                <button
                  className="btn small"
                  onClick={() => {
                    setPicked((s) => [...s, r]);
                    setQuery("");
                  }}
                >
                  Add
                </button>
              </li>
            ))}
        </ul>
      )}

      {error && (
        <p className="error" role="alert" style={{ marginTop: "0.8rem" }}>
          {error}
        </p>
      )}
      <button className="btn" onClick={() => void create()} disabled={busy} style={{ marginTop: "0.8rem" }}>
        {busy ? "Creating…" : "Create group"}
      </button>
    </div>
  );
}

const ROLE_RANK: Record<string, number> = { owner: 2, admin: 1, member: 0 };

/** GroupInfoScreen (T5.09): members + roles, add/remove/leave, promote/demote,
 *  settings (announcements / who-can-post), invite link, delete. */
export function GroupInfoScreen({ conversationId, onBack, onLeft }: { conversationId: string; onBack: () => void; onLeft: () => void }) {
  const { services } = useServices();
  const [group, setGroup] = useState<GroupInfo | null>(() => services.groupOf(conversationId) ?? null);
  const [members, setMembers] = useState<GroupMember[]>([]);
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<UserRef[]>([]);
  const [invite, setInvite] = useState<string | null>(null);
  const [copied, setCopied] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const reload = useCallback(() => {
    void services.loadGroup(conversationId).then((g) => setGroup(g));
    void services.listGroupMembers(conversationId).then(setMembers);
  }, [services, conversationId]);

  useEffect(() => {
    reload();
    const unsub = services.onChange(() => setMembers((m) => [...m])); // re-render as names resolve
    return unsub;
  }, [reload, services]);

  useEffect(() => {
    const q = query.trim();
    if (q.length < 2) {
      setResults([]);
      return;
    }
    let alive = true;
    const h = setTimeout(() => {
      services
        .searchContacts(q)
        .then((r) => {
          if (alive) setResults(r);
        })
        .catch(() => {});
    }, 200);
    return () => {
      alive = false;
      clearTimeout(h);
    };
  }, [query, services]);

  if (!group) return <p className="muted center">Loading…</p>;
  const myRole = group.myRole;
  const iAmOwner = myRole === "owner";
  const iCanManage = iAmOwner || myRole === "admin";
  const memberIds = new Set(members.map((m) => m.userId));

  async function guard(fn: () => Promise<void>): Promise<void> {
    setError(null);
    try {
      await fn();
      reload();
    } catch (err) {
      setError(messageOf(err));
    }
  }

  async function saveSettings(patch: Partial<GroupInfo["settings"]>): Promise<void> {
    if (!group) return;
    await guard(() => services.setGroupSettings(conversationId, { ...group.settings, ...patch }));
  }

  async function makeInvite(): Promise<void> {
    setError(null);
    try {
      setInvite((await services.createGroupInvite(conversationId)).url);
    } catch (err) {
      setError(messageOf(err));
    }
  }

  return (
    <div className="card">
      <button type="button" className="btn small" onClick={onBack}>
        ‹ Back to chat
      </button>
      <h1>{group.name}</h1>
      {group.description && <p className="muted">{group.description}</p>}
      <p className="muted" style={{ fontSize: "0.8rem" }}>
        You are {myRole === "owner" ? "the owner" : `a${myRole === "admin" ? "n admin" : " member"}`}.
      </p>

      {iCanManage && (
        <>
          <h2 style={{ marginTop: "1rem" }}>Settings</h2>
          <label style={{ display: "flex", alignItems: "center", gap: "0.5rem", marginBottom: "0.6rem" }}>
            <input type="checkbox" checked={group.settings.announcements} onChange={(e) => void saveSettings({ announcements: e.target.checked })} />
            <span>📢 Announcements only (admins post)</span>
          </label>
          <label style={fieldWrap}>
            <span className="muted" style={fieldLabel}>
              Who can post
            </span>
            <select className="input" value={group.settings.who_can_post} onChange={(e) => void saveSettings({ who_can_post: e.target.value })}>
              <option value="all">Everyone</option>
              <option value="admins">Admins only</option>
            </select>
          </label>
          <label style={fieldWrap}>
            <span className="muted" style={fieldLabel}>
              Who can edit group info
            </span>
            <select className="input" value={group.settings.who_can_edit_info} onChange={(e) => void saveSettings({ who_can_edit_info: e.target.value })}>
              <option value="all">Everyone</option>
              <option value="admins">Admins only</option>
            </select>
          </label>
        </>
      )}

      <h2 style={{ marginTop: "1rem" }}>Members ({members.length})</h2>
      <ul className="list">
        {[...members]
          .sort((a, b) => (ROLE_RANK[b.role] ?? 0) - (ROLE_RANK[a.role] ?? 0))
          .map((m) => (
            <li key={m.userId} className="row" style={{ display: "flex", justifyContent: "space-between", alignItems: "center", gap: "0.5rem" }}>
              <span>
                {services.nameForUser(m.userId)}{" "}
                {m.role !== "member" && <span className="muted" style={{ fontSize: "0.75rem" }}>· {m.role}</span>}
              </span>
              <span style={{ display: "flex", gap: "0.35rem" }}>
                {iAmOwner && m.role === "member" && (
                  <button className="btn small ghost" onClick={() => void guard(() => services.setGroupRole(conversationId, m.userId, 1))} title="Make admin">
                    ↑ admin
                  </button>
                )}
                {iAmOwner && m.role === "admin" && (
                  <button className="btn small ghost" onClick={() => void guard(() => services.setGroupRole(conversationId, m.userId, 0))} title="Demote to member">
                    ↓ member
                  </button>
                )}
                {iCanManage && m.role !== "owner" && (
                  <button className="btn small ghost" onClick={() => void guard(() => services.removeGroupMember(conversationId, m.userId))} title="Remove">
                    Remove
                  </button>
                )}
              </span>
            </li>
          ))}
      </ul>

      {iCanManage && (
        <>
          <h2 style={{ marginTop: "1rem" }}>Add members</h2>
          <input className="input" value={query} onChange={(e) => setQuery(e.target.value)} placeholder="Search by username…" aria-label="Add members" />
          {query.trim().length >= 2 && (
            <ul className="list">
              {results
                .filter((r) => !memberIds.has(r.userId))
                .map((r) => (
                  <li key={r.userId} className="row" style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
                    <span>@{r.username}</span>
                    <button
                      className="btn small"
                      onClick={() =>
                        void guard(async () => {
                          await services.addGroupMembers(conversationId, [r.userId]);
                          setQuery("");
                        })
                      }
                    >
                      Add
                    </button>
                  </li>
                ))}
            </ul>
          )}

          <h2 style={{ marginTop: "1rem" }}>Invite link</h2>
          {invite ? (
            <div>
              <input
                className="input mono"
                readOnly
                value={invite}
                aria-label="Group invite link"
                onFocus={(e) => e.currentTarget.select()}
              />
              <button
                className="btn small"
                onClick={() => {
                  void navigator.clipboard?.writeText(invite).then(() => {
                    setCopied(true);
                    setTimeout(() => setCopied(false), 1500);
                  });
                }}
              >
                {copied ? "Copied ✓" : "Copy link"}
              </button>
            </div>
          ) : (
            <button className="btn small" onClick={() => void makeInvite()}>
              Create invite link
            </button>
          )}
        </>
      )}

      {error && (
        <p className="error" role="alert" style={{ marginTop: "1rem" }}>
          {error}
        </p>
      )}

      <div style={{ display: "flex", gap: "0.5rem", marginTop: "1.5rem" }}>
        <button
          className="btn small ghost"
          onClick={() => {
            if (window.confirm("Leave this group?")) void guard(async () => (await services.leaveGroup(conversationId), onLeft()));
          }}
        >
          Leave group
        </button>
        {iAmOwner && (
          <button
            className="btn small ghost"
            style={{ color: "var(--danger, #c0392b)" }}
            onClick={() => {
              if (window.confirm("Delete this group for everyone? This can't be undone.")) void guard(async () => (await services.deleteGroup(conversationId), onLeft()));
            }}
          >
            Delete group
          </button>
        )}
      </div>
    </div>
  );
}

const STORY_BG = ["#128C7E", "#075E54", "#e5484d", "#7c3aed", "#f59e0b", "#111827"];

type StatusMode = { m: "list" } | { m: "compose" } | { m: "view"; author: string };

/** Status screen (T5.11): the story ring — my status + contacts' active stories,
 *  the composer, and the tap-through viewer. Content is E2EE; this dev build
 *  caches the author's own payload locally (cross-device rides STORY_KEY). */
export function Status({ onBack }: { onBack: () => void }) {
  const { services } = useServices();
  const [feed, setFeed] = useState<StoryFeedItem[]>([]);
  const [mode, setMode] = useState<StatusMode>({ m: "list" });
  const me = services.myUserId();

  useEffect(() => {
    let alive = true;
    const load = (): void => {
      services
        .storyFeed()
        .then((f) => {
          if (!alive) return;
          setFeed(f);
          for (const s of f) if (s.author !== me) void services.loadUserProfile(s.author);
        })
        .catch(() => {});
    };
    load();
    const unsub = services.onChange(load);
    const h = setInterval(load, 15000);
    return () => {
      alive = false;
      unsub();
      clearInterval(h);
    };
  }, [services, me]);

  const byAuthor = new Map<string, StoryFeedItem[]>();
  for (const s of feed) {
    const arr = byAuthor.get(s.author) ?? [];
    arr.push(s);
    byAuthor.set(s.author, arr);
  }
  for (const arr of byAuthor.values()) arr.sort((a, b) => a.expiresAtMs - b.expiresAtMs);
  const mine = byAuthor.get(me) ?? [];
  const others = [...byAuthor.entries()].filter(([a]) => a !== me);

  if (mode.m === "compose") return <StatusComposer onClose={() => setMode({ m: "list" })} />;
  if (mode.m === "view") {
    return (
      <StoryViewerOverlay
        author={mode.author}
        stories={byAuthor.get(mode.author) ?? []}
        isMine={mode.author === me}
        onClose={() => setMode({ m: "list" })}
      />
    );
  }

  return (
    <div className="card">
      <button type="button" className="btn small" onClick={onBack}>
        ‹ Back
      </button>
      <h1>Status</h1>
      <ul className="list">
        <li
          className="row"
          role="button"
          tabIndex={0}
          onClick={() => (mine.length ? setMode({ m: "view", author: me }) : setMode({ m: "compose" }))}
          onKeyDown={onActivate(() => (mine.length ? setMode({ m: "view", author: me }) : setMode({ m: "compose" })))}
        >
          <div className="row-title">➕ My status</div>
          <div className="row-sub">
            {mine.length ? `${mine.length} update${mine.length > 1 ? "s" : ""} · tap to view` : "Tap to add a status update"}
          </div>
        </li>
      </ul>
      <button className="btn small" onClick={() => setMode({ m: "compose" })}>
        ＋ Add status
      </button>

      <h2 style={{ marginTop: "1.5rem" }}>Recent updates</h2>
      {others.length === 0 ? (
        <p className="muted">No status updates from your contacts yet.</p>
      ) : (
        <ul className="list">
          {others.map(([author, stories]) => (
            <li
              key={author}
              className="row"
              role="button"
              tabIndex={0}
              onClick={() => setMode({ m: "view", author })}
              onKeyDown={onActivate(() => setMode({ m: "view", author }))}
            >
              <div className="row-title">🟢 {services.nameForUser(author)}</div>
              <div className="row-sub">
                {stories.length} update{stories.length > 1 ? "s" : ""}
              </div>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

function StatusComposer({ onClose }: { onClose: () => void }) {
  const { services } = useServices();
  const [tab, setTab] = useState<"text" | "photo">("text");
  const [text, setText] = useState("");
  const [bg, setBg] = useState(STORY_BG[0]);
  const [dataUrl, setDataUrl] = useState<string | null>(null);
  const [fileBytes, setFileBytes] = useState<{ bytes: Uint8Array; mime: string } | null>(null);
  const [audienceMode, setAudienceMode] = useState<"contacts" | "specific">("contacts");
  const [query, setQuery] = useState("");
  const [results, setResults] = useState<UserRef[]>([]);
  const [picked, setPicked] = useState<UserRef[]>([]);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const pickedIds = new Set(picked.map((p) => p.userId));

  useEffect(() => {
    if (audienceMode !== "specific" || query.trim().length < 2) {
      setResults([]);
      return;
    }
    let alive = true;
    const h = setTimeout(() => {
      services
        .searchContacts(query)
        .then((r) => {
          if (alive) setResults(r);
        })
        .catch(() => {});
    }, 200);
    return () => {
      alive = false;
      clearTimeout(h);
    };
  }, [query, audienceMode, services]);

  async function onPickPhoto(e: ChangeEvent<HTMLInputElement>): Promise<void> {
    const file = e.target.files?.[0];
    e.target.value = "";
    if (!file) return;
    if (file.size > 2 * 1024 * 1024) {
      setError("Image too large (max 2 MB).");
      return;
    }
    setError(null);
    setFileBytes({ bytes: new Uint8Array(await file.arrayBuffer()), mime: file.type || "image/jpeg" });
    const reader = new FileReader();
    reader.onload = () => setDataUrl(reader.result as string);
    reader.readAsDataURL(file);
    setTab("photo");
  }

  async function post(): Promise<void> {
    setBusy(true);
    setError(null);
    try {
      const audience = audienceMode === "specific" ? picked.map((p) => p.userId) : null;
      if (audienceMode === "specific" && (audience?.length ?? 0) === 0) {
        throw new Error("Pick at least one person, or share with your contacts.");
      }
      if (tab === "text") {
        if (!text.trim()) throw new Error("Write something first.");
        await services.postStory("text", null, audience, { kind: "text", text: text.trim(), bg });
      } else {
        if (!fileBytes || !dataUrl) throw new Error("Choose a photo first.");
        const mediaRef = await services.uploadStoryMedia(fileBytes.bytes, fileBytes.mime);
        await services.postStory("image", mediaRef, audience, { kind: "image", dataUrl });
      }
      onClose();
    } catch (err) {
      setError(messageOf(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="card">
      <button type="button" className="btn small" onClick={onClose}>
        ‹ Cancel
      </button>
      <h1>New status</h1>
      <div style={{ display: "flex", gap: "0.5rem", marginBottom: "0.8rem" }}>
        <button className={tab === "text" ? "btn small" : "btn small ghost"} onClick={() => setTab("text")}>
          Text
        </button>
        <button className={tab === "photo" ? "btn small" : "btn small ghost"} onClick={() => setTab("photo")}>
          Photo
        </button>
      </div>

      {tab === "text" ? (
        <>
          <div
            style={{
              background: bg,
              color: "#fff",
              borderRadius: 12,
              minHeight: 160,
              display: "flex",
              alignItems: "center",
              justifyContent: "center",
              padding: "1rem",
              marginBottom: "0.6rem",
              textAlign: "center",
              fontSize: "1.1rem",
              wordBreak: "break-word",
            }}
          >
            {text || "Type your status…"}
          </div>
          <textarea className="input" rows={2} value={text} onChange={(e) => setText(e.target.value)} placeholder="Type your status…" aria-label="Status text" maxLength={280} />
          <div style={{ display: "flex", gap: "0.4rem", margin: "0.5rem 0" }}>
            {STORY_BG.map((c) => (
              <button
                key={c}
                aria-label={`Background ${c}`}
                onClick={() => setBg(c)}
                style={{ width: 28, height: 28, borderRadius: "50%", background: c, border: c === bg ? "3px solid #333" : "2px solid #ccc", cursor: "pointer" }}
              />
            ))}
          </div>
        </>
      ) : (
        <>
          {dataUrl ? (
            <img src={dataUrl} alt="Status preview" style={{ maxWidth: "100%", borderRadius: 12, marginBottom: "0.6rem" }} />
          ) : (
            <p className="muted">Choose a photo to share as your status.</p>
          )}
          <label className="btn small ghost" style={{ display: "inline-block", cursor: "pointer" }}>
            {dataUrl ? "Change photo" : "Choose photo"}
            <input type="file" accept="image/*" hidden onChange={(e) => void onPickPhoto(e)} />
          </label>
        </>
      )}

      <h2 style={{ marginTop: "1rem" }}>Who can see this</h2>
      <label style={{ display: "flex", gap: "0.5rem", alignItems: "center" }}>
        <input type="radio" checked={audienceMode === "contacts"} onChange={() => setAudienceMode("contacts")} /> My contacts
      </label>
      <label style={{ display: "flex", gap: "0.5rem", alignItems: "center", marginBottom: "0.4rem" }}>
        <input type="radio" checked={audienceMode === "specific"} onChange={() => setAudienceMode("specific")} /> Only share with…
      </label>
      {audienceMode === "specific" && (
        <>
          {picked.length > 0 && (
            <div style={{ display: "flex", flexWrap: "wrap", gap: "0.4rem", marginBottom: "0.5rem" }}>
              {picked.map((p) => (
                <button key={p.userId} className="btn small ghost" onClick={() => setPicked((s) => s.filter((x) => x.userId !== p.userId))}>
                  @{p.username} ✕
                </button>
              ))}
            </div>
          )}
          <input className="input" value={query} onChange={(e) => setQuery(e.target.value)} placeholder="Search by username…" aria-label="Audience search" />
          {query.trim().length >= 2 && (
            <ul className="list">
              {results
                .filter((r) => !pickedIds.has(r.userId))
                .map((r) => (
                  <li key={r.userId} className="row" style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
                    <span>@{r.username}</span>
                    <button className="btn small" onClick={() => { setPicked((s) => [...s, r]); setQuery(""); }}>
                      Add
                    </button>
                  </li>
                ))}
            </ul>
          )}
        </>
      )}

      {error && (
        <p className="error" role="alert" style={{ marginTop: "0.6rem" }}>
          {error}
        </p>
      )}
      <button className="btn" onClick={() => void post()} disabled={busy} style={{ marginTop: "0.8rem" }}>
        {busy ? "Posting…" : "Post status"}
      </button>
    </div>
  );
}

function StoryViewerOverlay({
  author,
  stories,
  isMine,
  onClose,
}: {
  author: string;
  stories: StoryFeedItem[];
  isMine: boolean;
  onClose: () => void;
}) {
  const { services } = useServices();
  const [idx, setIdx] = useState(0);
  const [viewers, setViewers] = useState<StoryViewer[]>([]);
  const [showViewers, setShowViewers] = useState(false);
  const story = stories[idx];

  useEffect(() => {
    if (!story) return;
    if (!isMine) void services.viewStory(story.storyId); // don't self-count the author's own view
    if (isMine) void services.storyViewers(story.storyId).then(setViewers).catch(() => {});
    if (showViewers) return; // pause auto-advance while the viewer list is open
    const t = setTimeout(() => {
      if (idx < stories.length - 1) setIdx(idx + 1);
      else onClose();
    }, 5000);
    return () => clearTimeout(t);
  }, [idx, story, stories.length, isMine, showViewers, services, onClose]);

  if (!story) {
    onClose();
    return null;
  }
  const content = services.loadStoryContent(story.storyId);

  return (
    <div className="story-viewer">
      <div className="story-progress">
        {stories.map((s, i) => (
          <span key={s.storyId} className={i < idx ? "done" : i === idx ? "active" : ""} />
        ))}
      </div>
      <div className="story-head">
        <span>{isMine ? "My status" : services.nameForUser(author)}</span>
        {isMine && (
          <button className="btn small ghost" onClick={() => void services.deleteStory(story.storyId).then(onClose)} aria-label="Delete status">
            🗑
          </button>
        )}
        <button className="btn small ghost" onClick={onClose} aria-label="Close">
          ✕
        </button>
      </div>

      <div className="story-body" style={content?.kind === "text" ? { background: content.bg } : undefined}>
        {content?.kind === "text" ? (
          <p className="story-text">{content.text}</p>
        ) : content?.kind === "image" ? (
          <img src={content.dataUrl} alt="Status" className="story-image" />
        ) : (
          <div className="story-locked">
            🔒
            <div>Encrypted status</div>
            <span className="muted">content rides the STORY_KEY channel (not wired in this dev build)</span>
          </div>
        )}
      </div>

      <button className="story-tap left" aria-label="Previous" onClick={() => (idx > 0 ? setIdx(idx - 1) : undefined)} />
      <button className="story-tap right" aria-label="Next" onClick={() => (idx < stories.length - 1 ? setIdx(idx + 1) : onClose())} />

      {isMine && (
        <button className="story-viewers-btn" onClick={() => setShowViewers((v) => !v)}>
          👁 Viewed by {viewers.length}
        </button>
      )}
      {isMine && showViewers && (
        <div className="story-viewers-panel">
          <h3>Viewed by {viewers.length}</h3>
          {viewers.length === 0 ? (
            <p className="muted">No views yet.</p>
          ) : (
            <ul className="list">
              {viewers.map((v) => (
                <li key={v.userId} className="row">
                  {services.nameForUser(v.userId)}
                </li>
              ))}
            </ul>
          )}
          <button className="btn small" onClick={() => setShowViewers(false)}>
            Close
          </button>
        </div>
      )}
    </div>
  );
}

/** Settings screen (T5.12): linked-device management (list/rename/revoke),
 *  notification opt-in, and the account/backup surfaces. Device linking's QR
 *  scan + signed device-list cert is the crypto-wrapper deviceList seam. */
export function Settings({ onBack, onSignedOut }: { onBack: () => void; onSignedOut: () => void }) {
  const { services } = useServices();
  const [devices, setDevices] = useState<LinkedDevice[]>([]);
  const [editing, setEditing] = useState<string | null>(null);
  const [editName, setEditName] = useState("");
  const [pushOn, setPushOn] = useState<boolean>(() => {
    try {
      return localStorage.getItem("wa.push") === "on";
    } catch {
      return false;
    }
  });
  const [globalMute, setGlobalMute] = useState<boolean>(() => services.isGlobalMute());
  const [theme, setThemeState] = useState<ThemeChoice>(() => getTheme());
  const [notifs, setNotifs] = useState<NotificationEntry[]>([]);
  const [error, setError] = useState<string | null>(null);
  // T6.04 saved-reply + auto-reply local editor state
  const [tplTitle, setTplTitle] = useState("");
  const [tplText, setTplText] = useState("");
  const [autoReply, setAutoReplyState] = useState(() => services.getAutoReply());
  const [, setTick] = useState(0); // re-render on template/scheduled changes
  const myId = services.myDeviceId();

  const load = useCallback(() => {
    void services.listDevices().then(setDevices).catch(() => {});
    setNotifs(services.notificationHistory());
  }, [services]);
  useEffect(() => {
    load();
    return services.onChange(() => setTick((n) => n + 1)); // refresh scheduled/templates lists
  }, [load, services]);

  async function saveName(id: string): Promise<void> {
    setError(null);
    try {
      await services.renameDevice(id, editName.trim());
      setEditing(null);
      load();
    } catch (err) {
      setError(messageOf(err));
    }
  }

  async function revoke(id: string): Promise<void> {
    const isMe = id === myId;
    if (!window.confirm(isMe ? "Sign out this device?" : "Revoke this device? It will be signed out.")) return;
    setError(null);
    try {
      await services.revokeDevice(id);
      if (isMe) {
        await services.logout();
        onSignedOut();
      } else {
        load();
      }
    } catch (err) {
      setError(messageOf(err));
    }
  }

  async function togglePush(): Promise<void> {
    setError(null);
    if (pushOn) {
      try {
        localStorage.setItem("wa.push", "off");
      } catch {
        /* ignore */
      }
      setPushOn(false);
      return;
    }
    try {
      const sub = await registerWebPush();
      if (sub) {
        localStorage.setItem("wa.push", "on");
        setPushOn(true);
      } else {
        setError("Notifications were blocked or aren't supported in this browser.");
      }
    } catch {
      setError("Couldn't enable notifications.");
    }
  }

  return (
    <div className="card">
      <button type="button" className="btn small" onClick={onBack}>
        ‹ Back
      </button>
      <h1>Settings</h1>

      <h2>Appearance</h2>
      <label className="row-flags" style={{ display: "flex", justifyContent: "space-between", alignItems: "center" }}>
        <span>Theme</span>
        <span className="segmented">
          {(["system", "light", "dark"] as ThemeChoice[]).map((t) => (
            <button
              key={t}
              type="button"
              className={theme === t ? "active" : ""}
              onClick={() => {
                setTheme(t);
                setThemeState(t);
              }}
            >
              {t === "system" ? "System" : t === "light" ? "Light" : "Dark"}
            </button>
          ))}
        </span>
      </label>

      <h2 style={{ marginTop: "1.5rem" }}>Linked devices ({devices.length})</h2>
      <p className="muted" style={{ fontSize: "0.8rem" }}>
        Devices signed in to your account. Revoke any you don't recognise.
      </p>
      <ul className="list">
        {devices.map((d) => (
          <li key={d.id} className="row" style={{ display: "flex", justifyContent: "space-between", alignItems: "center", gap: "0.5rem" }}>
            {editing === d.id ? (
              <>
                <input className="input" value={editName} onChange={(e) => setEditName(e.target.value)} aria-label="Device name" maxLength={60} style={{ flex: 1 }} />
                <button className="btn small" onClick={() => void saveName(d.id)}>
                  Save
                </button>
                <button className="btn small ghost" onClick={() => setEditing(null)}>
                  ✕
                </button>
              </>
            ) : (
              <>
                <span style={{ flex: 1 }}>
                  {d.name || d.platform || "Device"}
                  {d.id === myId && <span className="muted" style={{ fontSize: "0.75rem" }}> · this device</span>}
                  {d.isPrimary && <span className="muted" style={{ fontSize: "0.75rem" }}> · primary</span>}
                  <br />
                  <span className="muted" style={{ fontSize: "0.72rem" }}>
                    {d.platform}
                    {d.lastActiveMs ? ` · active ${formatLastSeen(d.lastActiveMs)}` : ""}
                  </span>
                </span>
                <button
                  className="btn small ghost"
                  onClick={() => {
                    setEditing(d.id);
                    setEditName(d.name);
                  }}
                >
                  Rename
                </button>
                <button className="btn small ghost" onClick={() => void revoke(d.id)}>
                  {d.id === myId ? "Sign out" : "Revoke"}
                </button>
              </>
            )}
          </li>
        ))}
      </ul>

      <h2 style={{ marginTop: "1.5rem" }}>Link a device</h2>
      <p className="muted" style={{ fontSize: "0.85rem" }}>
        On the new device, open WhatsApp V2 → Link a device, then scan its QR here. The primary device signs
        the new device's key into your <span className="mono">signed device list</span> so all your devices trust
        it. QR scanning + the device-list signature are wired through <span className="mono">@wa/crypto-wrapper</span>{" "}
        (deviceList) — the on-device linking seam.
      </p>

      <h2 style={{ marginTop: "1.5rem" }}>Notifications</h2>
      <label style={{ display: "flex", alignItems: "center", gap: "0.6rem" }}>
        <input type="checkbox" checked={pushOn} onChange={() => void togglePush()} />
        <span>Push notifications on this device</span>
      </label>
      <label style={{ display: "flex", alignItems: "center", gap: "0.6rem", marginTop: "0.4rem" }}>
        <input type="checkbox" checked={globalMute} onChange={(e) => { services.setGlobalMute(e.target.checked); setGlobalMute(e.target.checked); }} />
        <span>Mute all chats (no in-app alerts)</span>
      </label>

      <h3 style={{ marginTop: "1rem" }}>Recent notifications</h3>
      {notifs.length === 0 ? (
        <p className="muted" style={{ fontSize: "0.85rem" }}>Nothing recent.</p>
      ) : (
        <>
          <ul className="list">
            {notifs.slice(0, 20).map((n) => (
              <li key={n.id} className="row">
                <div className="row-title">{n.title}</div>
                <div className="row-sub">
                  {n.preview} · {formatLastSeen(n.ts)}
                </div>
              </li>
            ))}
          </ul>
          <button className="btn small ghost" onClick={() => { services.clearNotifications(); setNotifs([]); }}>
            Clear history
          </button>
        </>
      )}

      <h2 style={{ marginTop: "1.5rem" }}>Saved replies</h2>
      <p className="muted" style={{ fontSize: "0.85rem" }}>Reusable messages you can insert from a chat's 📋 button.</p>
      <div style={{ display: "flex", gap: 6, marginBottom: 8 }}>
        <input className="input" placeholder="Title" value={tplTitle} style={{ maxWidth: 140 }} onChange={(e) => setTplTitle(e.target.value)} />
        <input className="input" placeholder="Message text" value={tplText} onChange={(e) => setTplText(e.target.value)} />
        <button
          className="btn small"
          disabled={tplText.trim() === ""}
          onClick={() => {
            services.addTemplate(tplTitle.trim() || tplText.trim().slice(0, 20), tplText.trim());
            setTplTitle("");
            setTplText("");
          }}
        >
          Add
        </button>
      </div>
      <ul className="list">
        {services.listTemplates().map((t) => (
          <li key={t.id} className="row" style={{ display: "flex", alignItems: "center", gap: 8 }}>
            <div style={{ flex: 1, minWidth: 0 }}>
              <div className="row-title">{t.title}</div>
              <div className="row-sub">{t.text}</div>
            </div>
            <button className="btn small ghost" aria-label="Delete saved reply" onClick={() => services.removeTemplate(t.id)}>
              🗑
            </button>
          </li>
        ))}
        {services.listTemplates().length === 0 ? <li className="row muted">No saved replies yet.</li> : null}
      </ul>

      <h2 style={{ marginTop: "1.5rem" }}>Auto-reply (away)</h2>
      <p className="muted" style={{ fontSize: "0.85rem" }}>
        When on, incoming messages get one automatic reply per chat per hour (skips chats you're viewing).
      </p>
      <label style={{ display: "flex", gap: 8, alignItems: "center", marginBottom: 8 }}>
        <input
          type="checkbox"
          checked={autoReply.enabled}
          onChange={(e) => {
            const next = { ...autoReply, enabled: e.target.checked };
            setAutoReplyState(next);
            services.setAutoReply(next.enabled, next.text);
          }}
        />
        Enable auto-reply
      </label>
      <input
        className="input"
        placeholder="Away message (e.g. I'm away, back soon)"
        value={autoReply.text}
        onChange={(e) => {
          const next = { ...autoReply, text: e.target.value };
          setAutoReplyState(next);
          services.setAutoReply(next.enabled, next.text);
        }}
      />

      <h2 style={{ marginTop: "1.5rem" }}>Scheduled messages</h2>
      {services.scheduledMessages().length === 0 ? (
        <p className="muted" style={{ fontSize: "0.85rem" }}>Nothing scheduled. Type a message in a chat and tap 🕒 to schedule it.</p>
      ) : (
        <ul className="list">
          {services.scheduledMessages().map((m) => (
            <li key={m.id} className="row" style={{ display: "flex", alignItems: "center", gap: 8 }}>
              <div style={{ flex: 1, minWidth: 0 }}>
                <div className="row-title">{m.text.slice(0, 40)}</div>
                <div className="row-sub">{new Date(m.sendAtMs).toLocaleString()}</div>
              </div>
              <button className="btn small ghost" aria-label="Cancel scheduled message" onClick={() => services.cancelScheduled(m.id)}>
                Cancel
              </button>
            </li>
          ))}
        </ul>
      )}

      <h2 style={{ marginTop: "1.5rem" }}>Chat backup</h2>
      <p className="muted" style={{ fontSize: "0.85rem" }}>
        End-to-end encrypted backups upload to your own storage, keyed by a password only you hold (Argon2id).
        Create/restore is wired server-side (<span className="mono">/v1/backups</span>) — the client archive +
        key-derivation UI is the next step.
      </p>

      <h2 style={{ marginTop: "1.5rem" }}>Account</h2>
      <p className="muted" style={{ fontSize: "0.85rem" }}>
        Export your data or delete your account — the account-lifecycle endpoints aren't exposed yet, so these
        remain on the roadmap.
      </p>

      {error && (
        <p className="error" role="alert" style={{ marginTop: "1rem" }}>
          {error}
        </p>
      )}
    </div>
  );
}

const CHANNEL_EMOJIS = ["👍", "❤️", "🔥", "😂", "🎉"];

function channelRow(c: ChannelInfo, onOpen: (id: string) => void): ReactNode {
  return (
    <li key={c.id} className="row" role="button" tabIndex={0} onClick={() => onOpen(c.id)} onKeyDown={onActivate(() => onOpen(c.id))}>
      <div className="row-title">
        📢 {c.name} {c.verified && <span title="Verified">✔️</span>}
        <span className="muted" style={{ fontWeight: 400, fontSize: "0.75rem" }}> · @{c.handle}</span>
      </div>
      <div className="row-sub">
        {c.followers} follower{c.followers === 1 ? "" : "s"}
        {c.description ? ` · ${c.description}` : ""}
      </div>
    </li>
  );
}

/** Channels screen (T7.02): discover + search public channels, or create one. */
export function Channels({ onOpen, onBack }: { onOpen: (id: string) => void; onBack: () => void }) {
  const { services } = useServices();
  const [tab, setTab] = useState<"discover" | "create">("discover");
  const [query, setQuery] = useState("");
  const [discover, setDiscover] = useState<ChannelInfo[]>([]);
  const [results, setResults] = useState<ChannelInfo[]>([]);
  // create form
  const [handle, setHandle] = useState("");
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [kind, setKind] = useState<"public" | "private">("public");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let alive = true;
    services.discoverChannels().then((c) => alive && setDiscover(c)).catch(() => {});
    const unsub = services.onChange(() => services.discoverChannels().then((c) => alive && setDiscover(c)).catch(() => {}));
    return () => { alive = false; unsub(); };
  }, [services]);

  useEffect(() => {
    if (query.trim().length < 2) { setResults([]); return; }
    let alive = true;
    const h = setTimeout(() => services.searchChannels(query).then((r) => alive && setResults(r)).catch(() => {}), 200);
    return () => { alive = false; clearTimeout(h); };
  }, [query, services]);

  async function create(): Promise<void> {
    setBusy(true);
    setError(null);
    try {
      onOpen(await services.createChannel(handle.trim(), name.trim(), description.trim(), kind));
    } catch (err) {
      setError(messageOf(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="card">
      <button type="button" className="btn small" onClick={onBack}>
        ‹ Back
      </button>
      <h1>Channels</h1>
      <div style={{ display: "flex", gap: "0.5rem", marginBottom: "0.8rem" }}>
        <button className={tab === "discover" ? "btn small" : "btn small ghost"} onClick={() => setTab("discover")}>
          Discover
        </button>
        <button className={tab === "create" ? "btn small" : "btn small ghost"} onClick={() => setTab("create")}>
          ＋ Create
        </button>
      </div>

      {tab === "discover" ? (
        <>
          <input className="input" value={query} onChange={(e) => setQuery(e.target.value)} placeholder="Search channels by name…" aria-label="Search channels" />
          {query.trim().length >= 2 ? (
            results.length === 0 ? (
              <p className="muted">No channels found.</p>
            ) : (
              <ul className="list">{results.map((c) => channelRow(c, onOpen))}</ul>
            )
          ) : (
            <>
              <h2 style={{ marginTop: "1rem" }}>Popular</h2>
              {discover.length === 0 ? <p className="muted">No channels yet — create the first one.</p> : <ul className="list">{discover.map((c) => channelRow(c, onOpen))}</ul>}
            </>
          )}
        </>
      ) : (
        <>
          <label style={fieldWrap}>
            <span className="muted" style={fieldLabel}>Handle (a–z, 0–9, _)</span>
            <input className="input" value={handle} onChange={(e) => setHandle(e.target.value)} placeholder="my_channel" maxLength={30} />
          </label>
          <label style={fieldWrap}>
            <span className="muted" style={fieldLabel}>Name</span>
            <input className="input" value={name} onChange={(e) => setName(e.target.value)} placeholder="My Channel" maxLength={80} />
          </label>
          <label style={fieldWrap}>
            <span className="muted" style={fieldLabel}>Description</span>
            <input className="input" value={description} onChange={(e) => setDescription(e.target.value)} placeholder="What's it about?" maxLength={500} />
          </label>
          <label style={fieldWrap}>
            <span className="muted" style={fieldLabel}>Visibility</span>
            <select className="input" value={kind} onChange={(e) => setKind(e.target.value as "public" | "private")}>
              <option value="public">Public — anyone can find &amp; follow</option>
              <option value="private">Private — invite-only</option>
            </select>
          </label>
          {error && <p className="error" role="alert">{error}</p>}
          <button className="btn" onClick={() => void create()} disabled={busy || !handle.trim() || !name.trim()}>
            {busy ? "Creating…" : "Create channel"}
          </button>
        </>
      )}
    </div>
  );
}

/** ChannelScreen (T7.02): a channel's feed — follow/unfollow, admin composer,
 *  posts with reactions + comments. */
export function ChannelScreen({ channelId, onBack }: { channelId: string; onBack: () => void }) {
  const { services } = useServices();
  const [channel, setChannel] = useState<ChannelInfo | null>(null);
  const [posts, setPosts] = useState<ChannelPost[]>([]);
  const [draft, setDraft] = useState("");
  const [scheduleAt, setScheduleAt] = useState("");
  const [insights, setInsights] = useState<ChannelInsights | null>(null);
  const [showInsights, setShowInsights] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(() => {
    void services.getChannel(channelId).then(setChannel).catch(() => {});
    void services.channelPosts(channelId).then(setPosts).catch(() => {});
  }, [services, channelId]);
  useEffect(() => {
    load();
    const h = setInterval(load, 8000);
    return () => clearInterval(h);
  }, [load]);

  if (!channel) return <p className="muted center">Loading…</p>;
  const isAdmin = channel.myRole === "owner" || channel.myRole === "admin";
  const isOwner = channel.myRole === "owner";
  const isMember = channel.myRole !== "";
  const premiumLocked = channel.premium && !channel.mySubscribed && !isAdmin;

  async function publish(): Promise<void> {
    const body = draft.trim();
    if (!body) return;
    setError(null);
    try {
      const at = scheduleAt ? new Date(scheduleAt).getTime() : undefined;
      await services.postToChannel(channelId, body, at && at > Date.now() ? at : undefined);
      setDraft("");
      setScheduleAt("");
      load();
    } catch (err) {
      setError(messageOf(err));
    }
  }

  async function toggleInsights(): Promise<void> {
    const next = !showInsights;
    setShowInsights(next);
    if (next) setInsights(await services.channelInsights(channelId));
  }

  async function subscribe(): Promise<void> {
    setError(null);
    try {
      await services.subscribeToChannel(channelId);
      load();
    } catch (err) {
      setError(messageOf(err));
    }
  }

  async function togglePremium(): Promise<void> {
    if (!channel) return;
    setError(null);
    const on = !channel.premium;
    let price = 0;
    if (on) {
      const raw = window.prompt("Monthly price in US cents (e.g. 500 = $5.00):", String(channel.priceCents || 500));
      if (raw === null) return;
      price = Math.max(0, Math.floor(Number(raw) || 0));
    }
    try {
      await services.setChannelPremium(channelId, on, price);
      load();
    } catch (err) {
      setError(messageOf(err));
    }
  }

  const price = `$${(channel.priceCents / 100).toFixed(2)}`;

  return (
    <div className="pane">
      <div className="pane-head">
        <button className="btn small ghost" onClick={onBack}>‹ Back</button>
        <div className="thread-title">
          <span>📢 {channel.name} {channel.verified && "✔️"} {channel.premium && <span title={`Premium · ${price}/mo`}>💎</span>}</span>
          <span className="thread-status" style={{ fontSize: "0.72rem", opacity: 0.7 }}>
            @{channel.handle} · {channel.followers} follower{channel.followers === 1 ? "" : "s"} · {channel.kind}
            {channel.premium ? ` · 💎 ${price}/mo` : ""}
          </span>
        </div>
        {channel.myRole === "owner" ? (
          <button className="btn small ghost" title="Delete channel" onClick={() => { if (window.confirm("Delete this channel for everyone?")) void services.deleteChannel(channelId).then(onBack); }}>
            🗑
          </button>
        ) : isMember ? (
          <button className="btn small ghost" onClick={() => void services.unfollowChannel(channelId).then(load)}>Following ✓</button>
        ) : (
          <button className="btn small" onClick={() => void services.followChannel(channelId).then(load)}>Follow</button>
        )}
      </div>

      {channel.description && <p className="muted" style={{ padding: "0.4rem 0.8rem", fontSize: "0.85rem" }}>{channel.description}</p>}

      {isAdmin && (
        <div style={{ display: "flex", gap: "0.5rem", padding: "0 0.8rem 0.4rem", flexWrap: "wrap" }}>
          <button className="btn small ghost" onClick={() => void toggleInsights()}>📊 Insights</button>
          {isOwner && (
            <button className="btn small ghost" onClick={() => void togglePremium()}>
              💎 {channel.premium ? `Premium ${price}/mo (turn off)` : "Enable premium"}
            </button>
          )}
        </div>
      )}

      {isAdmin && showInsights && insights && (
        <div style={{ display: "grid", gridTemplateColumns: "repeat(3, 1fr)", gap: "0.5rem", padding: "0.4rem 0.8rem", borderBottom: "1px solid var(--border, #e2e2e2)" }}>
          {[
            ["Followers", insights.followers],
            ["Subscribers", insights.subscribers],
            ["Posts", insights.posts],
            ["Views (reach)", insights.views],
            ["Reactions", insights.reactions],
            ["Comments", insights.comments],
          ].map(([label, val]) => (
            <div key={label} style={{ textAlign: "center", padding: "0.4rem", border: "1px solid var(--border, #e2e2e2)", borderRadius: 8 }}>
              <div style={{ fontSize: "1.2rem", fontWeight: 700 }}>{val}</div>
              <div className="muted" style={{ fontSize: "0.72rem" }}>{label}</div>
            </div>
          ))}
        </div>
      )}

      {isAdmin && (
        <div style={{ padding: "0.6rem 0.8rem", borderBottom: "1px solid var(--border, #e2e2e2)" }}>
          <textarea className="input" rows={2} value={draft} onChange={(e) => setDraft(e.target.value)} placeholder="Broadcast to your followers…" aria-label="New post" />
          <div style={{ display: "flex", gap: "0.5rem", alignItems: "center", marginTop: "0.4rem", flexWrap: "wrap" }}>
            <label className="muted" style={{ fontSize: "0.75rem" }}>
              Schedule <input type="datetime-local" value={scheduleAt} onChange={(e) => setScheduleAt(e.target.value)} />
            </label>
            <button className="btn small" onClick={() => void publish()} disabled={!draft.trim()}>
              {scheduleAt && new Date(scheduleAt).getTime() > Date.now() ? "Schedule" : "Post"}
            </button>
          </div>
          {error && <p className="error" role="alert">{error}</p>}
        </div>
      )}

      {premiumLocked ? (
        <div className="card" style={{ margin: "1rem", textAlign: "center" }}>
          <div style={{ fontSize: "2rem" }}>🔒</div>
          <h2>Premium channel</h2>
          <p className="muted">Subscribe for {price}/month to read {channel.name}'s posts.</p>
          <p className="muted" style={{ fontSize: "0.75rem" }}>Payments run through your provider — this dev build uses a no-op gateway (no charge).</p>
          {error && <p className="error" role="alert">{error}</p>}
          <button className="btn" onClick={() => void subscribe()}>Subscribe · {price}/mo</button>
        </div>
      ) : (
        <div className="messages">
          {posts.length === 0 ? <p className="muted center">No posts yet.</p> : null}
          {posts.map((p) => (
            <ChannelPostCard key={p.id} post={p} canDelete={isAdmin} onChanged={load} />
          ))}
        </div>
      )}
    </div>
  );
}

function ChannelPostCard({ post, canDelete, onChanged }: { post: ChannelPost; canDelete: boolean; onChanged: () => void }) {
  const { services } = useServices();
  const [mine, setMine] = useState<Set<string>>(new Set());
  const [open, setOpen] = useState(false);
  const [comments, setComments] = useState<Array<{ id: string; authorId: string; body: string; createdAtMs: number }>>([]);
  const [draft, setDraft] = useState("");

  // Record an (aggregate, privacy-preserving) view once when the post renders.
  useEffect(() => {
    if (!post.scheduled) void services.recordPostView(post.id);
  }, [services, post.id, post.scheduled]);

  async function react(emoji: string): Promise<void> {
    const on = !mine.has(emoji);
    setMine((s) => {
      const n = new Set(s);
      if (on) n.add(emoji);
      else n.delete(emoji);
      return n;
    });
    await services.reactToPost(post.id, emoji, on).catch(() => {});
    onChanged();
  }

  async function toggleComments(): Promise<void> {
    const next = !open;
    setOpen(next);
    if (next) setComments(await services.postComments(post.id));
  }

  async function comment(): Promise<void> {
    const body = draft.trim();
    if (!body) return;
    setDraft("");
    await services.commentOnPost(post.id, body).catch(() => {});
    setComments(await services.postComments(post.id));
    onChanged();
  }

  return (
    <div className="bubble theirs" style={{ maxWidth: "100%", position: "relative" }}>
      {post.scheduled && <div className="muted" style={{ fontSize: "0.72rem" }}>⏰ scheduled for {new Date(post.publishAtMs).toLocaleString()}</div>}
      <div style={{ whiteSpace: "pre-wrap", wordBreak: "break-word" }}>{post.body}</div>
      <div className="muted" style={{ fontSize: "0.7rem", marginTop: 4 }}>{new Date(post.createdAtMs).toLocaleString()}</div>
      <div style={{ display: "flex", gap: "0.3rem", flexWrap: "wrap", marginTop: 6, alignItems: "center" }}>
        {CHANNEL_EMOJIS.map((e) => (
          <button key={e} className="btn small ghost" style={{ padding: "2px 8px", opacity: mine.has(e) ? 1 : 0.7 }} onClick={() => void react(e)}>
            {e} {post.reactions[e] ? post.reactions[e] : ""}
          </button>
        ))}
        <button className="btn small ghost" onClick={() => void toggleComments()}>💬 {post.comments}</button>
        {canDelete && (
          <button className="btn small ghost" title="Delete post" onClick={() => void services.deleteChannelPost(post.id).then(onChanged)}>
            🗑
          </button>
        )}
      </div>
      {open && (
        <div style={{ marginTop: 8, borderTop: "1px solid var(--border, #e2e2e2)", paddingTop: 8 }}>
          {comments.length === 0 ? <p className="muted" style={{ fontSize: "0.8rem" }}>No comments yet.</p> : null}
          {comments.map((c) => (
            <div key={c.id} style={{ fontSize: "0.82rem", marginBottom: 4 }}>
              <strong className="mono">{services.nameForUser(c.authorId)}</strong> {c.body}
            </div>
          ))}
          <div style={{ display: "flex", gap: "0.4rem", marginTop: 4 }}>
            <input className="input" value={draft} onChange={(e) => setDraft(e.target.value)} placeholder="Add a comment…" aria-label="Comment" />
            <button className="btn small" onClick={() => void comment()} disabled={!draft.trim()}>Send</button>
          </div>
        </div>
      )}
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

// Quick emoji reactions offered in the message menu (T5.05b).
const QUICK_REACTIONS = ["👍", "❤️", "😂", "😮", "😢", "🙏"];

// Chat wallpaper presets (T5.15) — CSS backgrounds keyed by name; persisted
// per-chat on-device. "none" clears back to the themed default.
const WALLPAPERS: Record<string, string> = {
  sage: "linear-gradient(160deg, #dfe9e3, #f6f9f7)",
  dusk: "linear-gradient(160deg, #2b2f43, #4b5178)",
  sand: "linear-gradient(160deg, #f3e9d8, #fbf6ec)",
  ocean: "linear-gradient(160deg, #cfeef2, #eafbfd)",
  rose: "linear-gradient(160deg, #f6dfe6, #fdf1f5)",
  charcoal: "linear-gradient(160deg, #1b232a, #2a343d)",
};

/** WallpaperSheet lets the user pick a per-chat wallpaper (or clear it). */
function WallpaperSheet({ current, onPick, onClose }: { current: string | null; onPick: (key: string | null) => void; onClose: () => void }) {
  return (
    <div className="sheet-backdrop" role="dialog" aria-modal="true" onClick={onClose}>
      <div className="sheet" onClick={(e) => e.stopPropagation()}>
        <strong>Chat wallpaper</strong>
        <div className="wallpaper-grid">
          <button
            className={`wallpaper-swatch${current === null ? " selected" : ""}`}
            title="Default"
            aria-label="Default wallpaper"
            style={{ background: "var(--surface)" }}
            onClick={() => onPick(null)}
          />
          {Object.entries(WALLPAPERS).map(([key, bg]) => (
            <button
              key={key}
              className={`wallpaper-swatch${current === key ? " selected" : ""}`}
              title={key}
              aria-label={`${key} wallpaper`}
              style={{ background: bg }}
              onClick={() => onPick(key)}
            />
          ))}
        </div>
        <button className="btn ghost" onClick={onClose}>
          Done
        </button>
      </div>
    </div>
  );
}

/** ForwardPicker lists the user's other conversations to forward a message into. */
function ForwardPicker({ exclude, onPick, onClose }: { exclude: string; onPick: (conversationId: string) => void; onClose: () => void }) {
  const { services } = useServices();
  const [items, setItems] = useState<ChatSummary[]>([]);
  useEffect(() => {
    void services.conversations().then(setItems).catch(() => {});
  }, [services]);
  const targets = items.filter((c) => c.conversationId !== exclude);
  return (
    <div className="sheet-backdrop" role="dialog" aria-modal="true" onClick={onClose}>
      <div className="sheet" onClick={(e) => e.stopPropagation()}>
        <strong>Forward to…</strong>
        {targets.length === 0 ? (
          <p className="muted">No other conversations.</p>
        ) : (
          <ul className="list" style={{ maxHeight: "50vh" }}>
            {targets.map((c) => (
              <li
                key={c.conversationId}
                className="row"
                role="button"
                tabIndex={0}
                onClick={() => onPick(c.conversationId)}
                onKeyDown={onActivate(() => onPick(c.conversationId))}
              >
                <div className="row-title">
                  {services.groupNameOf(c.conversationId) ? `👥 ${services.groupNameOf(c.conversationId)}` : services.peerNameOf(c.conversationId) || c.title}
                </div>
              </li>
            ))}
          </ul>
        )}
        <button className="btn ghost" onClick={onClose}>
          Cancel
        </button>
      </div>
    </div>
  );
}

/** PollBubble renders a poll message: options with live tallies + vote toggles,
 *  and a Close control for the creator. Results (index-based) come from the
 *  server; option TEXTS are from the decrypted body (E2EE). */
function PollBubble({ poll, mine }: { poll: PollBody; mine: boolean }) {
  const { services } = useServices();
  const [res, setRes] = useState<PollResults | null>(null);
  const [busy, setBusy] = useState(false);

  const load = useCallback(() => {
    void services.pollResults(poll.pollId).then(setRes).catch(() => {});
  }, [services, poll.pollId]);
  useEffect(() => {
    load();
    return services.onChange(load); // refresh on any store change
  }, [load, services]);

  const vote = (i: number): void => {
    if (busy || res?.closed) return;
    let next: number[];
    if (poll.multi) {
      const set = new Set(res?.myVotes ?? []);
      if (set.has(i)) set.delete(i);
      else set.add(i);
      next = [...set];
      if (next.length === 0) return; // server rejects an empty vote; keep the last
    } else {
      next = [i];
    }
    setBusy(true);
    void services
      .votePoll(poll.pollId, next)
      .then(setRes)
      .catch((e) => window.alert(messageOf(e)))
      .finally(() => setBusy(false));
  };

  const total = res?.totalVoters ?? 0;
  return (
    <div className="poll">
      <div className="poll-q">📊 {poll.question}</div>
      <div className="poll-options">
        {poll.options.map((opt, i) => {
          const count = res?.tallies[i] ?? 0;
          const isMine = res?.myVotes.includes(i) ?? false;
          const pct = total > 0 ? Math.round((count / total) * 100) : 0;
          return (
            <button key={i} className={`poll-option${isMine ? " mine" : ""}`} disabled={busy || res?.closed} onClick={() => vote(i)}>
              <span className="poll-bar" style={{ width: `${pct}%` }} aria-hidden />
              <span className="poll-opt-label">{isMine ? "✓ " : ""}{opt}</span>
              <span className="poll-opt-count">{count}</span>
            </button>
          );
        })}
      </div>
      <div className="poll-foot">
        <span className="muted">
          {total} vote{total === 1 ? "" : "s"}
          {poll.multi ? " · multiple" : ""}
          {res?.closed ? " · closed" : ""}
        </span>
        {mine && res && !res.closed ? (
          <button
            className="btn small ghost"
            onClick={() => {
              if (window.confirm("Close this poll? No more votes will be accepted.")) {
                void services.closePoll(poll.pollId).then(load).catch((e) => window.alert(messageOf(e)));
              }
            }}
          >
            Close poll
          </button>
        ) : null}
      </div>
    </div>
  );
}

/** PollComposer collects a question + 2–12 options + a multi toggle. */
function PollComposer({ onCreate, onClose }: { onCreate: (question: string, options: string[], multi: boolean) => void; onClose: () => void }) {
  const [question, setQuestion] = useState("");
  const [options, setOptions] = useState<string[]>(["", ""]);
  const [multi, setMulti] = useState(false);
  const cleaned = options.map((o) => o.trim()).filter((o) => o !== "");
  const valid = question.trim() !== "" && cleaned.length >= 2;
  const setOpt = (i: number, v: string): void => setOptions((os) => os.map((o, j) => (j === i ? v : o)));
  return (
    <div className="sheet-backdrop" role="dialog" aria-modal="true" onClick={onClose}>
      <div className="sheet" onClick={(e) => e.stopPropagation()}>
        <strong>Create poll</strong>
        <input className="input" placeholder="Ask a question" value={question} autoFocus onChange={(e) => setQuestion(e.target.value)} />
        {options.map((o, i) => (
          <div key={i} style={{ display: "flex", gap: 6 }}>
            <input className="input" placeholder={`Option ${i + 1}`} value={o} onChange={(e) => setOpt(i, e.target.value)} />
            {options.length > 2 ? (
              <button className="btn small ghost" aria-label="Remove option" onClick={() => setOptions((os) => os.filter((_, j) => j !== i))}>
                ×
              </button>
            ) : null}
          </div>
        ))}
        {options.length < 12 ? (
          <button className="btn small ghost" onClick={() => setOptions((os) => [...os, ""])}>
            ＋ Add option
          </button>
        ) : null}
        <label style={{ display: "flex", gap: 8, alignItems: "center", fontSize: "0.9rem" }}>
          <input type="checkbox" checked={multi} onChange={(e) => setMulti(e.target.checked)} /> Allow multiple answers
        </label>
        <div style={{ display: "flex", gap: 8, justifyContent: "flex-end" }}>
          <button className="btn ghost" onClick={onClose}>
            Cancel
          </button>
          <button className="btn" disabled={!valid} onClick={() => onCreate(question.trim(), cleaned, multi)}>
            Create
          </button>
        </div>
      </div>
    </div>
  );
}

// currentPosition resolves the device's coordinates via the browser Geolocation
// API (prompts for permission; only used when the user shares location).
function currentPosition(): Promise<{ lat: number; lng: number }> {
  return new Promise((resolve, reject) => {
    if (!navigator.geolocation) {
      reject(new Error("Geolocation isn't available in this browser."));
      return;
    }
    navigator.geolocation.getCurrentPosition(
      (p) => resolve({ lat: p.coords.latitude, lng: p.coords.longitude }),
      (e) => reject(new Error(e.message || "Location permission denied.")),
      { enableHighAccuracy: true, timeout: 10_000 },
    );
  });
}

// mapsHref builds an "open in maps" link for a coordinate (OpenStreetMap — no
// key, no client-side tile fetch that would leak location to a tile server).
function mapsHref(lat: number, lng: number): string {
  return `https://www.openstreetmap.org/?mlat=${lat}&mlon=${lng}#map=16/${lat}/${lng}`;
}
function fmtCoord(lat: number, lng: number): string {
  return `${lat.toFixed(5)}, ${lng.toFixed(5)}`;
}

/** LocationCard renders a one-off shared place. */
function LocationCard({ loc }: { loc: LocationBody }) {
  return (
    <a className="loc-card" href={mapsHref(loc.lat, loc.lng)} target="_blank" rel="noopener noreferrer">
      <span className="loc-pin" aria-hidden>📍</span>
      <span className="loc-body">
        <span className="loc-title">{loc.label || "Location"}</span>
        <span className="loc-coord mono">{fmtCoord(loc.lat, loc.lng)}</span>
        <span className="loc-open">Open in maps ↗</span>
      </span>
    </a>
  );
}

/** LiveLocationBubble renders the latest sample of a live share, with a live/
 *  ended state. The sender sees a Stop control while their share is ticking. */
function LiveLocationBubble({ live, mine }: { live: LiveLocationBody; mine: boolean }) {
  const { services } = useServices();
  const [, force] = useState(0);
  const ended = Date.now() > live.untilMs;
  useEffect(() => {
    if (ended) return;
    const h = setInterval(() => force((n) => n + 1), 30_000); // refresh the "ends in" label
    return () => clearInterval(h);
  }, [ended]);
  const sharing = mine && services.isLiveSharing(live.shareId);
  return (
    <div className="loc-card live">
      <a className="loc-inner" href={mapsHref(live.lat, live.lng)} target="_blank" rel="noopener noreferrer">
        <span className="loc-pin" aria-hidden>{ended ? "📍" : "🛰️"}</span>
        <span className="loc-body">
          <span className="loc-title">{ended ? "Live location ended" : "Live location"}</span>
          <span className="loc-coord mono">{fmtCoord(live.lat, live.lng)}</span>
          <span className="loc-open">
            {ended ? "Open last spot ↗" : `Live until ${new Date(live.untilMs).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" })} ↗`}
          </span>
        </span>
      </a>
      {sharing ? (
        <button
          className="btn small danger"
          onClick={() => {
            services.stopLiveLocation(live.shareId);
            force((n) => n + 1);
          }}
        >
          Stop
        </button>
      ) : null}
    </div>
  );
}

/** ContactCardBubble renders a shared contact with a Message action. */
function ContactCardBubble({ card }: { card: ContactCardBody }) {
  const { services } = useServices();
  return (
    <div className="contact-card">
      <span className="contact-avatar" aria-hidden>👤</span>
      <span className="contact-body">
        <span className="contact-name">{card.name}</span>
        {card.phone ? <span className="contact-phone mono">{card.phone}</span> : null}
      </span>
      {card.userId ? (
        <button className="btn small" onClick={() => void services.openDirectWithUser(card.userId!).catch(() => {})}>
          Message
        </button>
      ) : null}
    </div>
  );
}

/** LocationSheet shares the device's current place, or starts a time-boxed live
 *  share (15 min / 1 hour). */
function LocationSheet({ conversationId, onClose }: { conversationId: string; onClose: () => void }) {
  const { services } = useServices();
  const [pos, setPos] = useState<{ lat: number; lng: number } | null>(null);
  const [err, setErr] = useState<string | null>(null);
  useEffect(() => {
    currentPosition().then(setPos).catch((e: Error) => setErr(e.message));
  }, []);
  const startLive = (mins: number): void => {
    services.startLiveLocation(conversationId, mins * 60_000, currentPosition);
    onClose();
  };
  return (
    <div className="sheet-backdrop" role="dialog" aria-modal="true" onClick={onClose}>
      <div className="sheet" onClick={(e) => e.stopPropagation()}>
        <strong>Share location</strong>
        {err ? <p className="muted">Couldn't get your location: {err}</p> : pos ? <p className="mono">{fmtCoord(pos.lat, pos.lng)}</p> : <p className="muted">Locating…</p>}
        <button className="btn" disabled={!pos} onClick={() => { if (pos) { void services.sendLocation(conversationId, pos.lat, pos.lng); onClose(); } }}>
          Send current location
        </button>
        <div style={{ display: "flex", gap: 8 }}>
          <button className="btn ghost" style={{ flex: 1 }} disabled={!pos} onClick={() => startLive(15)}>
            Live · 15 min
          </button>
          <button className="btn ghost" style={{ flex: 1 }} disabled={!pos} onClick={() => startLive(60)}>
            Live · 1 hour
          </button>
        </div>
        <button className="btn ghost" onClick={onClose}>
          Cancel
        </button>
      </div>
    </div>
  );
}

/** ContactPicker shares one of your direct-chat peers as a contact card. Phone
 *  numbers aren't held client-side (privacy), so the card carries name + userId
 *  (its Message button opens a chat); a phone is a later refinement. */
function ContactPicker({ conversationId, onClose }: { conversationId: string; onClose: () => void }) {
  const { services } = useServices();
  const [items, setItems] = useState<ChatSummary[]>([]);
  useEffect(() => {
    void services.conversations().then(setItems).catch(() => {});
  }, [services]);
  const peers = items
    .map((c) => ({ userId: services.peerOf(c.conversationId), name: services.peerNameOf(c.conversationId) }))
    .filter((p): p is { userId: string; name: string } => !!p.userId && !!p.name);
  return (
    <div className="sheet-backdrop" role="dialog" aria-modal="true" onClick={onClose}>
      <div className="sheet" onClick={(e) => e.stopPropagation()}>
        <strong>Share a contact</strong>
        {peers.length === 0 ? (
          <p className="muted">No contacts to share yet.</p>
        ) : (
          <ul className="list" style={{ maxHeight: "50vh" }}>
            {peers.map((p) => {
              const share = (): void => {
                void services.sendContactCard(conversationId, p.name, "", p.userId);
                onClose();
              };
              return (
                <li key={p.userId} className="row" role="button" tabIndex={0} onClick={share} onKeyDown={onActivate(share)}>
                  <div className="row-title">👤 {p.name}</div>
                </li>
              );
            })}
          </ul>
        )}
        <button className="btn ghost" onClick={onClose}>
          Cancel
        </button>
      </div>
    </div>
  );
}

// toLocalInput formats an epoch-ms as a <input type="datetime-local"> value in
// the browser's local time.
function toLocalInput(ms: number): string {
  const d = new Date(ms - new Date(ms).getTimezoneOffset() * 60_000);
  return d.toISOString().slice(0, 16);
}

/** ScheduleSheet picks a future time to send the current draft (T6.04). */
function ScheduleSheet({ draft, onSchedule, onClose }: { draft: string; onSchedule: (sendAtMs: number) => void; onClose: () => void }) {
  const [when, setWhen] = useState(() => toLocalInput(Date.now() + 60 * 60_000)); // default +1h
  const ms = new Date(when).getTime();
  const valid = draft.trim() !== "" && Number.isFinite(ms) && ms > Date.now();
  return (
    <div className="sheet-backdrop" role="dialog" aria-modal="true" onClick={onClose}>
      <div className="sheet" onClick={(e) => e.stopPropagation()}>
        <strong>Schedule message</strong>
        <p className="muted" style={{ margin: 0 }}>“{draft.trim().slice(0, 80) || "(type a message first)"}”</p>
        <input className="input" type="datetime-local" value={when} onChange={(e) => setWhen(e.target.value)} />
        <div style={{ display: "flex", gap: 8, justifyContent: "flex-end" }}>
          <button className="btn ghost" onClick={onClose}>
            Cancel
          </button>
          <button className="btn" disabled={!valid} onClick={() => onSchedule(ms)}>
            Schedule
          </button>
        </div>
      </div>
    </div>
  );
}

/** TemplatePicker inserts a saved reply into the composer (T6.04). */
function TemplatePicker({ onPick, onClose }: { onPick: (text: string) => void; onClose: () => void }) {
  const { services } = useServices();
  const templates = services.listTemplates();
  return (
    <div className="sheet-backdrop" role="dialog" aria-modal="true" onClick={onClose}>
      <div className="sheet" onClick={(e) => e.stopPropagation()}>
        <strong>Saved replies</strong>
        {templates.length === 0 ? (
          <p className="muted">No saved replies yet — add them in Settings → Saved replies.</p>
        ) : (
          <ul className="list" style={{ maxHeight: "50vh" }}>
            {templates.map((t) => (
              <li key={t.id} className="row" role="button" tabIndex={0} onClick={() => onPick(t.text)} onKeyDown={onActivate(() => onPick(t.text))}>
                <div className="row-title">{t.title || t.text.slice(0, 30)}</div>
                <div className="row-sub">{t.text}</div>
              </li>
            ))}
          </ul>
        )}
        <button className="btn ghost" onClick={onClose}>
          Cancel
        </button>
      </div>
    </div>
  );
}

export function Thread({
  conversationId,
  onBack,
  onGroupInfo,
  onSearchInChat,
  focusMsgUuid,
}: {
  conversationId: string;
  onBack: () => void;
  onGroupInfo: (id: string) => void;
  onSearchInChat?: (id: string) => void;
  focusMsgUuid?: string;
}) {
  const { services } = useServices();
  const call = useCall();
  const [messages, setMessages] = useState<ThreadMessage[]>([]);
  const [group, setGroup] = useState<GroupInfo | null>(() => services.groupOf(conversationId) ?? null);
  const [muted, setMuted] = useState<boolean>(() => services.isMuted(conversationId));
  const [draft, setDraft] = useState("");
  const [gallery, setGallery] = useState<{ items: MediaEnvelope[]; startKey: string } | null>(null);
  const [replyingTo, setReplyingTo] = useState<QuotedRef | null>(null);
  const [editing, setEditing] = useState<string | null>(null); // msgUuid being edited
  const [sendingMedia, setSendingMedia] = useState(false);
  const [wallpaper, setWallpaperState] = useState<string | null>(() => services.chatWallpaper(conversationId));
  const [showWallpaper, setShowWallpaper] = useState(false);
  const [flashId, setFlashId] = useState<string | null>(null); // jump-to-original highlight
  const focusedRef = useRef<string | null>(null); // search jump-to-result (once per target)
  const [forwardMsg, setForwardMsg] = useState<ThreadMessage | null>(null); // message being forwarded
  const [picker, setPicker] = useState<"emoji" | "gif" | "sticker" | null>(null); // composer picker
  const [showPoll, setShowPoll] = useState(false); // poll-creation modal
  const [showLocation, setShowLocation] = useState(false); // location-share sheet
  const [showContact, setShowContact] = useState(false); // contact-share picker
  const [showSchedule, setShowSchedule] = useState(false); // schedule-message sheet
  const [showTemplates, setShowTemplates] = useState(false); // saved-reply picker
  const fileRef = useRef<HTMLInputElement>(null);
  const composerRef = useRef<HTMLInputElement>(null);
  const bubbleRefs = useRef<Record<string, HTMLDivElement | null>>({});

  const lastReadRef = useRef(0);
  const subscribedRef = useRef(false);
  const lastTypingRef = useRef(0);
  useEffect(() => {
    let alive = true;
    lastReadRef.current = 0; // reset the read watermark per conversation
    subscribedRef.current = false;
    services.setActiveConversation(conversationId); // clears unread + suppresses toasts here
    setMuted(services.isMuted(conversationId));
    setDraft(services.draft(conversationId)); // restore the persisted composer draft
    setWallpaperState(services.chatWallpaper(conversationId));
    setGroup(services.groupOf(conversationId) ?? null);
    // Classify the conversation: a group (name + settings for the header/composer)
    // or a 1:1 (peer presence). loadGroup 404s on direct chats → null.
    void services.loadGroup(conversationId).then((g) => {
      if (alive) setGroup(g);
    });
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
      services.setActiveConversation(null); // leaving the thread → toasts resume
    };
  }, [services, conversationId]);

  const isAdmin = group ? group.myRole === "owner" || group.myRole === "admin" : false;
  const canPost = group
    ? isAdmin || (!group.settings.announcements && group.settings.who_can_post !== "admins")
    : true;
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
    services.setDraft(conversationId, v); // persist per-chat draft (T5.15)
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
    services.setDraft(conversationId, ""); // clear the persisted draft
    lastTypingRef.current = 0;
    services.sendTyping(conversationId, false); // stop the typing indicator on send
    if (editing) {
      const target = editing;
      setEditing(null);
      await services.editMessage(conversationId, target, text);
      setMessages(await services.thread(conversationId));
    } else {
      const reply = replyingTo ?? undefined;
      setReplyingTo(null);
      // Undo-send: hold the message for a short window; the Undo bar can cancel it.
      services.sendTextWithUndo(conversationId, text, reply);
    }
  }

  function undoSend(): void {
    const text = services.undoSend();
    if (text !== null) {
      setDraft(text); // restore to the composer so the user can edit or discard
      services.setDraft(conversationId, text);
    }
  }

  // Jump-to-original: scroll a message into view and flash it (T5.15).
  function jumpTo(msgUuid: string): void {
    const el = bubbleRefs.current[msgUuid];
    if (!el) return;
    el.scrollIntoView({ behavior: "smooth", block: "center" });
    setFlashId(msgUuid);
    setTimeout(() => setFlashId((c) => (c === msgUuid ? null : c)), 1500);
  }

  // Jump-to-search-result (T6.05): once the focused message is rendered, scroll
  // to + flash it, once per target so it doesn't re-jump on every refresh.
  useEffect(() => {
    if (!focusMsgUuid || focusedRef.current === focusMsgUuid) return;
    if (bubbleRefs.current[focusMsgUuid]) {
      focusedRef.current = focusMsgUuid;
      jumpTo(focusMsgUuid);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [focusMsgUuid, messages]);

  async function exportChat(): Promise<void> {
    const text = await services.exportChat(conversationId);
    const url = URL.createObjectURL(new Blob([text], { type: "text/plain" }));
    const a = document.createElement("a");
    a.href = url;
    a.download = `chat-${conversationId.slice(0, 8)}.txt`;
    a.click();
    URL.revokeObjectURL(url);
  }

  function pickWallpaper(key: string | null): void {
    services.setChatWallpaper(conversationId, key);
    setWallpaperState(key);
    setShowWallpaper(false);
  }

  // Composer helpers (T6.01): insert at the cursor + wrap the selection with a
  // markdown marker (*bold*, _italic_), restoring focus/caret after the update.
  function insertAtCursor(insert: string): void {
    const el = composerRef.current;
    const start = el?.selectionStart ?? draft.length;
    const end = el?.selectionEnd ?? draft.length;
    const next = draft.slice(0, start) + insert + draft.slice(end);
    onDraftChange(next);
    requestAnimationFrame(() => {
      el?.focus();
      const pos = start + insert.length;
      el?.setSelectionRange(pos, pos);
    });
  }
  function wrapSelection(marker: string): void {
    const el = composerRef.current;
    const start = el?.selectionStart ?? draft.length;
    const end = el?.selectionEnd ?? draft.length;
    if (start === end) {
      insertAtCursor(marker + marker);
      requestAnimationFrame(() => el?.setSelectionRange(start + marker.length, start + marker.length));
      return;
    }
    const next = draft.slice(0, start) + marker + draft.slice(start, end) + marker + draft.slice(end);
    onDraftChange(next);
    requestAnimationFrame(() => {
      el?.focus();
      el?.setSelectionRange(start + marker.length, end + marker.length);
    });
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
    react: (m, emoji) => {
      const mine = m.reactions.some((r) => r.emoji === emoji && r.mine);
      void services.react(conversationId, m.msgUuid, emoji, mine ? "remove" : "add");
    },
    forward: (m) => setForwardMsg(m),
  };

  // Every image/video in the thread, so the lightbox can page across them.
  const visuals: MediaEnvelope[] = [];
  // Latest live-location sample per share, so the thread shows one moving pin.
  const liveLatest = new Map<string, string>();
  for (const m of messages) {
    if (m.deleted) continue;
    const media = parseMediaMessage(m.body);
    if (media) for (const a of media.attachments) if (isVisual(a)) visuals.push(a);
    const lv = parseLiveLocation(m.body);
    if (lv) liveLatest.set(lv.shareId, m.msgUuid);
  }

  return (
    <div className="pane">
      <div className="pane-head">
        <button className="btn small ghost" onClick={onBack}>
          ‹ Back
        </button>
        <div
          className="thread-title"
          role={group ? "button" : undefined}
          tabIndex={group ? 0 : undefined}
          style={group ? { cursor: "pointer" } : undefined}
          onClick={group ? () => onGroupInfo(conversationId) : undefined}
          onKeyDown={group ? onActivate(() => onGroupInfo(conversationId)) : undefined}
          title={group ? "Group info" : undefined}
        >
          <span className={group || services.peerNameOf(conversationId) ? "" : "mono"}>
            {group ? group.name : services.peerNameOf(conversationId) || conversationId.slice(0, 12)}
          </span>
          {group ? (
            <span className="thread-status" style={{ fontSize: "0.72rem", opacity: 0.7 }}>
              {group.settings.announcements ? "📢 Announcements" : "Group"} · tap for info
            </span>
          ) : statusLine ? (
            <span className="thread-status" style={{ fontSize: "0.72rem", opacity: 0.7 }}>
              {statusLine}
            </span>
          ) : null}
        </div>
        {onSearchInChat && (
          <button className="btn small ghost" title="Search in this chat" aria-label="Search in this chat" onClick={() => onSearchInChat(conversationId)}>
            <span aria-hidden>🔍</span>
          </button>
        )}
        <button
          className="btn small ghost"
          title={muted ? "Unmute notifications" : "Mute notifications"}
          aria-label={muted ? "Unmute notifications" : "Mute notifications"}
          onClick={() => {
            services.toggleMute(conversationId);
            setMuted(services.isMuted(conversationId));
          }}
        >
          <span aria-hidden>{muted ? "🔇" : "🔔"}</span>
        </button>
        <button className="btn small ghost" title="Chat wallpaper" aria-label="Chat wallpaper" onClick={() => setShowWallpaper(true)}>
          <span aria-hidden>🎨</span>
        </button>
        <button className="btn small ghost" title="Export chat" aria-label="Export chat" onClick={() => void exportChat()}>
          <span aria-hidden>⬇</span>
        </button>
        {group ? (
          <button className="btn small ghost" title="Group info" aria-label="Group info" onClick={() => onGroupInfo(conversationId)}>
            <span aria-hidden>ℹ️</span>
          </button>
        ) : (
          <>
            <button
              className="btn small ghost call-btn"
              title="Voice call"
              aria-label="Start voice call"
              disabled={!peerId}
              onClick={() => peerId && void call.startCall(peerId, "voice")}
            >
              <span aria-hidden>📞</span>
            </button>
            <button
              className="btn small ghost call-btn"
              title="Video call"
              aria-label="Start video call"
              disabled={!peerId}
              onClick={() => peerId && void call.startCall(peerId, "video")}
            >
              <span aria-hidden>📹</span>
            </button>
          </>
        )}
      </div>
      <div className="messages" style={wallpaper ? { background: WALLPAPERS[wallpaper] ?? undefined } : undefined}>
        {messages.length === 0 ? <p className="muted center">Say hello 👋</p> : null}
        {messages.map((m) => {
          // Collapse a live-location share to its latest sample (one moving pin).
          const lv = parseLiveLocation(m.body);
          if (lv && liveLatest.get(lv.shareId) !== m.msgUuid) return null;
          return (
            <MessageBubble
              key={m.msgUuid}
              message={m}
              actions={actions}
              onOpen={(env) => setGallery({ items: visuals, startKey: env.objectKey })}
              bubbleRef={(el) => {
                bubbleRefs.current[m.msgUuid] = el;
              }}
              flash={flashId === m.msgUuid}
              onJump={jumpTo}
            />
          );
        })}
      </div>
      <DownloadsPanel />
      {services.hasPendingSend(conversationId) ? (
        <div className="undo-bar" role="status">
          <span>Sending…</span>
          <button type="button" onClick={undoSend}>
            Undo
          </button>
        </div>
      ) : null}
      {showWallpaper ? (
        <WallpaperSheet current={wallpaper} onPick={pickWallpaper} onClose={() => setShowWallpaper(false)} />
      ) : null}
      {forwardMsg ? (
        <ForwardPicker
          exclude={conversationId}
          onPick={(target) => {
            void services.forwardMessage(conversationId, forwardMsg.msgUuid, target);
            setForwardMsg(null);
          }}
          onClose={() => setForwardMsg(null)}
        />
      ) : null}
      {showPoll ? (
        <PollComposer
          onClose={() => setShowPoll(false)}
          onCreate={(question, options, multi) => {
            setShowPoll(false);
            void services.createPoll(conversationId, question, options, multi).catch((e) => window.alert(messageOf(e)));
          }}
        />
      ) : null}
      {showLocation ? <LocationSheet conversationId={conversationId} onClose={() => setShowLocation(false)} /> : null}
      {showContact ? <ContactPicker conversationId={conversationId} onClose={() => setShowContact(false)} /> : null}
      {showTemplates ? (
        <TemplatePicker
          onClose={() => setShowTemplates(false)}
          onPick={(text) => {
            setShowTemplates(false);
            insertAtCursor(text);
          }}
        />
      ) : null}
      {showSchedule ? (
        <ScheduleSheet
          draft={draft}
          onClose={() => setShowSchedule(false)}
          onSchedule={(sendAtMs) => {
            const text = draft.trim();
            if (text) {
              services.scheduleMessage(conversationId, text, sendAtMs);
              setDraft("");
              services.setDraft(conversationId, "");
            }
            setShowSchedule(false);
          }}
        />
      ) : null}
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
      {group && !canPost ? (
        <div className="composer" style={{ justifyContent: "center", padding: "0.9rem" }}>
          <span className="muted" role="status">
            📢 Only admins can post in this group.
          </span>
        </div>
      ) : (
        <div className="composer-wrap">
          {picker === "emoji" ? <EmojiPicker onPick={(e) => insertAtCursor(e)} onClose={() => setPicker(null)} /> : null}
          {picker === "gif" ? (
            <GifPicker
              onPick={(g) => {
                setPicker(null);
                void services.sendGif(conversationId, g).catch((err) => window.alert(messageOf(err)));
              }}
              onClose={() => setPicker(null)}
            />
          ) : null}
          {picker === "sticker" ? (
            <StickerPicker
              onPick={(s) => {
                setPicker(null);
                void services.sendSticker(conversationId, s);
              }}
              onClose={() => setPicker(null)}
            />
          ) : null}
          <form className="composer" onSubmit={send}>
            <input ref={fileRef} type="file" hidden onChange={onPickFile} aria-hidden />
            <button className="btn small ghost" type="button" aria-label="Bold" title="Bold (*text*)" disabled={!!editing} onClick={() => wrapSelection("*")}>
              <b>B</b>
            </button>
            <button className="btn small ghost" type="button" aria-label="Italic" title="Italic (_text_)" disabled={!!editing} onClick={() => wrapSelection("_")}>
              <i>I</i>
            </button>
            <button className="btn small ghost" type="button" aria-label="Emoji" title="Emoji" onClick={() => setPicker((p) => (p === "emoji" ? null : "emoji"))}>
              😊
            </button>
            <button className="btn small ghost" type="button" aria-label="GIF" title="GIF search" disabled={!!editing} onClick={() => setPicker((p) => (p === "gif" ? null : "gif"))}>
              GIF
            </button>
            <button className="btn small ghost" type="button" aria-label="Sticker" title="Stickers" disabled={!!editing} onClick={() => setPicker((p) => (p === "sticker" ? null : "sticker"))}>
              🏷
            </button>
            <button className="btn small ghost" type="button" aria-label="Poll" title="Create a poll" disabled={!!editing} onClick={() => setShowPoll(true)}>
              📊
            </button>
            <button className="btn small ghost" type="button" aria-label="Location" title="Share location" disabled={!!editing} onClick={() => setShowLocation(true)}>
              📍
            </button>
            <button className="btn small ghost" type="button" aria-label="Contact" title="Share a contact" disabled={!!editing} onClick={() => setShowContact(true)}>
              👤
            </button>
            <button className="btn small ghost" type="button" aria-label="Saved replies" title="Saved replies" disabled={!!editing} onClick={() => setShowTemplates(true)}>
              📋
            </button>
            <button className="btn small ghost" type="button" aria-label="Schedule" title="Schedule this message" disabled={!!editing || draft.trim() === ""} onClick={() => setShowSchedule(true)}>
              🕒
            </button>
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
              ref={composerRef}
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
        </div>
      )}
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
  react(m: ThreadMessage, emoji: string): void;
  forward(m: ThreadMessage): void;
}

const EDIT_WINDOW_MS = 15 * 60 * 1000; // FR-MSG-06
const DELETE_WINDOW_MS = 48 * 60 * 60 * 1000; // FR-MSG-05

/** snippetOf gives a short preview of a message for a reply quote. */
function snippetOf(m: ThreadMessage): string {
  if (m.deleted) return "deleted message";
  if (parseMediaMessage(m.body)) return "📎 Media";
  const sticker = parseSticker(m.body);
  if (sticker) return `${sticker.emoji} Sticker`;
  const poll = parsePoll(m.body);
  if (poll) return `📊 ${poll.question}`;
  return parseTextMessage(m.body).text.slice(0, 80);
}

/** MessageBubble renders a text/media message with its quoted reply, edited/
 *  star/pin state, and a hover action menu (reply/copy/edit/delete/star/pin). */
function MessageBubble({
  message,
  actions,
  onOpen,
  bubbleRef,
  flash,
  onJump,
}: {
  message: ThreadMessage;
  actions: MessageActions;
  onOpen: (env: MediaEnvelope) => void;
  bubbleRef?: (el: HTMLDivElement | null) => void;
  flash?: boolean;
  onJump?: (msgUuid: string) => void;
}) {
  const [menu, setMenu] = useState(false);
  const media = message.deleted ? null : parseMediaMessage(message.body);
  const sticker = media || message.deleted ? null : parseSticker(message.body);
  const poll = media || sticker || message.deleted ? null : parsePoll(message.body);
  const location = media || sticker || poll || message.deleted ? null : parseLocation(message.body);
  const live = media || sticker || poll || location || message.deleted ? null : parseLiveLocation(message.body);
  const contact = media || sticker || poll || location || live || message.deleted ? null : parseContactCard(message.body);
  const special = !!(media || sticker || poll || location || live || contact);
  const text = special || message.deleted ? null : parseTextMessage(message.body);
  const age = Date.now() - message.createdAt;
  const canEdit = message.mine && !message.deleted && !special && age < EDIT_WINDOW_MS;
  const canDeleteAll = message.mine && !message.deleted && age < DELETE_WINDOW_MS;

  const run = (fn: (m: ThreadMessage) => void) => () => {
    setMenu(false);
    fn(message);
  };

  return (
    <div ref={bubbleRef} className={`bubble ${message.mine ? "mine" : "theirs"}${flash ? " jump-flash" : ""}`} style={{ position: "relative" }}>
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
          <div className="react-row">
            {QUICK_REACTIONS.map((emoji) => (
              <button
                key={emoji}
                className={`react-pick${message.reactions.some((r) => r.emoji === emoji && r.mine) ? " on" : ""}`}
                aria-label={`React ${emoji}`}
                onClick={() => {
                  setMenu(false);
                  actions.react(message, emoji);
                }}
              >
                {emoji}
              </button>
            ))}
          </div>
          <button className="menu-item" onClick={run(actions.reply)}>↩ Reply</button>
          <button className="menu-item" onClick={run(actions.forward)}>↪ Forward</button>
          {text ? <button className="menu-item" onClick={run(actions.copy)}>⧉ Copy</button> : null}
          <button className="menu-item" onClick={run(actions.toggleStar)}>{message.starred ? "☆ Unstar" : "⭐ Star"}</button>
          <button className="menu-item" onClick={run(actions.togglePin)}>{message.pinned ? "📌 Unpin" : "📌 Pin"}</button>
          {canEdit ? <button className="menu-item" onClick={run(actions.edit)}>✎ Edit</button> : null}
          {canDeleteAll ? <button className="menu-item danger" onClick={run(actions.deleteForEveryone)}>🗑 Delete for everyone</button> : null}
          <button className="menu-item danger" onClick={run(actions.deleteForMe)}>🗑 Delete for me</button>
        </div>
      ) : null}

      {text?.reply ? (
        <div
          className="reply-quote"
          role="button"
          tabIndex={0}
          title="Jump to original message"
          onClick={() => text.reply && onJump?.(text.reply.msgUuid)}
          onKeyDown={onActivate(() => text.reply && onJump?.(text.reply.msgUuid))}
          style={{ borderLeft: "3px solid #128C7E", padding: "2px 8px", margin: "0 0 4px", background: "rgba(0,0,0,0.06)", borderRadius: 4, fontSize: "0.8rem", opacity: 0.85 }}
        >
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
      ) : sticker ? (
        <span className="sticker-msg" role="img" aria-label="sticker">{sticker.emoji}</span>
      ) : poll ? (
        <PollBubble poll={poll} mine={message.mine} />
      ) : location ? (
        <LocationCard loc={location} />
      ) : live ? (
        <LiveLocationBubble live={live} mine={message.mine} />
      ) : contact ? (
        <ContactCardBubble card={contact} />
      ) : (
        <>
          <span>{message.deleted ? <em style={{ opacity: 0.7 }}>This message was deleted</em> : text ? <RichText text={text.text} /> : null}</span>
          {message.edited && !message.deleted ? <span style={{ fontSize: "0.68rem", opacity: 0.6, marginLeft: 4 }}>(edited)</span> : null}
          {text?.linkPreview ? <LinkPreviewCard preview={text.linkPreview} /> : null}
        </>
      )}
      {message.reactions.length > 0 ? (
        <div className="reactions">
          {message.reactions.map((r) => (
            <button
              key={r.emoji}
              className={`reaction-chip${r.mine ? " mine" : ""}`}
              title={r.mine ? "Remove your reaction" : "React"}
              onClick={() => actions.react(message, r.emoji)}
            >
              <span>{r.emoji}</span>
              {r.count > 1 ? <span className="reaction-count">{r.count}</span> : null}
            </button>
          ))}
        </div>
      ) : null}
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

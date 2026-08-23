import {
  DISAPPEARING_PRESETS,
  SNIPPET_CLOSE,
  SNIPPET_OPEN,
  correctGrammar,
  detectToxicity,
  disclosureFor,
  extractHashtags,
  isValidPhone,
  makeClear,
  makeStroke,
  meetingSummary,
  mergeOps,
  parseInteractive,
  renderStrokes,
  replyTextFor,
  smartReplies,
  sweepExpired,
  validateInteractive,
  type AiMode,
  type BoardOp,
  type ChatSummary,
  type EphemeralMessage,
  type InteractiveButton,
  type InteractiveMessage,
  type MeetingSummary,
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
import { useCallback, useEffect, useRef, useState, type ChangeEvent, type CSSProperties, type FormEvent, type KeyboardEvent, type PointerEvent as ReactPointerEvent, type ReactNode } from "react";
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
import { Icon } from "./icons";
import type { BotView, CallHistoryItem, ChannelInfo, ChannelInsights, ChannelPost, CollabActivity, CollabComment, CollabNote, CollabRevision, CollabTask, CommunityEvent, CommunityInfo, CommunityMember, CommunitySummary, DiscoverResult, GroupInfo, GroupMember, Invite, LinkedDevice, LoginInfo, MatchedContact, NotifPrefs, NotificationEntry, ScheduledNotif, PasskeyInfo, PollResults, StoryFeedItem, StoryViewer, UserRef } from "../services/appServices";

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

/** WhatsApp default avatar: a gray circle with a white person (or group)
 *  silhouette — the exact placeholder WhatsApp shows for a contact without a
 *  photo. `group` (or the legacy `emoji="👥"`) swaps in the group silhouette. */
export function Avatar({ size = 49, emoji, group }: { name?: string; id?: string; size?: number; emoji?: string; group?: boolean }) {
  const isGroup = group ?? emoji === "👥";
  const glyph = Math.round(size * 0.62);
  return (
    <span className="avatar avatar-default" aria-hidden style={{ width: size, height: size }}>
      <svg width={glyph} height={glyph} viewBox="0 0 24 24" fill="currentColor" aria-hidden focusable={false}>
        {isGroup ? (
          <>
            <circle cx="7.5" cy="9" r="2.6" />
            <circle cx="16.5" cy="9" r="2.6" />
            <path d="M1.5 19.4c0-2.9 2.6-4.6 6-4.6s6 1.7 6 4.6z" />
            <path d="M12.6 15c.8-.32 1.75-.5 2.9-.5 3.4 0 6 1.7 6 4.6h-6.5" />
          </>
        ) : (
          <>
            <circle cx="12" cy="8.4" r="4.3" />
            <path d="M3.4 20.6c0-4.5 3.9-7.1 8.6-7.1s8.6 2.6 8.6 7.1z" />
          </>
        )}
      </svg>
    </span>
  );
}

/** AuthLayout is the branded shell for the signed-out flow (login / verify): a
 *  centred card with the product mark, a heading pair, the form, and the E2EE
 *  reassurance footer. */
function AuthLayout({
  title,
  subtitle,
  onSubmit,
  children,
}: {
  title: string;
  subtitle: string;
  onSubmit: (e: FormEvent) => void;
  children: ReactNode;
}) {
  return (
    <div className="auth">
      <form className="auth-card" onSubmit={onSubmit}>
        <div className="auth-brand">
          <span className="auth-mark" aria-hidden>
            <Icon name="chats" size={26} />
          </span>
          <span className="auth-brand-name">WhatsApp V2</span>
        </div>
        <div className="auth-heading">
          <h1>{title}</h1>
          <p className="muted">{subtitle}</p>
        </div>
        {children}
      </form>
      <p className="auth-foot">
        <Icon name="info" size={14} /> Your messages are end-to-end encrypted.
      </p>
    </div>
  );
}

/** Screen is the standard full-screen shell (U3): a sticky header carrying an
 *  optional back button, the title (+ subtitle) and trailing actions, over a
 *  scrolling body whose content sits in a readable centred column. Every full
 *  screen uses this so headers, padding and scroll behaviour match everywhere. */
export function Screen({
  title,
  subtitle,
  onBack,
  actions,
  wide,
  children,
}: {
  title: ReactNode;
  subtitle?: ReactNode;
  onBack?: () => void;
  actions?: ReactNode;
  wide?: boolean;
  children: ReactNode;
}) {
  return (
    <div className="screen">
      <header className="screen-head">
        {onBack ? (
          <button type="button" className="wa-icon" aria-label="Back" onClick={onBack}>
            <Icon name="back" size={22} />
          </button>
        ) : null}
        <div className="screen-title">
          <span className="screen-title-text">{title}</span>
          {subtitle ? <span className="screen-subtitle">{subtitle}</span> : null}
        </div>
        {actions ? <div className="screen-head-actions">{actions}</div> : null}
      </header>
      <div className="screen-body">
        <div className={`screen-inner${wide ? " wide" : ""}`}>{children}</div>
      </div>
    </div>
  );
}

/** Section is a grouped card of related content inside a Screen body. */
export function Section({
  title,
  desc,
  actions,
  flush,
  children,
}: {
  title?: ReactNode;
  desc?: ReactNode;
  actions?: ReactNode;
  flush?: boolean;
  children?: ReactNode;
}) {
  return (
    <section className={`section${flush ? " flush" : ""}`}>
      {title || actions ? (
        <div className="section-head">
          {title ? <h2 className="section-title">{title}</h2> : <span />}
          {actions ? <div className="inline">{actions}</div> : null}
        </div>
      ) : null}
      {desc ? <p className="section-desc">{desc}</p> : null}
      {children}
    </section>
  );
}

/** EmptyState is the one "nothing here yet" block used across every list. */
export function EmptyState({ icon, title, text, action }: { icon?: ReactNode; title: string; text?: ReactNode; action?: ReactNode }) {
  return (
    <div className="empty">
      {icon ? <div className="empty-icon" aria-hidden>{icon}</div> : null}
      <div className="empty-title">{title}</div>
      {text ? <p className="empty-text">{text}</p> : null}
      {action}
    </div>
  );
}

/** fmtRowTime renders a chat-list timestamp: clock for today, weekday within a
 *  week, else a short date — like the WhatsApp chat list. */
function fmtRowTime(ms: number): string {
  if (!ms) return "";
  const d = new Date(ms);
  const now = new Date();
  const sameDay = d.toDateString() === now.toDateString();
  if (sameDay) return d.toLocaleTimeString([], { hour: "numeric", minute: "2-digit" });
  const dayMs = 24 * 60 * 60 * 1000;
  if (now.getTime() - ms < 7 * dayMs) return d.toLocaleDateString([], { weekday: "short" });
  return d.toLocaleDateString([], { day: "2-digit", month: "2-digit", year: "2-digit" });
}

/** fmtClock renders a bubble timestamp (h:mm). */
function fmtClock(ms: number): string {
  return new Date(ms).toLocaleTimeString([], { hour: "numeric", minute: "2-digit" });
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
    <Screen title="New chat" onBack={onBack}>
      <Section desc="Enter the phone number of someone who already has an account. Numbers must be in international format.">
        <form className="stack" onSubmit={submit}>
          <div className="field">
            <label className="field-label" htmlFor="newchat-phone">
              Phone number
            </label>
            <input
              id="newchat-phone"
              className={`input${error ? " invalid" : ""}`}
              value={phone}
              onChange={(e) => setPhone(e.target.value)}
              placeholder="+14155550123"
              aria-label="Contact phone number in international format"
              inputMode="tel"
              autoFocus
              disabled={busy}
            />
            {error ? (
              <span className="field-error" role="alert">
                {error}
              </span>
            ) : (
              <span className="field-help">We'll start an end-to-end encrypted chat.</span>
            )}
          </div>
          <div className="actions end">
            <button type="button" className="btn ghost" onClick={onBack}>
              Cancel
            </button>
            <button className="btn" type="submit" disabled={busy}>
              {busy ? "Starting…" : "Start chat"}
            </button>
          </div>
        </form>
      </Section>
    </Screen>
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
    <Screen
      title="Your profile"
      onBack={onBack}
      actions={
        <button type="button" className="btn small ghost" onClick={onSettings}>
          <Icon name="settings" size={17} /> Settings
        </button>
      }
    >
      <section className="section profile-hero">
        <Avatar size={88} />
        <div className="profile-hero-text">
          <span className="profile-hero-name">{displayName.trim() || "Your name"}</span>
          {username.trim() ? <span className="profile-hero-handle">@{username.trim()}</span> : null}
          <span className="profile-hero-about">{about.trim() || "Hey there! I use WhatsApp V2"}</span>
        </div>
      </section>

      <Section title="Profile details" desc="Your name and about are visible to people you chat with, subject to the privacy settings below.">
        <form className="stack" onSubmit={save}>
          <div className="field">
            <label className="field-label" htmlFor="profile-name">
              Display name
            </label>
            <input
              id="profile-name"
              className="input"
              value={displayName}
              onChange={(e) => setDisplayName(e.target.value)}
              placeholder="Your name"
              aria-label="Display name"
              maxLength={100}
              disabled={busy}
            />
          </div>
          <div className="field">
            <label className="field-label" htmlFor="profile-username">
              Username
            </label>
            <input
              id="profile-username"
              className="input"
              value={username}
              onChange={(e) => setUsername(e.target.value)}
              placeholder="username"
              aria-label="Username"
              maxLength={30}
              disabled={busy}
            />
            <span className="field-help">Letters, numbers, underscore and dot. People can find you by this.</span>
          </div>
          <div className="field">
            <label className="field-label" htmlFor="profile-about">
              About
            </label>
            <input
              id="profile-about"
              className="input"
              value={about}
              onChange={(e) => setAbout(e.target.value)}
              placeholder="Hey there! I use WhatsApp V2"
              aria-label="About"
              maxLength={200}
              disabled={busy}
            />
          </div>
          {error ? (
            <p className="error" role="alert">
              {error}
            </p>
          ) : null}
          <div className="actions end">
            {saved ? (
              <span className="badge accent" role="status">
                ✓ Saved
              </span>
            ) : null}
            <button className="btn" type="submit" disabled={busy}>
              {busy ? "Saving…" : "Save changes"}
            </button>
          </div>
        </form>
      </Section>

      <Section title="Privacy" desc="Choose who can see each part of your profile.">
        <div className="stack">
          {PRIVACY_FIELDS.map((f) => (
            <div className="field-row" key={f.key}>
              <label className="field-label" htmlFor={`privacy-${f.key}`}>
                {f.label}
              </label>
              <select
                id={`privacy-${f.key}`}
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
            </div>
          ))}
        </div>
      </Section>

      <Section title={`Blocked contacts${blocked.length ? ` (${blocked.length})` : ""}`}>
        {blocked.length === 0 ? (
          <p className="section-desc">You haven't blocked anyone.</p>
        ) : (
          <ul className="list">
            {blocked.map((id) => (
              <li key={id} className="row static">
                <Avatar size={38} />
                <div className="row-main">
                  <div className="row-title">{services.nameForUser(id)}</div>
                </div>
                <button className="btn small ghost" onClick={() => void unblock(id)}>
                  Unblock
                </button>
              </li>
            ))}
          </ul>
        )}
      </Section>
    </Screen>
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
    <AuthLayout
      title="Enter your phone number"
      subtitle="We'll send a one-time code to confirm it's you."
      onSubmit={submit}
    >
      <div className="field">
        <label className="field-label" htmlFor="login-phone">
          Phone number
        </label>
        <input
          id="login-phone"
          className={`input${error ? " invalid" : ""}`}
          value={phone}
          onChange={(e) => setPhone(e.target.value)}
          placeholder="+14155550123"
          aria-label="Phone number in international format"
          inputMode="tel"
          autoComplete="tel"
          autoFocus
          disabled={busy}
        />
        {error ? (
          <span className="field-error" role="alert">
            {error}
          </span>
        ) : (
          <span className="field-help">Include your country code, e.g. +1 for the US.</span>
        )}
      </div>
      <button className="btn large block" type="submit" disabled={busy}>
        {busy ? "Sending…" : "Send code"}
      </button>
    </AuthLayout>
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
    <AuthLayout
      title={needsPin ? "Enter your PIN" : "Enter your code"}
      subtitle={needsPin ? "This device needs your 2-step verification PIN." : `We sent a 6-digit code to ${phone}.`}
      onSubmit={submit}
    >
      {needsPin ? (
        <input
          className={`input code${error ? " invalid" : ""}`}
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
          className={`input code${error ? " invalid" : ""}`}
          value={code}
          onChange={(e) => setCode(e.target.value)}
          placeholder="······"
          aria-label="One-time verification code"
          inputMode="numeric"
          maxLength={6}
          autoComplete="one-time-code"
          autoFocus
          disabled={busy}
        />
      )}
      {error ? (
        <p className="error" role="alert">
          {error}
        </p>
      ) : null}
      <button className="btn large block" type="submit" disabled={busy}>
        {busy ? "Verifying…" : "Verify"}
      </button>
    </AuthLayout>
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
    <Screen title="Calls" subtitle="Call metadata only — never content" onBack={onBack}>
      {error ? (
        <p className="error" role="alert">
          {error}
        </p>
      ) : null}
      {!error && calls.length === 0 ? (
        <Section flush>
          <EmptyState
            icon={<Icon name="phone" size={26} />}
            title="No calls yet"
            text="Start a voice or video call from any chat and it will appear here."
          />
        </Section>
      ) : null}
      {calls.length > 0 ? (
        <Section flush>
          <ul className="list">
            {calls.map((c) => (
              <li key={c.id} className="row static">
                <span className={`call-glyph${c.outcome === "missed" ? " missed" : ""}`} aria-hidden>
                  <Icon name={c.kind === 2 ? "video" : "phone"} size={19} />
                </span>
                <div className="row-main">
                  <div className="row-line1">
                    <span className="row-title">{services.nameForUser(c.participants[0] ?? c.initiator)}</span>
                    <span className="row-time">{c.startedAt ? new Date(c.startedAt).toLocaleString() : ""}</span>
                  </div>
                  <div className="row-sub">
                    {c.kind === 2 ? "Video" : "Voice"} · {c.outcome}
                  </div>
                </div>
              </li>
            ))}
          </ul>
        </Section>
      ) : null}
    </Screen>
  );
}

export function ChatList({
  onOpen,
  onNew,
  onContacts,
  onNewGroup,
  onDiscover,
  activeId,
}: {
  onOpen: (id: string) => void;
  onNew: () => void;
  onContacts: () => void;
  onNewGroup: () => void;
  onDiscover?: () => void;
  activeId?: string;
}) {
  const { services } = useServices();
  const [items, setItems] = useState<ChatSummary[]>([]);
  const [showArchived, setShowArchived] = useState(false);
  const [showHidden, setShowHidden] = useState(false);
  const [filter, setFilter] = useState("");
  const [menuOpen, setMenuOpen] = useState(false);

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

  const nameOf = (id: string): string => services.groupNameOf(id) || services.peerNameOf(id) || "";

  const renderRow = (c: ChatSummary): ReactNode => {
    const id = c.conversationId;
    const unread = services.unreadCount(id);
    const muted = services.isMuted(id);
    const fav = services.isFavorite(id);
    const archived = services.isArchived(id);
    const isGroup = !!services.groupNameOf(id);
    const name = nameOf(id) || c.title || id.slice(0, 12);
    const stop = (fn: () => void) => (e: { stopPropagation: () => void }) => {
      e.stopPropagation();
      fn();
    };
    return (
      <li
        key={id}
        className={`row${id === activeId ? " active" : ""}`}
        role="button"
        tabIndex={0}
        onClick={() => onOpen(id)}
        onKeyDown={onActivate(() => onOpen(id))}
      >
        <Avatar name={name} id={id} emoji={isGroup ? "👥" : undefined} size={49} />
        <div className="row-main">
          <div className="row-line1">
            <span className="row-title">
              {fav && <span title="Favorite" style={{ marginRight: 4 }}>⭐</span>}
              {name}
              {muted && <span title="Muted" style={{ marginLeft: 6, opacity: 0.6 }}>🔇</span>}
            </span>
            <span className="row-time">{fmtRowTime(c.updatedAt)}</span>
          </div>
          <div className="row-line2">
            <span className="row-sub">{c.lastPreview || "No messages yet"}</span>
            <span className="row-right">
              {unread > 0 && (
                <span className="unread-badge" aria-label={`${unread} unread`} style={muted ? { background: "#8696a0" } : undefined}>
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
            </span>
          </div>
        </div>
      </li>
    );
  };

  const q = filter.trim().toLowerCase();
  const match = (c: ChatSummary): boolean =>
    q === "" || nameOf(c.conversationId).toLowerCase().includes(q) || (c.title || "").toLowerCase().includes(q) || (c.lastPreview || "").toLowerCase().includes(q);
  // Hidden chats (T10.01) are kept out of the main sections, in a collapsible
  // group at the bottom.
  const shown = items.filter(match).filter((c) => !services.isHidden(c.conversationId));
  const hidden = items.filter(match).filter((c) => services.isHidden(c.conversationId));
  const favorites = shown.filter((c) => services.isFavorite(c.conversationId) && !services.isArchived(c.conversationId));
  const archived = shown.filter((c) => services.isArchived(c.conversationId));
  const normal = shown.filter((c) => !services.isFavorite(c.conversationId) && !services.isArchived(c.conversationId));

  return (
    <div className="pane">
      <div className="wa-list-head">
        <span className="wa-list-title">Chats</span>
        <span className="wa-list-actions">
          <button className="wa-icon" onClick={onContacts} aria-label="Contacts" title="Contacts">
            <Icon name="contacts" size={22} />
          </button>
          <button className="wa-icon" onClick={onNew} aria-label="New chat" title="New chat">
            <Icon name="newchat" size={22} />
          </button>
          <span className="wa-menu-wrap">
            <button className="wa-icon" onClick={() => setMenuOpen((v) => !v)} aria-label="Menu" title="Menu" aria-expanded={menuOpen}>
              <Icon name="menu" size={22} />
            </button>
            {menuOpen ? (
              <>
                <div className="wa-menu-backdrop" onClick={() => setMenuOpen(false)} />
                <div className="wa-menu" role="menu">
                  <button className="menu-item" role="menuitem" onClick={() => { setMenuOpen(false); onNewGroup(); }}>New group</button>
                  <button className="menu-item" role="menuitem" onClick={() => { setMenuOpen(false); onContacts(); }}>Contacts</button>
                  {onDiscover ? <button className="menu-item" role="menuitem" onClick={() => { setMenuOpen(false); onDiscover(); }}>Discover</button> : null}
                </div>
              </>
            ) : null}
          </span>
        </span>
      </div>
      <div className="wa-search">
        <input
          value={filter}
          onChange={(e) => setFilter(e.target.value)}
          placeholder="Search or start a new chat"
          aria-label="Search chats"
        />
      </div>
      {items.length === 0 ? (
        <p className="muted center">No conversations yet. Start one with the ✏️ button.</p>
      ) : shown.length === 0 ? (
        <p className="muted center">No chats match “{filter}”.</p>
      ) : (
        <ul className="list">
          {favorites.length > 0 && <li className="list-section">Favorites</li>}
          {favorites.map(renderRow)}
          {favorites.length > 0 && normal.length > 0 && <li className="list-section">Chats</li>}
          {normal.map(renderRow)}
          {archived.length > 0 && (
            <li className="list-section" role="button" tabIndex={0} onClick={() => setShowArchived((v) => !v)} onKeyDown={onActivate(() => setShowArchived((v) => !v))} style={{ cursor: "pointer" }}>
              <span>🗄 Archived ({archived.length})</span>
              <span>{showArchived ? "▲" : "▼"}</span>
            </li>
          )}
          {showArchived && archived.map(renderRow)}
          {hidden.length > 0 && (
            <li className="list-section" role="button" tabIndex={0} onClick={() => setShowHidden((v) => !v)} onKeyDown={onActivate(() => setShowHidden((v) => !v))} style={{ cursor: "pointer" }}>
              <span>🙈 Hidden ({hidden.length})</span>
              <span>{showHidden ? "▲" : "▼"}</span>
            </li>
          )}
          {showHidden && hidden.map(renderRow)}
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
        <button className="wa-icon wa-back" onClick={onBack} aria-label="Back" title="Back">
          <Icon name="back" size={24} />
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
    <li key={u.userId} className="row static">
      <Avatar size={40} />
      <div className="row-main">
        <div className="row-title">@{u.username}</div>
      </div>
      <div className="row-right">
        <button
          className={`wa-icon${favIds.has(u.userId) ? " on" : ""}`}
          onClick={() => void toggleFav(u)}
          aria-label={favIds.has(u.userId) ? "Remove favorite" : "Add favorite"}
          title={favIds.has(u.userId) ? "Unfavorite" : "Favorite"}
        >
          <Icon name="star" size={18} />
        </button>
        <button className="btn small secondary" onClick={() => void message(u.userId)}>
          Message
        </button>
      </div>
    </li>
  );

  return (
    <Screen title="Contacts" onBack={onBack}>
      {error ? (
        <p className="error" role="alert">
          {error}
        </p>
      ) : null}

      <Section title="Find people" desc="Search anyone on WhatsApp V2 by their username.">
        <input
          className="input"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="Search by username…"
          aria-label="Search by username"
        />
        {query.trim().length >= 2 ? (
          results.length === 0 ? (
            <p className="section-desc">No users match “{query.trim()}”.</p>
          ) : (
            <ul className="list">{results.map(userRow)}</ul>
          )
        ) : null}
      </Section>

      <Section title={`Favorites${favorites.length ? ` (${favorites.length})` : ""}`}>
        {favorites.length === 0 ? (
          <p className="section-desc">Star someone to keep them handy here.</p>
        ) : (
          <ul className="list">{favorites.map(userRow)}</ul>
        )}
      </Section>

      <Section title="Find by phone" desc="Paste numbers in international format, one per line. Only a peppered hash of each number is sent — never the numbers themselves.">
        <textarea
          className="input"
          rows={3}
          value={phones}
          onChange={(e) => setPhones(e.target.value)}
          placeholder={"+14155550123\n+919876543210"}
          aria-label="Phone numbers"
        />
        <div className="actions">
          <button className="btn small" onClick={() => void checkPhones()} disabled={busy || phones.trim() === ""}>
            {busy ? "Checking…" : "Check numbers"}
          </button>
        </div>
        {matched !== null ? (
          matched.length === 0 ? (
            <p className="section-desc">None of those numbers are on WhatsApp V2 yet.</p>
          ) : (
            <ul className="list">{matched.map((m) => userRow({ userId: m.userId, username: m.username }))}</ul>
          )
        ) : null}
      </Section>

      <Section title="Invite a friend" desc="Share a personal link so someone can join and start a chat with you.">
        {invite ? (
          <div className="inline">
            <input
              className="input mono"
              readOnly
              value={invite.url}
              aria-label="Invite link"
              onFocus={(e) => e.currentTarget.select()}
              style={{ flex: 1, minWidth: 220 }}
            />
            <button className="btn small secondary" onClick={() => void copyInvite()}>
              {copied ? "Copied ✓" : "Copy link"}
            </button>
          </div>
        ) : (
          <div className="actions">
            <button className="btn small" onClick={() => void makeInvite()}>
              Create invite link
            </button>
          </div>
        )}
      </Section>
    </Screen>
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
    <Screen
      title="New group"
      onBack={onBack}
      actions={
        <button className="btn small" onClick={() => void create()} disabled={busy}>
          {busy ? "Creating…" : "Create group"}
        </button>
      }
    >
      <Section title="Group details">
        <div className="field">
          <label className="field-label" htmlFor="group-name">
            Group name
          </label>
          <input
            id="group-name"
            className={`input${error ? " invalid" : ""}`}
            value={name}
            onChange={(e) => setName(e.target.value)}
            placeholder="Weekend Trip"
            aria-label="Group name"
            maxLength={100}
            disabled={busy}
          />
        </div>
        <div className="field">
          <label className="field-label" htmlFor="group-desc">
            Description <span className="muted">(optional)</span>
          </label>
          <input
            id="group-desc"
            className="input"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            placeholder="What's this group about?"
            aria-label="Group description"
            maxLength={500}
            disabled={busy}
          />
        </div>
      </Section>

      <Section title={`Members (${picked.length})`} desc="Add people by username. You can always add more once the group exists.">
        {picked.length > 0 ? (
          <div className="inline">
            {picked.map((p) => (
              <button
                key={p.userId}
                className="chip on"
                onClick={() => setPicked((s) => s.filter((x) => x.userId !== p.userId))}
                title="Remove"
              >
                @{p.username} <span aria-hidden>✕</span>
              </button>
            ))}
          </div>
        ) : null}
        <input
          className="input"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="Add members by username…"
          aria-label="Add members"
        />
        {query.trim().length >= 2 ? (
          <ul className="list">
            {results
              .filter((r) => !pickedIds.has(r.userId))
              .map((r) => (
                <li key={r.userId} className="row static">
                  <Avatar size={38} />
                  <div className="row-main">
                    <div className="row-title">@{r.username}</div>
                  </div>
                  <button
                    className="btn small secondary"
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
        ) : null}
      </Section>

      {error ? (
        <p className="error" role="alert">
          {error}
        </p>
      ) : null}
    </Screen>
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
    <Screen title="Group info" onBack={onBack}>
      <section className="section profile-hero">
        <Avatar size={80} group />
        <div className="profile-hero-text">
          <span className="profile-hero-name">{group.name}</span>
          <span className="profile-hero-about">
            {members.length} member{members.length === 1 ? "" : "s"}
          </span>
          {group.description ? <span className="profile-hero-about">{group.description}</span> : null}
          <span className="inline">
            <span className={`badge${myRole === "member" ? "" : " accent"}`}>You're {myRole === "owner" ? "the owner" : myRole === "admin" ? "an admin" : "a member"}</span>
          </span>
        </div>
      </section>

      {error ? (
        <p className="error" role="alert">
          {error}
        </p>
      ) : null}

      {iCanManage ? (
        <Section title="Group settings" desc="Control who can post and who can change the group's name, photo and description.">
          <label className="setting">
            <span className="setting-text">
              <span className="setting-title">Announcements only</span>
              <span className="setting-desc">Only admins can send messages to this group.</span>
            </span>
            <input
              type="checkbox"
              className="switch"
              checked={group.settings.announcements}
              onChange={(e) => void saveSettings({ announcements: e.target.checked })}
            />
          </label>
          <div className="field-row">
            <label className="field-label" htmlFor="grp-post">
              Who can post
            </label>
            <select id="grp-post" className="input" value={group.settings.who_can_post} onChange={(e) => void saveSettings({ who_can_post: e.target.value })}>
              <option value="all">Everyone</option>
              <option value="admins">Admins only</option>
            </select>
          </div>
          <div className="field-row">
            <label className="field-label" htmlFor="grp-edit">
              Who can edit group info
            </label>
            <select id="grp-edit" className="input" value={group.settings.who_can_edit_info} onChange={(e) => void saveSettings({ who_can_edit_info: e.target.value })}>
              <option value="all">Everyone</option>
              <option value="admins">Admins only</option>
            </select>
          </div>
        </Section>
      ) : null}

      <Section title={`Members (${members.length})`}>
        <ul className="list">
          {[...members]
            .sort((a, b) => (ROLE_RANK[b.role] ?? 0) - (ROLE_RANK[a.role] ?? 0))
            .map((m) => (
              <li key={m.userId} className="row static">
                <Avatar size={40} />
                <div className="row-main">
                  <div className="row-line1">
                    <span className="row-title">{services.nameForUser(m.userId)}</span>
                    {m.role !== "member" ? <span className="badge accent">{m.role}</span> : null}
                  </div>
                </div>
                <div className="row-right">
                  {iAmOwner && m.role === "member" ? (
                    <button className="btn small ghost" onClick={() => void guard(() => services.setGroupRole(conversationId, m.userId, 1))} title="Make admin">
                      Make admin
                    </button>
                  ) : null}
                  {iAmOwner && m.role === "admin" ? (
                    <button className="btn small ghost" onClick={() => void guard(() => services.setGroupRole(conversationId, m.userId, 0))} title="Demote to member">
                      Demote
                    </button>
                  ) : null}
                  {iCanManage && m.role !== "owner" ? (
                    <button className="btn small ghost danger" onClick={() => void guard(() => services.removeGroupMember(conversationId, m.userId))} title="Remove">
                      Remove
                    </button>
                  ) : null}
                </div>
              </li>
            ))}
        </ul>
      </Section>

      {iCanManage ? (
        <>
          <Section title="Add members">
            <input className="input" value={query} onChange={(e) => setQuery(e.target.value)} placeholder="Search by username…" aria-label="Add members" />
            {query.trim().length >= 2 ? (
              <ul className="list">
                {results
                  .filter((r) => !memberIds.has(r.userId))
                  .map((r) => (
                    <li key={r.userId} className="row static">
                      <Avatar size={38} />
                      <div className="row-main">
                        <div className="row-title">@{r.username}</div>
                      </div>
                      <button
                        className="btn small secondary"
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
            ) : null}
          </Section>

          <Section title="Invite link" desc="Anyone with this link can join the group.">
            {invite ? (
              <div className="inline">
                <input
                  className="input mono"
                  readOnly
                  value={invite}
                  aria-label="Group invite link"
                  onFocus={(e) => e.currentTarget.select()}
                  style={{ flex: 1, minWidth: 220 }}
                />
                <button
                  className="btn small secondary"
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
              <div className="actions">
                <button className="btn small" onClick={() => void makeInvite()}>
                  Create invite link
                </button>
              </div>
            )}
          </Section>
        </>
      ) : null}

      <Section title="Danger zone" desc="Leaving removes this group from your chats. Deleting removes it for everyone.">
        <div className="actions">
          <button
            className="btn outline"
            onClick={() => {
              if (window.confirm("Leave this group?")) void guard(async () => (await services.leaveGroup(conversationId), onLeft()));
            }}
          >
            Leave group
          </button>
          {iAmOwner ? (
            <button
              className="btn danger"
              onClick={() => {
                if (window.confirm("Delete this group for everyone? This can't be undone.")) void guard(async () => (await services.deleteGroup(conversationId), onLeft()));
              }}
            >
              Delete group
            </button>
          ) : null}
        </div>
      </Section>
    </Screen>
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
    <div className="pane">
      <div className="pane-head">
        <button className="wa-icon wa-back" onClick={onBack} aria-label="Back" title="Back">
          <Icon name="back" size={24} />
        </button>
        <span className="pane-head-title">Updates</span>
      </div>
      <ul className="list">
        <li className="list-section">Status</li>
        <li
          className="row"
          role="button"
          tabIndex={0}
          onClick={() => (mine.length ? setMode({ m: "view", author: me }) : setMode({ m: "compose" }))}
          onKeyDown={onActivate(() => (mine.length ? setMode({ m: "view", author: me }) : setMode({ m: "compose" })))}
        >
          <span className="status-avatar">
            <Avatar size={49} />
            {mine.length ? null : (
              <span className="status-add" aria-hidden>
                <Icon name="plus" size={14} />
              </span>
            )}
          </span>
          <div className="row-main">
            <div className="row-line1">
              <span className="row-title">My status</span>
            </div>
            <div className="row-line2">
              <span className="row-sub">{mine.length ? `${mine.length} update${mine.length > 1 ? "s" : ""} · tap to view` : "Tap to add status update"}</span>
              {mine.length ? (
                <span className="row-right">
                  <button
                    className="wa-icon"
                    aria-label="Add to my status"
                    title="Add status"
                    onClick={(e) => { e.stopPropagation(); setMode({ m: "compose" }); }}
                  >
                    <Icon name="camera" size={20} />
                  </button>
                </span>
              ) : null}
            </div>
          </div>
        </li>
        {others.length > 0 ? <li className="list-section">Recent updates</li> : null}
        {others.map(([author, stories]) => (
          <li
            key={author}
            className="row"
            role="button"
            tabIndex={0}
            onClick={() => setMode({ m: "view", author })}
            onKeyDown={onActivate(() => setMode({ m: "view", author }))}
          >
            <span className="status-avatar status-ring">
              <Avatar size={49} />
            </span>
            <div className="row-main">
              <div className="row-line1">
                <span className="row-title">{services.nameForUser(author)}</span>
              </div>
              <div className="row-line2">
                <span className="row-sub">{stories.length} update{stories.length > 1 ? "s" : ""}</span>
              </div>
            </div>
          </li>
        ))}
        {others.length === 0 ? <li className="status-empty">No status updates from your contacts yet.</li> : null}
      </ul>
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
/** SecuritySection (T10.02): passkeys (Face ID / Touch ID / Hello) as a 2FA +
 *  biometric-unlock factor, and the recent-sign-ins surface with a new-location
 *  flag. */
function SecuritySection() {
  const { services } = useServices();
  const [passkeys, setPasskeys] = useState<PasskeyInfo[]>([]);
  const [logins, setLogins] = useState<LoginInfo[]>([]);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  const load = useCallback(() => {
    void services.listPasskeys().then(setPasskeys).catch(() => {});
    void services.recentLogins().then(setLogins).catch(() => {});
  }, [services]);
  useEffect(() => {
    load();
  }, [load]);

  async function addPasskey(): Promise<void> {
    setErr(null);
    setBusy(true);
    try {
      await services.registerPasskey();
      load();
    } catch (e) {
      setErr(messageOf(e));
    } finally {
      setBusy(false);
    }
  }

  const fmt = (ms: number): string => new Date(ms).toLocaleString();

  return (
    <>
      <Section
        title="Passkeys"
        desc="Passkeys (Face ID / Touch ID / Windows Hello) add a phishing-resistant second factor and unlock your locked chats. The key never leaves this device."
        actions={
          services.passkeysSupported() ? (
            <button className="btn small" onClick={() => void addPasskey()} disabled={busy}>
              {busy ? "Waiting…" : "Add a passkey"}
            </button>
          ) : null
        }
      >
        {!services.passkeysSupported() ? <p className="field-help">Passkeys aren't supported in this browser.</p> : null}
        {err ? <p className="error">{err}</p> : null}
        {passkeys.length > 0 ? (
          <ul className="list">
            {passkeys.map((p) => (
              <li key={p.id} className="row static">
                <span className="avatar avatar-default" style={{ width: 40, height: 40 }}>
                  <Icon name="settings" size={22} />
                </span>
                <div className="row-main">
                  <div className="row-title">{p.name}</div>
                  <div className="row-sub">
                    Added {fmt(p.created_at_ms)}
                    {p.last_used_at_ms ? ` · used ${fmt(p.last_used_at_ms)}` : ""}
                  </div>
                </div>
                <button className="btn small ghost danger" onClick={() => void services.deletePasskey(p.id).then(load)}>
                  Remove
                </button>
              </li>
            ))}
          </ul>
        ) : (
          <p className="field-help">No passkeys yet.</p>
        )}
      </Section>

      <Section title="Recent sign-ins" desc="Every sign-in to your account. Anything you don't recognise is worth revoking in Devices.">
        {logins.length === 0 ? (
          <p className="section-desc">No recent sign-ins recorded yet.</p>
        ) : (
          <ul className="list">
            {logins.map((l, i) => (
              <li key={i} className="row static">
                <div className="row-main">
                  <div className="row-line1">
                    <span className="row-title">{l.ip || "unknown IP"}</span>
                    {l.suspicious ? <span className="badge warning">new location</span> : null}
                    <span className="row-time">{fmt(l.at_ms)}</span>
                  </div>
                  <div className="row-sub">{l.user_agent || "unknown device"}</div>
                </div>
              </li>
            ))}
          </ul>
        )}
      </Section>
    </>
  );
}

/** AiSection (T11.01): the AI-features consent + disclosure control. Off by
 *  default; on-device keeps everything local, an opt-in server mode discloses
 *  that content leaves the device, and an operator kill-switch can disable it. */
function AiSection() {
  const { services } = useServices();
  const [, force] = useState(0);
  const rerender = (): void => force((n) => n + 1);
  useEffect(() => {
    void services.loadAiConfig().then(rerender);
  }, [services]);

  if (!services.aiKillSwitchOn()) {
    return (
      <Section title="AI features">
        <p className="section-desc">AI features have been turned off by your administrator.</p>
      </Section>
    );
  }

  const s = services.getAiSettings();
  const pick = (mode: AiMode): void => {
    services.setAiMode(mode);
    if (mode === "on-device") services.setAiConsent("onDevice", true);
    rerender();
  };
  const modes: { key: AiMode; title: string; desc: string }[] = [
    { key: "off", title: "Off", desc: "No AI features run." },
    { key: "on-device", title: "On this device only", desc: "Everything stays local — nothing is sent anywhere." },
    ...(services.aiServerAvailable()
      ? [{ key: "server" as AiMode, title: "On a server (opt-in)", desc: "Content you use with AI leaves your device for that request." }]
      : []),
  ];

  return (
    <Section title="AI features" desc={disclosureFor(s.mode)}>
      <div className="choice-group" role="radiogroup" aria-label="AI mode">
        {modes.map((m) => (
          <label key={m.key} className={`choice${s.mode === m.key ? " on" : ""}`}>
            <input type="radio" name="ai-mode" checked={s.mode === m.key} onChange={() => pick(m.key)} />
            <span className="choice-text">
              <span className="choice-title">{m.title}</span>
              <span className="choice-desc">{m.desc}</span>
            </span>
          </label>
        ))}
      </div>
      {s.mode === "server" ? (
        <label className="setting">
          <span className="setting-text">
            <span className="setting-title">I understand what leaves my device</span>
            <span className="setting-desc">
              Content I use with AI is sent to the AI service and isn't end-to-end encrypted for that request.
            </span>
          </span>
          <input
            type="checkbox"
            className="switch"
            checked={s.consent.server}
            onChange={(e) => {
              services.setAiConsent("server", e.target.checked);
              rerender();
            }}
          />
        </label>
      ) : null}
      <p className="field-help">
        Smart replies, summaries and translation respect this setting — it controls whether they may run at all.
      </p>
    </Section>
  );
}

/** BotsSection lets the user register bots (public @handle + an https webhook),
 *  see their bots, reveal a freshly-rotated shared secret once, and delete them
 *  (T13.02). The secret signs webhook deliveries (X-WA-Signature: HMAC-SHA256). */
function BotsSection() {
  const { services } = useServices();
  const [bots, setBots] = useState<BotView[]>([]);
  const [handle, setHandle] = useState("");
  const [name, setName] = useState("");
  const [webhook, setWebhook] = useState("");
  const [secret, setSecret] = useState<{ id: string; value: string } | null>(null); // shown once
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(() => {
    void services.listBots().then(setBots).catch(() => {});
  }, [services]);
  useEffect(load, [load]);

  async function register(): Promise<void> {
    setError(null);
    setBusy(true);
    try {
      const res = await services.registerBot(handle.trim(), name.trim(), webhook.trim());
      setSecret({ id: res.bot.id, value: res.secret });
      setHandle("");
      setName("");
      setWebhook("");
      load();
    } catch (err) {
      setError(messageOf(err));
    } finally {
      setBusy(false);
    }
  }

  async function rotate(id: string): Promise<void> {
    try {
      setSecret({ id, value: await services.rotateBotSecret(id) });
    } catch (err) {
      window.alert(messageOf(err));
    }
  }

  async function remove(id: string): Promise<void> {
    if (!window.confirm("Delete this bot? Its webhook will stop receiving events.")) return;
    try {
      await services.deleteBot(id);
      if (secret?.id === id) setSecret(null);
      load();
    } catch (err) {
      window.alert(messageOf(err));
    }
  }

  return (
    <>
      <Section
        title="Register a bot"
        desc="Give your bot a public @handle and an https webhook. We deliver events signed with a shared secret (X-WA-Signature: HMAC-SHA256) so your bot can verify they came from us."
      >
        <div className="stack tight">
          <div className="field">
            <label className="field-label" htmlFor="bot-handle">
              Handle
            </label>
            <input
              id="bot-handle"
              className="input"
              placeholder="news_bot"
              value={handle}
              maxLength={32}
              onChange={(e) => setHandle(e.target.value.toLowerCase())}
            />
            <span className="field-help">Lowercase letters, numbers and underscore, 3–32 characters.</span>
          </div>
          <div className="field">
            <label className="field-label" htmlFor="bot-name">
              Display name
            </label>
            <input id="bot-name" className="input" placeholder="News Bot" value={name} maxLength={60} onChange={(e) => setName(e.target.value)} />
          </div>
          <div className="field">
            <label className="field-label" htmlFor="bot-hook">
              Webhook URL
            </label>
            <input id="bot-hook" className={`input${error ? " invalid" : ""}`} placeholder="https://your-bot.example/webhook" value={webhook} onChange={(e) => setWebhook(e.target.value)} />
            {error ? <span className="field-error">{error}</span> : <span className="field-help">Must be https — events are POSTed here.</span>}
          </div>
        </div>
        <div className="actions end">
          <button className="btn" disabled={busy || handle.trim() === "" || name.trim() === "" || webhook.trim() === ""} onClick={() => void register()}>
            Register bot
          </button>
        </div>
        {secret ? (
          <div className="callout accent">
            <div className="callout-title">Shared secret — copy it now, it's shown once</div>
            <code className="callout-code">{secret.value}</code>
            <div className="actions">
              <button className="btn small secondary" onClick={() => void navigator.clipboard?.writeText(secret.value).catch(() => {})}>
                Copy secret
              </button>
              <button className="btn small ghost" onClick={() => setSecret(null)}>
                Dismiss
              </button>
            </div>
          </div>
        ) : null}
      </Section>

      <Section title={`Your bots${bots.length ? ` (${bots.length})` : ""}`}>
        {bots.length === 0 ? (
          <p className="section-desc">You haven't registered any bots yet.</p>
        ) : (
          <ul className="list">
            {bots.map((b) => (
              <li key={b.id} className="row static">
                <div className="row-main">
                  <div className="row-line1">
                    <span className="row-title">@{b.handle}</span>
                    <span className="badge">{b.name}</span>
                  </div>
                  <div className="row-sub mono">{b.webhook_url}</div>
                </div>
                <div className="row-right">
                  <button className="btn small ghost" onClick={() => void rotate(b.id)}>
                    Rotate secret
                  </button>
                  <button className="btn small ghost danger" onClick={() => void remove(b.id)}>
                    Delete
                  </button>
                </div>
              </li>
            ))}
          </ul>
        )}
      </Section>
    </>
  );
}

/** minToHHMM/hhmmToMin convert a minute-of-day to/from an <input type="time">. */
function minToHHMM(min: number): string {
  if (min < 0) return "22:00";
  const h = Math.floor(min / 60);
  const m = min % 60;
  return `${String(h).padStart(2, "0")}:${String(m).padStart(2, "0")}`;
}
function hhmmToMin(v: string): number {
  const parts = v.split(":");
  const h = parseInt(parts[0] ?? "", 10);
  const m = parseInt(parts[1] ?? "", 10);
  if (Number.isNaN(h) || Number.isNaN(m)) return 0;
  return h * 60 + m;
}

/** NotificationsSection is the T14.01 multi-channel surface: extra delivery
 *  channels (email/SMS content-free nudges, desktop), quiet hours, sound/vibrate,
 *  and scheduled reminders. Server-authoritative, so it applies across devices. */
function NotificationsSection() {
  const { services } = useServices();
  const [prefs, setPrefs] = useState<NotifPrefs>(() => services.notifPrefs());
  const [quietOn, setQuietOn] = useState(false);
  const [saved, setSaved] = useState(false);
  const [scheduled, setScheduled] = useState<ScheduledNotif[]>([]);
  const [title, setTitle] = useState("");
  const [due, setDue] = useState("");
  const [err, setErr] = useState<string | null>(null);

  const loadScheduled = useCallback(() => {
    void services.scheduledNotifications().then(setScheduled).catch(() => {});
  }, [services]);
  useEffect(() => {
    void services.loadNotifPrefs().then((p) => {
      setPrefs(p);
      setQuietOn(p.quiet_start_min >= 0 && p.quiet_end_min >= 0);
    });
    loadScheduled();
  }, [services, loadScheduled]);

  const set = <K extends keyof NotifPrefs>(k: K, v: NotifPrefs[K]): void => {
    setPrefs((p) => ({ ...p, [k]: v }));
    setSaved(false);
  };

  async function save(): Promise<void> {
    const next: NotifPrefs = quietOn
      ? prefs
      : { ...prefs, quiet_start_min: -1, quiet_end_min: -1 };
    try {
      await services.saveNotifPrefs(next);
      setPrefs(next);
      setSaved(true);
    } catch (e) {
      window.alert(messageOf(e));
    }
  }

  async function addReminder(): Promise<void> {
    setErr(null);
    const ms = new Date(due).getTime();
    if (!title.trim() || Number.isNaN(ms) || ms <= Date.now()) {
      setErr("Enter a title and a future date/time.");
      return;
    }
    try {
      await services.scheduleNotification(title.trim(), ms);
      setTitle("");
      setDue("");
      loadScheduled();
    } catch (e) {
      setErr(messageOf(e));
    }
  }

  const channel = (key: "email" | "sms" | "desktop", label: string, hint: string): ReactNode => (
    <label className="setting">
      <span className="setting-text">
        <span className="setting-title">{label}</span>
        <span className="setting-desc">{hint}</span>
      </span>
      <input type="checkbox" className="switch" checked={prefs[key]} onChange={(e) => set(key, e.target.checked)} />
    </label>
  );

  return (
    <>
      <Section
        title="Delivery channels"
        desc="Extra ways to be alerted when you're away. Email and SMS are content-free nudges — they only say you have new activity, never the message itself."
        actions={
          <>
            {saved ? (
              <span className="badge accent" role="status">
                ✓ Saved
              </span>
            ) : null}
            <button className="btn small" onClick={() => void save()}>
              Save
            </button>
          </>
        }
      >
        {channel("desktop", "Desktop notifications", "Browser notifications while a client is open.")}
        {channel("email", "Email nudge", "A generic email when you have unread activity. Requires a configured mail relay.")}
        {channel("sms", "SMS nudge", "A last-resort text when you have unread activity. Requires a configured SMS gateway.")}

        <label className="setting">
          <span className="setting-text">
            <span className="setting-title">Quiet hours</span>
            <span className="setting-desc">Silence alerts during a daily window. Calls still ring through.</span>
          </span>
          <input
            type="checkbox"
            className="switch"
            checked={quietOn}
            onChange={(e) => {
              setQuietOn(e.target.checked);
              setSaved(false);
            }}
          />
        </label>
        {quietOn ? (
          <div className="inline">
            <label className="field-label" htmlFor="quiet-from">
              From
            </label>
            <input
              id="quiet-from"
              className="input"
              type="time"
              style={{ width: "auto" }}
              value={minToHHMM(prefs.quiet_start_min < 0 ? 1320 : prefs.quiet_start_min)}
              onChange={(e) => set("quiet_start_min", hhmmToMin(e.target.value))}
            />
            <label className="field-label" htmlFor="quiet-to">
              to
            </label>
            <input
              id="quiet-to"
              className="input"
              type="time"
              style={{ width: "auto" }}
              value={minToHHMM(prefs.quiet_end_min < 0 ? 420 : prefs.quiet_end_min)}
              onChange={(e) => set("quiet_end_min", hhmmToMin(e.target.value))}
            />
          </div>
        ) : null}

        <label className="setting">
          <span className="setting-text">
            <span className="setting-title">Sound</span>
            <span className="setting-desc">Play a short tone for new messages.</span>
          </span>
          <input type="checkbox" className="switch" checked={prefs.sound} onChange={(e) => set("sound", e.target.checked)} />
        </label>
        <label className="setting">
          <span className="setting-text">
            <span className="setting-title">Vibration</span>
            <span className="setting-desc">Where the device supports it.</span>
          </span>
          <input type="checkbox" className="switch" checked={prefs.vibrate} onChange={(e) => set("vibrate", e.target.checked)} />
        </label>
      </Section>

      <Section title="Scheduled reminders" desc="Get a content-free nudge to yourself at a set time.">
        <div className="inline">
          <input className="input" placeholder="Reminder title" value={title} maxLength={200} onChange={(e) => setTitle(e.target.value)} style={{ flex: 1, minWidth: 180 }} />
          <input className="input" type="datetime-local" value={due} onChange={(e) => setDue(e.target.value)} style={{ width: "auto" }} />
          <button className="btn small" onClick={() => void addReminder()}>
            Add
          </button>
        </div>
        {err ? <span className="field-error">{err}</span> : null}
        {scheduled.length > 0 ? (
          <ul className="list">
            {scheduled.map((n) => (
              <li key={n.id} className="row static">
                <div className="row-main">
                  <div className="row-line1">
                    <span className="row-title">{n.title}</span>
                    {n.fired ? <span className="badge">fired</span> : null}
                  </div>
                  <div className="row-sub">{new Date(n.due_at_ms).toLocaleString()}</div>
                </div>
                <button
                  className="btn small ghost danger"
                  onClick={() => void services.cancelScheduledNotification(n.id).then(loadScheduled).catch((e) => window.alert(messageOf(e)))}
                >
                  Cancel
                </button>
              </li>
            ))}
          </ul>
        ) : null}
      </Section>
    </>
  );
}

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
    <Screen title="Settings" subtitle="Appearance, privacy, notifications and devices" onBack={onBack} wide>
      <nav className="settings-nav" aria-label="Settings sections">
        {SETTINGS_SECTIONS.map((s) => (
          <button key={s.id} className="chip" onClick={() => document.getElementById(s.id)?.scrollIntoView({ behavior: "smooth", block: "start" })}>
            {s.label}
          </button>
        ))}
      </nav>

      {error ? (
        <p className="error" role="alert">
          {error}
        </p>
      ) : null}

      <section className="section" id="set-appearance">
        <div className="section-head">
          <h2 className="section-title">Appearance</h2>
        </div>
        <div className="setting">
          <span className="setting-text">
            <span className="setting-title">Theme</span>
            <span className="setting-desc">System follows your operating system's light/dark setting.</span>
          </span>
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
        </div>
      </section>

      <div id="set-notifications">
        <section className="section">
          <div className="section-head">
            <h2 className="section-title">Notifications</h2>
          </div>
          <label className="setting">
            <span className="setting-text">
              <span className="setting-title">Push notifications on this device</span>
              <span className="setting-desc">Wake this browser when a message arrives while it's closed.</span>
            </span>
            <input type="checkbox" className="switch" checked={pushOn} onChange={() => void togglePush()} />
          </label>
          <label className="setting">
            <span className="setting-text">
              <span className="setting-title">Mute all chats</span>
              <span className="setting-desc">Silence in-app alerts everywhere without changing per-chat settings.</span>
            </span>
            <input
              type="checkbox"
              className="switch"
              checked={globalMute}
              onChange={(e) => {
                services.setGlobalMute(e.target.checked);
                setGlobalMute(e.target.checked);
              }}
            />
          </label>
        </section>

        <NotificationsSection />

        <Section
          title="Recent notifications"
          actions={
            notifs.length > 0 ? (
              <button
                className="btn small ghost"
                onClick={() => {
                  services.clearNotifications();
                  setNotifs([]);
                }}
              >
                Clear history
              </button>
            ) : null
          }
        >
          {notifs.length === 0 ? (
            <p className="section-desc">Nothing recent.</p>
          ) : (
            <ul className="list">
              {notifs.slice(0, 20).map((n) => (
                <li key={n.id} className="row static">
                  <div className="row-main">
                    <div className="row-title">{n.title}</div>
                    <div className="row-sub">
                      {n.preview} · {formatLastSeen(n.ts)}
                    </div>
                  </div>
                </li>
              ))}
            </ul>
          )}
        </Section>
      </div>

      <div id="set-privacy">
        <SecuritySection />
      </div>

      <div id="set-ai">
        <AiSection />
      </div>

      <div id="set-messaging">
        <Section title="Saved replies" desc="Reusable messages you can insert from a chat's tools menu.">
          <div className="inline">
            <input className="input" placeholder="Title" value={tplTitle} style={{ maxWidth: 160 }} onChange={(e) => setTplTitle(e.target.value)} />
            <input className="input" placeholder="Message text" value={tplText} style={{ flex: 1, minWidth: 180 }} onChange={(e) => setTplText(e.target.value)} />
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
          {services.listTemplates().length === 0 ? (
            <p className="section-desc">No saved replies yet.</p>
          ) : (
            <ul className="list">
              {services.listTemplates().map((t) => (
                <li key={t.id} className="row static">
                  <div className="row-main">
                    <div className="row-title">{t.title}</div>
                    <div className="row-sub">{t.text}</div>
                  </div>
                  <button className="wa-icon" aria-label="Delete saved reply" title="Delete" onClick={() => services.removeTemplate(t.id)}>
                    <Icon name="trash" size={17} />
                  </button>
                </li>
              ))}
            </ul>
          )}
        </Section>

        <Section title="Auto-reply" desc="When on, incoming messages get one automatic reply per chat per hour. Chats you're actively viewing are skipped.">
          <label className="setting">
            <span className="setting-text">
              <span className="setting-title">Away auto-reply</span>
              <span className="setting-desc">Let people know you're away.</span>
            </span>
            <input
              type="checkbox"
              className="switch"
              checked={autoReply.enabled}
              onChange={(e) => {
                const next = { ...autoReply, enabled: e.target.checked };
                setAutoReplyState(next);
                services.setAutoReply(next.enabled, next.text);
              }}
            />
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
        </Section>

        <Section title="Scheduled messages">
          {services.scheduledMessages().length === 0 ? (
            <p className="section-desc">Nothing scheduled. Type a message in a chat and pick Schedule from the tools menu.</p>
          ) : (
            <ul className="list">
              {services.scheduledMessages().map((m) => (
                <li key={m.id} className="row static">
                  <div className="row-main">
                    <div className="row-title">{m.text.slice(0, 60)}</div>
                    <div className="row-sub">{new Date(m.sendAtMs).toLocaleString()}</div>
                  </div>
                  <button className="btn small ghost" aria-label="Cancel scheduled message" onClick={() => services.cancelScheduled(m.id)}>
                    Cancel
                  </button>
                </li>
              ))}
            </ul>
          )}
        </Section>
      </div>

      <div id="set-devices">
        <Section title={`Linked devices (${devices.length})`} desc="Devices signed in to your account. Revoke any you don't recognise.">
          <ul className="list">
            {devices.map((d) => (
              <li key={d.id} className="row static">
                {editing === d.id ? (
                  <>
                    <input className="input" value={editName} onChange={(e) => setEditName(e.target.value)} aria-label="Device name" maxLength={60} style={{ flex: 1 }} />
                    <button className="btn small" onClick={() => void saveName(d.id)}>
                      Save
                    </button>
                    <button className="wa-icon" aria-label="Cancel" onClick={() => setEditing(null)}>
                      <Icon name="close" size={17} />
                    </button>
                  </>
                ) : (
                  <>
                    <div className="row-main">
                      <div className="row-line1">
                        <span className="row-title">{d.name || d.platform || "Device"}</span>
                        {d.id === myId ? <span className="badge accent">this device</span> : null}
                        {d.isPrimary ? <span className="badge">primary</span> : null}
                      </div>
                      <div className="row-sub">
                        {d.platform}
                        {d.lastActiveMs ? ` · active ${formatLastSeen(d.lastActiveMs)}` : ""}
                      </div>
                    </div>
                    <div className="row-right">
                      <button
                        className="btn small ghost"
                        onClick={() => {
                          setEditing(d.id);
                          setEditName(d.name);
                        }}
                      >
                        Rename
                      </button>
                      <button className="btn small ghost danger" onClick={() => void revoke(d.id)}>
                        {d.id === myId ? "Sign out" : "Revoke"}
                      </button>
                    </div>
                  </>
                )}
              </li>
            ))}
          </ul>
        </Section>

        <Section title="Link a device">
          <p className="section-desc">
            On the new device, open WhatsApp V2 → Link a device, then scan its QR here. Your primary device signs
            the new device's key into your <span className="mono">signed device list</span> so all your devices
            trust it.
          </p>
          <p className="field-help">
            QR scanning and the device-list signature are wired through <span className="mono">@wa/crypto-wrapper</span>{" "}
            (deviceList) — the on-device linking seam.
          </p>
        </Section>
      </div>

      <div id="set-bots">
        <BotsSection />
      </div>

      <div id="set-account">
        <Section title="Chat backup" desc="End-to-end encrypted backups upload to your own storage, keyed by a password only you hold (Argon2id).">
          <p className="field-help">
            Create and restore are wired server-side (<span className="mono">/v1/backups</span>) — the client archive
            and key-derivation UI is the next step.
          </p>
        </Section>

        <Section title="Account" desc="Export your data or delete your account.">
          <p className="field-help">The account-lifecycle endpoints aren't exposed yet, so these remain on the roadmap.</p>
        </Section>
      </div>
    </Screen>
  );
}

/** The Settings sub-nav: jump targets for the grouped sections below. */
const SETTINGS_SECTIONS: { id: string; label: string }[] = [
  { id: "set-appearance", label: "Appearance" },
  { id: "set-notifications", label: "Notifications" },
  { id: "set-privacy", label: "Privacy & security" },
  { id: "set-ai", label: "AI" },
  { id: "set-messaging", label: "Messaging" },
  { id: "set-devices", label: "Devices" },
  { id: "set-bots", label: "Bots" },
  { id: "set-account", label: "Account" },
];

const CHANNEL_EMOJIS = ["👍", "❤️", "🔥", "😂", "🎉"];

function channelRow(c: ChannelInfo, onOpen: (id: string) => void): ReactNode {
  return (
    <li key={c.id} className="row" role="button" tabIndex={0} onClick={() => onOpen(c.id)} onKeyDown={onActivate(() => onOpen(c.id))}>
      <span className="entity-glyph" aria-hidden>
        <Icon name="channel" size={20} />
      </span>
      <div className="row-main">
        <div className="row-line1">
          <span className="row-title">{c.name}</span>
          {c.verified ? (
            <span className="badge accent" title="Verified">
              ✔ verified
            </span>
          ) : null}
        </div>
        <div className="row-sub">
          @{c.handle} · {c.followers} follower{c.followers === 1 ? "" : "s"}
          {c.description ? ` · ${c.description}` : ""}
        </div>
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
    <Screen
      title="Channels"
      subtitle="Broadcast feeds you can follow"
      onBack={onBack}
      actions={
        <span className="segmented">
          <button className={tab === "discover" ? "active" : ""} onClick={() => setTab("discover")}>
            Discover
          </button>
          <button className={tab === "create" ? "active" : ""} onClick={() => setTab("create")}>
            Create
          </button>
        </span>
      }
    >
      {tab === "discover" ? (
        <>
          <Section title="Find a channel">
            <input
              className="input"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Search channels by name…"
              aria-label="Search channels"
            />
            {query.trim().length >= 2 ? (
              results.length === 0 ? (
                <p className="section-desc">No channels match “{query.trim()}”.</p>
              ) : (
                <ul className="list">{results.map((c) => channelRow(c, onOpen))}</ul>
              )
            ) : null}
          </Section>

          {query.trim().length < 2 ? (
            <Section title="Popular">
              {discover.length === 0 ? (
                <EmptyState
                  icon={<Icon name="channel" size={26} />}
                  title="No channels yet"
                  text="Channels are one-to-many broadcast feeds. Create the first one."
                  action={
                    <button className="btn small" onClick={() => setTab("create")}>
                      Create a channel
                    </button>
                  }
                />
              ) : (
                <ul className="list">{discover.map((c) => channelRow(c, onOpen))}</ul>
              )}
            </Section>
          ) : null}
        </>
      ) : (
        <Section title="Create a channel" desc="Channel posts are visible to the server so they can be searched and delivered at scale — unlike your chats, they are not end-to-end encrypted.">
          <div className="field">
            <label className="field-label" htmlFor="ch-handle">
              Handle
            </label>
            <input id="ch-handle" className="input" value={handle} onChange={(e) => setHandle(e.target.value)} placeholder="my_channel" maxLength={30} />
            <span className="field-help">Lowercase letters, numbers and underscore. This is the channel's public @name.</span>
          </div>
          <div className="field">
            <label className="field-label" htmlFor="ch-name">
              Name
            </label>
            <input id="ch-name" className="input" value={name} onChange={(e) => setName(e.target.value)} placeholder="My Channel" maxLength={80} />
          </div>
          <div className="field">
            <label className="field-label" htmlFor="ch-desc">
              Description
            </label>
            <input id="ch-desc" className="input" value={description} onChange={(e) => setDescription(e.target.value)} placeholder="What's it about?" maxLength={500} />
          </div>
          <div className="field-row">
            <label className="field-label" htmlFor="ch-kind">
              Visibility
            </label>
            <select id="ch-kind" className="input" value={kind} onChange={(e) => setKind(e.target.value as "public" | "private")}>
              <option value="public">Public — anyone can find &amp; follow</option>
              <option value="private">Private — invite-only</option>
            </select>
          </div>
          {error ? (
            <p className="error" role="alert">
              {error}
            </p>
          ) : null}
          <div className="actions end">
            <button className="btn" onClick={() => void create()} disabled={busy || !handle.trim() || !name.trim()}>
              {busy ? "Creating…" : "Create channel"}
            </button>
          </div>
        </Section>
      )}
    </Screen>
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
    // Real-time push (T7.04): the gateway forwards a channel_event when a post
    // lands; refetch on it. The poll is now just a safety net for missed nudges.
    services.subscribeChannel(channelId);
    const unsub = services.onChannelEvent((id) => {
      if (id === channelId) load();
    });
    const h = setInterval(load, 20000);
    return () => {
      services.unsubscribeChannel(channelId);
      unsub();
      clearInterval(h);
    };
  }, [load, services, channelId]);

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
        <button className="wa-icon wa-back" onClick={onBack} aria-label="Back" title="Back">
          <Icon name="back" size={24} />
        </button>
        <span className="entity-glyph" aria-hidden>
          <Icon name="channel" size={20} />
        </span>
        <div className="thread-title">
          <span className="thread-name">
            {channel.name}
            {channel.verified ? <span className="badge accent" title="Verified">✔</span> : null}
            {channel.premium ? <span className="badge warning" title={`Premium · ${price}/mo`}>{price}/mo</span> : null}
          </span>
          <span className="thread-status">
            @{channel.handle} · {channel.followers} follower{channel.followers === 1 ? "" : "s"} · {channel.kind}
          </span>
        </div>
        {channel.myRole === "owner" ? (
          <button className="wa-icon" aria-label="Delete channel" title="Delete channel" onClick={() => { if (window.confirm("Delete this channel for everyone?")) void services.deleteChannel(channelId).then(onBack); }}>
            <Icon name="trash" size={19} />
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

/** SecretChatSheet (T10.01): the per-chat disappearing timer + the secret-chat
 *  toggles (lock / hide / screenshot-protection). */
function SecretChatSheet({ conversationId, onClose }: { conversationId: string; onClose: () => void }) {
  const { services } = useServices();
  const [, force] = useState(0);
  const rerender = (): void => force((n) => n + 1);
  const ttl = services.disappearingSeconds(conversationId);
  const toggle = (fn: () => void) => () => {
    fn();
    rerender();
  };
  return (
    <div className="sheet-backdrop" role="dialog" aria-modal="true" onClick={onClose}>
      <div className="sheet" onClick={(e) => e.stopPropagation()}>
        <strong>Disappearing messages</strong>
        <p className="muted">New messages vanish for everyone after the timer. The timer is end-to-end encrypted.</p>
        <div className="disappear-list">
          {DISAPPEARING_PRESETS.map((p) => (
            <button
              key={p.seconds}
              className={`disappear-opt${ttl === p.seconds ? " on" : ""}`}
              onClick={() => void services.setDisappearing(conversationId, p.seconds).then(rerender).catch((e) => window.alert(messageOf(e)))}
            >
              <span>{p.label}</span>
              {ttl === p.seconds ? <Icon name="check" size={18} /> : null}
            </button>
          ))}
        </div>
        <div className="secret-toggles">
          <label className="secret-toggle">
            <span>🔒 Lock this chat</span>
            <input type="checkbox" checked={services.isLocked(conversationId)} onChange={toggle(() => services.toggleLock(conversationId))} />
          </label>
          <label className="secret-toggle">
            <span>🙈 Hide from chat list</span>
            <input type="checkbox" checked={services.isHidden(conversationId)} onChange={toggle(() => services.toggleHidden(conversationId))} />
          </label>
          <label className="secret-toggle">
            <span>📸 Screenshot protection</span>
            <input type="checkbox" checked={services.isScreenshotProtected(conversationId)} onChange={toggle(() => services.toggleScreenshotProtection(conversationId))} />
          </label>
        </div>
        <button className="btn" onClick={onClose}>
          Done
        </button>
      </div>
    </div>
  );
}

/** LockGate covers a locked chat until the user unlocks — via a passkey/biometric
 *  assertion (T10.02) when available, else a tap for the session. */
function LockGate({ onUnlock }: { onUnlock: () => void }) {
  const { services } = useServices();
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  async function biometric(): Promise<void> {
    setErr(null);
    setBusy(true);
    try {
      if (await services.loginPasskey()) onUnlock();
      else setErr("Couldn’t verify with a passkey.");
    } catch {
      setErr("Biometric unlock failed.");
    } finally {
      setBusy(false);
    }
  }
  return (
    <div className="locked-gate">
      <div className="locked-lock" aria-hidden>🔒</div>
      <p>This chat is locked.</p>
      {services.passkeysSupported() ? (
        <button className="btn" onClick={() => void biometric()} disabled={busy}>
          {busy ? "Verifying…" : "Unlock with biometrics"}
        </button>
      ) : null}
      <button className="btn ghost" onClick={onUnlock}>Unlock</button>
      {err ? <p className="error">{err}</p> : null}
    </div>
  );
}

/** ReportSheet files a trust-and-safety report against the chat's peer (T10.03),
 *  optionally blocking them too. Only content the user consents to leaves the
 *  device — the report itself is metadata. */
function ReportSheet({ conversationId, onClose }: { conversationId: string; onClose: () => void }) {
  const { services } = useServices();
  const peer = services.peerOf(conversationId);
  const [reason, setReason] = useState(0);
  const [note, setNote] = useState("");
  const [alsoBlock, setAlsoBlock] = useState(true);
  const [busy, setBusy] = useState(false);
  const [err, setErr] = useState<string | null>(null);
  const reasons = ["Spam", "Harassment", "Scam or phishing", "Impersonation", "Other"];

  async function submit(): Promise<void> {
    if (!peer) return;
    setErr(null);
    setBusy(true);
    try {
      await services.reportUser(peer, reason, note.trim() || undefined);
      if (alsoBlock) await services.blockUser(peer);
      onClose();
      window.alert("Thanks — your report was sent to our safety team.");
    } catch (e) {
      setErr(messageOf(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <div className="sheet-backdrop" role="dialog" aria-modal="true" onClick={onClose}>
      <div className="sheet" onClick={(e) => e.stopPropagation()}>
        <strong>Report {services.peerNameOf(conversationId) || "contact"}</strong>
        {!peer ? (
          <>
            <p className="muted">Reporting is available in direct chats.</p>
            <button className="btn" onClick={onClose}>Close</button>
          </>
        ) : (
          <>
            <div className="secret-toggles" style={{ borderTop: "none", paddingTop: 0 }}>
              {reasons.map((r, i) => (
                <label key={i} className="secret-toggle">
                  <span>{r}</span>
                  <input type="radio" name="report-reason" checked={reason === i} onChange={() => setReason(i)} />
                </label>
              ))}
            </div>
            <textarea className="input" rows={2} placeholder="Add details (optional)" value={note} maxLength={500} onChange={(e) => setNote(e.target.value)} />
            <label className="secret-toggle">
              <span>Also block this contact</span>
              <input type="checkbox" checked={alsoBlock} onChange={(e) => setAlsoBlock(e.target.checked)} />
            </label>
            {err ? <p className="error">{err}</p> : null}
            <div style={{ display: "flex", gap: 8 }}>
              <button className="btn ghost" onClick={onClose} disabled={busy}>Cancel</button>
              <button className="btn danger" onClick={() => void submit()} disabled={busy}>{busy ? "Reporting…" : "Report"}</button>
            </div>
          </>
        )}
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

/** InteractiveBubble renders an interactive message: text, an optional card, and
 *  up to three tap-able buttons. A "reply" button sends its payload/label back as
 *  a normal message; a "url" button opens a link; a "callback" is a bot ping (no
 *  client action in a user↔user thread). */
function InteractiveBubble({ msg, onQuickReply }: { msg: InteractiveMessage; onQuickReply?: (text: string) => void }) {
  const tap = (b: InteractiveButton): void => {
    if (b.kind === "url" && b.url) {
      window.open(b.url, "_blank", "noopener,noreferrer");
      return;
    }
    if (b.kind === "callback") return; // a bot handles callbacks server-side
    onQuickReply?.(replyTextFor(b)); // "reply": send the payload/label as a message
  };
  return (
    <div className="interactive">
      {msg.card ? (
        <div className="interactive-card">
          {msg.card.imageUrl ? <img className="interactive-card-img" src={msg.card.imageUrl} alt="" /> : null}
          <div className="interactive-card-body">
            <strong>{msg.card.title}</strong>
            {msg.card.subtitle ? <span className="muted">{msg.card.subtitle}</span> : null}
          </div>
        </div>
      ) : null}
      <div className="interactive-text"><RichText text={msg.text} /></div>
      <div className="interactive-buttons">
        {msg.buttons.map((b) => (
          <button key={b.id} className="interactive-btn" onClick={() => tap(b)}>
            {b.kind === "url" ? "🔗 " : ""}{b.label}
          </button>
        ))}
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

/** InteractiveComposer builds an interactive message: a body + 1–3 quick-reply
 *  buttons. Each button sends its label back as a reply when tapped (T13.02). */
function InteractiveComposer({ onSend, onClose }: { onSend: (text: string, buttons: InteractiveButton[]) => void; onClose: () => void }) {
  const [text, setText] = useState("");
  const [labels, setLabels] = useState<string[]>(["", ""]);
  const buttons: InteractiveButton[] = labels
    .map((l) => l.trim())
    .filter((l) => l !== "")
    .map((l, i) => ({ id: `b${i}`, label: l, kind: "reply" as const }));
  const err = validateInteractive(text, buttons);
  const setLabel = (i: number, v: string): void => setLabels((ls) => ls.map((l, j) => (j === i ? v : l)));
  return (
    <div className="sheet-backdrop" role="dialog" aria-modal="true" onClick={onClose}>
      <div className="sheet" onClick={(e) => e.stopPropagation()}>
        <strong>Interactive message</strong>
        <textarea className="input" rows={2} placeholder="Message text" value={text} autoFocus onChange={(e) => setText(e.target.value)} />
        <span className="muted" style={{ fontSize: "0.8rem" }}>Buttons (1–3) — tapping one sends its label back as a reply.</span>
        {labels.map((l, i) => (
          <div key={i} style={{ display: "flex", gap: 6 }}>
            <input className="input" placeholder={`Button ${i + 1}`} value={l} maxLength={40} onChange={(e) => setLabel(i, e.target.value)} />
            {labels.length > 1 ? (
              <button className="btn small ghost" aria-label="Remove button" onClick={() => setLabels((ls) => ls.filter((_, j) => j !== i))}>×</button>
            ) : null}
          </div>
        ))}
        {labels.length < 3 ? (
          <button className="btn small ghost" onClick={() => setLabels((ls) => [...ls, ""])}>＋ Add button</button>
        ) : null}
        <div style={{ display: "flex", gap: 8, justifyContent: "flex-end", alignItems: "center" }}>
          {err ? <span className="muted" style={{ fontSize: "0.78rem", marginRight: "auto" }}>{err}</span> : null}
          <button className="btn ghost" onClick={onClose}>Cancel</button>
          <button className="btn" disabled={!!err} onClick={() => onSend(text.trim(), buttons)}>Send</button>
        </div>
      </div>
    </div>
  );
}

/** Communities screen (T8.02): discover/search public communities, or create one. */
export function Communities({ onOpen, onBack }: { onOpen: (id: string) => void; onBack: () => void }) {
  const { services } = useServices();
  const [tab, setTab] = useState<"discover" | "create">("discover");
  const [query, setQuery] = useState("");
  const [discover, setDiscover] = useState<CommunitySummary[]>([]);
  const [results, setResults] = useState<CommunitySummary[]>([]);
  const [name, setName] = useState("");
  const [description, setDescription] = useState("");
  const [kind, setKind] = useState<"public" | "private">("public");
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState<string | null>(null);

  useEffect(() => {
    let alive = true;
    services.discoverCommunities().then((c) => alive && setDiscover(c)).catch(() => {});
    return () => {
      alive = false;
    };
  }, [services]);

  useEffect(() => {
    if (query.trim().length < 2) {
      setResults([]);
      return;
    }
    let alive = true;
    const h = setTimeout(() => services.searchCommunities(query).then((r) => alive && setResults(r)).catch(() => {}), 200);
    return () => {
      alive = false;
      clearTimeout(h);
    };
  }, [query, services]);

  async function create(): Promise<void> {
    setBusy(true);
    setError(null);
    try {
      onOpen(await services.createCommunity(name.trim(), description.trim(), kind));
    } catch (err) {
      setError(messageOf(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Screen
      title="Communities"
      subtitle="Groups, announcements and events under one roof"
      onBack={onBack}
      actions={
        <span className="segmented">
          <button className={tab === "discover" ? "active" : ""} onClick={() => setTab("discover")}>
            Discover
          </button>
          <button className={tab === "create" ? "active" : ""} onClick={() => setTab("create")}>
            Create
          </button>
        </span>
      }
    >
      {tab === "discover" ? (
        <>
          <Section title="Find a community">
            <input
              className="input"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
              placeholder="Search communities by name…"
              aria-label="Search communities"
            />
            {query.trim().length >= 2 ? (
              results.length === 0 ? (
                <p className="section-desc">No communities match “{query.trim()}”.</p>
              ) : (
                <ul className="list">{results.map((c) => communityRow(c, onOpen))}</ul>
              )
            ) : null}
          </Section>

          {query.trim().length < 2 ? (
            <Section title="Popular">
              {discover.length === 0 ? (
                <EmptyState
                  icon={<Icon name="community" size={26} />}
                  title="No communities yet"
                  text="A community groups related chats together with announcements and a shared calendar."
                  action={
                    <button className="btn small" onClick={() => setTab("create")}>
                      Create a community
                    </button>
                  }
                />
              ) : (
                <ul className="list">{discover.map((c) => communityRow(c, onOpen))}</ul>
              )}
            </Section>
          ) : null}
        </>
      ) : (
        <Section title="Create a community">
          <div className="field">
            <label className="field-label" htmlFor="co-name">
              Name
            </label>
            <input id="co-name" className="input" value={name} onChange={(e) => setName(e.target.value)} placeholder="My Community" maxLength={80} />
          </div>
          <div className="field">
            <label className="field-label" htmlFor="co-desc">
              Description
            </label>
            <input id="co-desc" className="input" value={description} onChange={(e) => setDescription(e.target.value)} placeholder="What's it about?" maxLength={500} />
          </div>
          <div className="field-row">
            <label className="field-label" htmlFor="co-kind">
              Visibility
            </label>
            <select id="co-kind" className="input" value={kind} onChange={(e) => setKind(e.target.value as "public" | "private")}>
              <option value="public">Public — anyone can find &amp; join</option>
              <option value="private">Private — invite-only</option>
            </select>
          </div>
          {error ? (
            <p className="error" role="alert">
              {error}
            </p>
          ) : null}
          <div className="actions end">
            <button className="btn" onClick={() => void create()} disabled={busy || !name.trim()}>
              {busy ? "Creating…" : "Create community"}
            </button>
          </div>
        </Section>
      )}
    </Screen>
  );
}

function communityRow(c: CommunitySummary, onOpen: (id: string) => void): ReactNode {
  return (
    <li key={c.id} className="row" role="button" tabIndex={0} onClick={() => onOpen(c.id)} onKeyDown={onActivate(() => onOpen(c.id))}>
      <span className="entity-glyph" aria-hidden>
        <Icon name="community" size={20} />
      </span>
      <div className="row-main">
        <div className="row-title">{c.name}</div>
        <div className="row-sub">
          {c.memberCount} member{c.memberCount === 1 ? "" : "s"}
          {c.description ? ` · ${c.description}` : ""}
        </div>
      </div>
    </li>
  );
}

/** CommunityScreen (T8.02): a community's home — announcements, grouped groups,
 *  shared calendar, members/roles, and admin moderation. */
export function CommunityScreen({ communityId, onBack, onOpenGroup }: { communityId: string; onBack: () => void; onOpenGroup: (groupId: string) => void }) {
  const { services } = useServices();
  const [community, setCommunity] = useState<CommunityInfo | null>(null);
  const [groupIds, setGroupIds] = useState<string[]>([]);
  const [events, setEvents] = useState<CommunityEvent[]>([]);
  const [members, setMembers] = useState<CommunityMember[]>([]);
  const [evTitle, setEvTitle] = useState("");
  const [evWhen, setEvWhen] = useState("");
  const [addGroupOpen, setAddGroupOpen] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const load = useCallback(() => {
    void services.getCommunity(communityId).then(setCommunity).catch(() => {});
    void services.communityGroupIds(communityId).then((ids) => {
      setGroupIds(ids);
      for (const id of ids) void services.loadGroup(id).catch(() => {}); // resolve names
    }).catch(() => {});
    void services.communityEvents(communityId).then(setEvents).catch(() => {});
    void services.communityMembers(communityId).then(setMembers).catch(() => {});
  }, [services, communityId]);
  useEffect(() => {
    load();
    return services.onChange(load);
  }, [load, services]);

  if (!community) return <p className="muted center">Loading…</p>;
  const isAdmin = community.myRole === "owner" || community.myRole === "admin";
  const isOwner = community.myRole === "owner";
  const isMember = community.myRole !== "";

  const guard = (fn: () => Promise<void>) => (): void => {
    setError(null);
    void fn().catch((e) => setError(messageOf(e)));
  };

  async function createEvent(): Promise<void> {
    const title = evTitle.trim();
    const ms = evWhen ? new Date(evWhen).getTime() : 0;
    if (!title || !ms) return;
    await services.createCommunityEvent(communityId, title, "", ms);
    setEvTitle("");
    setEvWhen("");
  }

  return (
    <Screen
      title={community.name}
      subtitle={`${community.memberCount} member${community.memberCount === 1 ? "" : "s"} · ${community.groupCount} group${community.groupCount === 1 ? "" : "s"}`}
      onBack={onBack}
      actions={
        <>
          {!isMember ? (
            <button className="btn small" onClick={guard(() => services.joinCommunity(communityId))}>
              Join
            </button>
          ) : !isOwner ? (
            <button className="btn small ghost" onClick={guard(() => services.leaveCommunity(communityId))}>
              Leave
            </button>
          ) : null}
        </>
      }
    >
      <section className="section profile-hero">
        <span className="entity-glyph" style={{ width: 72, height: 72 }} aria-hidden>
          <Icon name="community" size={30} />
        </span>
        <div className="profile-hero-text">
          <span className="profile-hero-name">{community.name}</span>
          <span className="inline">
            <span className="badge">{community.kind}</span>
            {community.myRole ? <span className="badge accent">you: {community.myRole}</span> : null}
          </span>
          {community.description ? <span className="profile-hero-about">{community.description}</span> : null}
        </div>
      </section>

      {error ? (
        <p className="error" role="alert">
          {error}
        </p>
      ) : null}

      <Section
        title="Announcements"
        desc="The community-wide group every member is in."
        actions={
          <button className="btn small secondary" onClick={() => onOpenGroup(community.announcementGroupId)}>
            Open
          </button>
        }
      />

      <Section
        title={`Groups (${groupIds.length})`}
        actions={
          isAdmin ? (
            <button className="btn small ghost" onClick={() => setAddGroupOpen((v) => !v)}>
              {addGroupOpen ? "Cancel" : "Add a group"}
            </button>
          ) : null
        }
      >
        {groupIds.length === 0 ? <p className="section-desc">No groups linked yet.</p> : null}
        {groupIds.length > 0 ? (
          <ul className="list">
            {groupIds.map((gid) => (
              <li key={gid} className="row" role="button" tabIndex={0} onClick={() => onOpenGroup(gid)} onKeyDown={onActivate(() => onOpenGroup(gid))}>
                <Avatar size={40} group />
                <div className="row-main">
                  <div className="row-title">{services.groupNameOf(gid) || gid.slice(0, 12)}</div>
                </div>
                {isAdmin ? (
                  <button
                    className="btn small ghost danger"
                    aria-label="Unlink group"
                    onClick={(e) => {
                      e.stopPropagation();
                      guard(() => services.removeCommunityGroup(communityId, gid))();
                    }}
                  >
                    Remove
                  </button>
                ) : null}
              </li>
            ))}
          </ul>
        ) : null}
        {isAdmin && addGroupOpen ? (
          <GroupLinkPicker
            excludeIds={groupIds}
            onPick={(gid) => {
              setAddGroupOpen(false);
              guard(() => services.addCommunityGroup(communityId, gid))();
            }}
          />
        ) : null}
      </Section>

      <Section title="Events">
        {events.length === 0 ? <p className="section-desc">No upcoming events.</p> : null}
        {events.length > 0 ? (
          <ul className="list">
            {events.map((e) => (
              <li key={e.id} className="row static">
                <span className="entity-glyph neutral" aria-hidden>
                  <Icon name="clock" size={18} />
                </span>
                <div className="row-main">
                  <div className="row-title">{e.title}</div>
                  <div className="row-sub">{new Date(e.startsAtMs).toLocaleString()}</div>
                </div>
                {isAdmin ? (
                  <button className="wa-icon" aria-label="Delete event" onClick={guard(() => services.deleteCommunityEvent(communityId, e.id))}>
                    <Icon name="trash" size={17} />
                  </button>
                ) : null}
              </li>
            ))}
          </ul>
        ) : null}
        {isAdmin ? (
          <div className="inline">
            <input className="input" placeholder="Event title" value={evTitle} style={{ flex: 1, minWidth: 160 }} onChange={(e) => setEvTitle(e.target.value)} />
            <input className="input" type="datetime-local" value={evWhen} style={{ width: "auto" }} onChange={(e) => setEvWhen(e.target.value)} />
            <button className="btn small" disabled={!evTitle.trim() || !evWhen} onClick={() => void createEvent()}>
              Add
            </button>
          </div>
        ) : null}
      </Section>

      {isMember ? (
        <Section title={`Members (${members.length})`}>
          <ul className="list">
            {members.map((m) => (
              <li key={m.userId} className="row static">
                <Avatar size={40} />
                <div className="row-main">
                  <div className="row-line1">
                    <span className="row-title">{services.nameForUser(m.userId)}</span>
                    {m.role !== "member" ? <span className="badge accent">{m.role}</span> : null}
                  </div>
                </div>
                <div className="row-right">
                  {isOwner && m.role !== "owner" ? (
                    <button
                      className="btn small ghost"
                      onClick={guard(() => services.setCommunityRole(communityId, m.userId, m.role === "admin" ? "member" : "admin"))}
                    >
                      {m.role === "admin" ? "Demote" : "Make admin"}
                    </button>
                  ) : null}
                  {isAdmin && m.role !== "owner" ? (
                    <button className="btn small ghost danger" aria-label="Remove member" onClick={guard(() => services.removeCommunityMember(communityId, m.userId))}>
                      Remove
                    </button>
                  ) : null}
                </div>
              </li>
            ))}
          </ul>
        </Section>
      ) : null}

      {isOwner ? (
        <Section title="Danger zone" desc="Deleting removes the community and unlinks its groups for everyone.">
          <div className="actions">
            <button
              className="btn danger"
              onClick={() => {
                if (window.confirm("Delete this community? This can't be undone.")) guard(() => services.deleteCommunity(communityId).then(onBack))();
              }}
            >
              Delete community
            </button>
          </div>
        </Section>
      ) : null}
    </Screen>
  );
}

/** GroupLinkPicker lists the user's group conversations to link into a community. */
function GroupLinkPicker({ excludeIds, onPick }: { excludeIds: string[]; onPick: (groupId: string) => void }) {
  const { services } = useServices();
  const [items, setItems] = useState<ChatSummary[]>([]);
  useEffect(() => {
    void services.conversations().then((cs) => {
      setItems(cs);
      for (const c of cs) services.ensureConversationKind(c.conversationId);
    }).catch(() => {});
  }, [services]);
  const groups = items.filter((c) => services.groupNameOf(c.conversationId) && !excludeIds.includes(c.conversationId));
  if (groups.length === 0) return <p className="muted" style={{ fontSize: "0.85rem" }}>No groups of yours to add.</p>;
  return (
    <ul className="list" style={{ maxHeight: "30vh" }}>
      {groups.map((c) => (
        <li key={c.conversationId} className="row" role="button" tabIndex={0} onClick={() => onPick(c.conversationId)} onKeyDown={onActivate(() => onPick(c.conversationId))}>
          <div className="row-title">👥 {services.groupNameOf(c.conversationId)}</div>
        </li>
      ))}
    </ul>
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

/** CollabScreen (T12.01): a conversation's shared Notes & Tasks workspace, with
 *  an activity timeline. Notes edit under optimistic version concurrency. */
export function CollabScreen({ conversationId, onBack }: { conversationId: string; onBack: () => void }) {
  const { services } = useServices();
  const [tab, setTab] = useState<"tasks" | "notes" | "activity">("tasks");
  const [tasks, setTasks] = useState<CollabTask[]>([]);
  const [notes, setNotes] = useState<CollabNote[]>([]);
  const [activity, setActivity] = useState<CollabActivity[]>([]);
  const [newTask, setNewTask] = useState("");
  const [editing, setEditing] = useState<CollabNote | "new" | null>(null);
  const [err, setErr] = useState<string | null>(null);

  const reload = useCallback(() => {
    void services.collabTasks(conversationId).then(setTasks).catch(() => {});
    void services.collabNotes(conversationId).then(setNotes).catch(() => {});
    void services.collabActivity(conversationId).then(setActivity).catch(() => {});
  }, [services, conversationId]);
  useEffect(() => {
    reload();
  }, [reload]);

  const name = (id: string): string => services.nameForUser(id);
  async function addTask(): Promise<void> {
    const t = newTask.trim();
    if (!t) return;
    try {
      await services.createTask(conversationId, t);
      setNewTask("");
      reload();
    } catch (e) {
      setErr(messageOf(e));
    }
  }

  return (
    <div className="pane">
      <div className="pane-head">
        <button className="wa-icon wa-back" onClick={onBack} aria-label="Back" title="Back">
          <Icon name="back" size={24} />
        </button>
        <span className="pane-head-title">Notes &amp; Tasks</span>
      </div>
      <div className="collab-tabs">
        {(["tasks", "notes", "activity"] as const).map((t) => (
          <button key={t} className={`collab-tab${tab === t ? " on" : ""}`} onClick={() => setTab(t)}>
            {t[0]!.toUpperCase() + t.slice(1)}
          </button>
        ))}
      </div>
      {err ? <p className="error" style={{ padding: "0 16px" }}>{err}</p> : null}
      {tab === "tasks" ? (
        <>
          <div style={{ display: "flex", gap: 8, padding: "10px 14px" }}>
            <input className="input" value={newTask} placeholder="Add a task…" onChange={(e) => setNewTask(e.target.value)} onKeyDown={(e) => e.key === "Enter" && void addTask()} />
            <button className="btn" onClick={() => void addTask()}>Add</button>
          </div>
          <ul className="list">
            {tasks.map((t) => (
              <li key={t.id} className="row" style={{ cursor: "default" }}>
                <input type="checkbox" checked={t.done} onChange={() => void services.toggleTask(conversationId, t.id, !t.done).then(reload)} style={{ width: 20, height: 20 }} />
                <div className="row-main">
                  <div className="row-line1">
                    <span className="row-title" style={{ textDecoration: t.done ? "line-through" : "none", opacity: t.done ? 0.6 : 1 }}>{t.title}</span>
                  </div>
                </div>
                <button className="wa-icon" title="Delete task" aria-label="Delete task" onClick={() => void services.deleteCollabTask(conversationId, t.id).then(reload)}>
                  <Icon name="trash" size={18} />
                </button>
              </li>
            ))}
            {tasks.length === 0 ? <li className="status-empty">No tasks yet.</li> : null}
          </ul>
        </>
      ) : tab === "notes" ? (
        <>
          <div style={{ padding: "10px 14px" }}>
            <button className="btn small" onClick={() => setEditing("new")}>＋ New note</button>
          </div>
          <ul className="list">
            {notes.map((n) => (
              <li key={n.id} className="row" role="button" tabIndex={0} onClick={() => setEditing(n)} onKeyDown={onActivate(() => setEditing(n))}>
                <div className="row-main">
                  <div className="row-line1">
                    <span className="row-title">{n.title}</span>
                    {n.approval !== "none" ? <span className={`collab-badge collab-${n.approval}`}>{n.approval}</span> : null}
                  </div>
                  <div className="row-line2">
                    <span className="row-sub">v{n.version} · {name(n.updated_by)}</span>
                  </div>
                </div>
              </li>
            ))}
            {notes.length === 0 ? <li className="status-empty">No notes yet.</li> : null}
          </ul>
        </>
      ) : (
        <ul className="list">
          {activity.map((a, i) => (
            <li key={i} className="row" style={{ cursor: "default" }}>
              <div className="row-main">
                <div className="row-line1">
                  <span className="row-title" style={{ fontWeight: 400, fontSize: 14 }}>{name(a.actor)} {a.summary}</span>
                </div>
                <div className="row-line2">
                  <span className="row-sub">{formatLastSeen(a.at_ms)}</span>
                </div>
              </div>
            </li>
          ))}
          {activity.length === 0 ? <li className="status-empty">No activity yet.</li> : null}
        </ul>
      )}
      {editing ? <NoteEditor conversationId={conversationId} note={editing === "new" ? null : editing} onClose={() => { setEditing(null); reload(); }} /> : null}
    </div>
  );
}

/** DiscoverScreen (T13.01): public metadata search across channels, public
 *  communities, and usernames. Results open the entity; nothing E2EE is shown. */
export function DiscoverScreen({ onBack, onOpenChannel, onOpenCommunity }: { onBack: () => void; onOpenChannel: (id: string) => void; onOpenCommunity: (id: string) => void }) {
  const { services } = useServices();
  const [query, setQuery] = useState("");
  const [filter, setFilter] = useState<"" | "channel" | "community" | "user">("");
  const [results, setResults] = useState<DiscoverResult[]>([]);
  const [searching, setSearching] = useState(false);

  useEffect(() => {
    const q = query.trim();
    if (q.length < 2) {
      setResults([]);
      return;
    }
    setSearching(true);
    const h = window.setTimeout(() => {
      void services
        .discover(q, filter ? [filter] : [])
        .then(setResults)
        .catch(() => setResults([]))
        .finally(() => setSearching(false));
    }, 250);
    return () => window.clearTimeout(h);
  }, [services, query, filter]);

  const tabs: { key: typeof filter; label: string }[] = [
    { key: "", label: "All" },
    { key: "channel", label: "Channels" },
    { key: "community", label: "Communities" },
    { key: "user", label: "People" },
  ];
  const icon = (k: DiscoverResult["kind"]): ReactNode =>
    k === "channel" ? <Icon name="channel" size={22} /> : k === "community" ? <Icon name="community" size={22} /> : <Icon name="contacts" size={22} />;
  const open = (r: DiscoverResult): void => {
    if (r.kind === "channel") onOpenChannel(r.id);
    else if (r.kind === "community") onOpenCommunity(r.id);
  };

  return (
    <div className="pane">
      <div className="pane-head">
        <button className="wa-icon wa-back" onClick={onBack} aria-label="Back" title="Back">
          <Icon name="back" size={24} />
        </button>
        <span className="pane-head-title">Discover</span>
      </div>
      <div className="wa-search">
        <input value={query} onChange={(e) => setQuery(e.target.value)} placeholder="Search channels, communities, people" aria-label="Discover search" autoFocus />
      </div>
      <div className="collab-tabs">
        {tabs.map((t) => (
          <button key={t.label} className={`collab-tab${filter === t.key ? " on" : ""}`} onClick={() => setFilter(t.key)}>{t.label}</button>
        ))}
      </div>
      {query.trim().length < 2 ? (
        <p className="muted center">Search public channels, communities, and usernames.</p>
      ) : searching && results.length === 0 ? (
        <p className="muted center">Searching…</p>
      ) : results.length === 0 ? (
        <p className="muted center">No results for “{query.trim()}”.</p>
      ) : (
        <ul className="list">
          {results.map((r) => (
            <li key={`${r.kind}:${r.id}`} className="row" role={r.kind === "user" ? undefined : "button"} tabIndex={r.kind === "user" ? undefined : 0} onClick={() => open(r)} onKeyDown={r.kind === "user" ? undefined : onActivate(() => open(r))} style={r.kind === "user" ? { cursor: "default" } : undefined}>
              <span className="avatar avatar-default" style={{ width: 44, height: 44 }}>{icon(r.kind)}</span>
              <div className="row-main">
                <div className="row-line1">
                  <span className="row-title">
                    {r.title}
                    {r.verified ? <span className="collab-badge collab-approved" style={{ textTransform: "none" }}>✓ Verified</span> : null}
                  </span>
                </div>
                <div className="row-line2">
                  <span className="row-sub">{r.handle ? r.handle + (r.subtitle ? " · " + r.subtitle : "") : r.subtitle || r.kind}</span>
                </div>
              </div>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}

/** WhiteboardScreen (T12.02): a real-time collaborative canvas over the CRDT
 *  op-log. Strokes append locally + POST to the server; a 1.2s poll merges peers'
 *  ops (grow-only set → convergent). */
export function WhiteboardScreen({ conversationId, onBack }: { conversationId: string; onBack: () => void }) {
  const { services } = useServices();
  const canvasRef = useRef<HTMLCanvasElement>(null);
  const opsRef = useRef<BoardOp[]>([]);
  const cursorRef = useRef(0);
  const drawingRef = useRef<{ points: number[] } | null>(null);
  const [color, setColor] = useState("#e5484d");
  const [width, setWidth] = useState(3);
  const me = services.myUserId();
  const colors = ["#111b21", "#e5484d", "#1fa855", "#0a7cff", "#f2994a", "#9b6dd6"];

  const redraw = useCallback(() => {
    const c = canvasRef.current;
    if (!c) return;
    const ctx = c.getContext("2d");
    if (!ctx) return;
    const w = c.width;
    const h = c.height;
    ctx.clearRect(0, 0, w, h);
    const drawPath = (pts: number[], col: string, wid: number): void => {
      ctx.strokeStyle = col;
      ctx.lineWidth = wid;
      ctx.lineJoin = "round";
      ctx.lineCap = "round";
      ctx.beginPath();
      for (let i = 0; i < pts.length; i += 2) {
        const x = pts[i]! * w;
        const y = pts[i + 1]! * h;
        if (i === 0) ctx.moveTo(x, y);
        else ctx.lineTo(x, y);
      }
      ctx.stroke();
    };
    for (const s of renderStrokes(opsRef.current)) drawPath(s.points, s.color, s.width);
    const d = drawingRef.current;
    if (d && d.points.length >= 2) drawPath(d.points, color, width);
  }, [color, width]);

  useEffect(() => {
    const c = canvasRef.current;
    if (!c) return;
    const resize = (): void => {
      const r = c.getBoundingClientRect();
      c.width = r.width;
      c.height = r.height;
      redraw();
    };
    resize();
    window.addEventListener("resize", resize);
    return () => window.removeEventListener("resize", resize);
  }, [redraw]);

  useEffect(() => {
    let alive = true;
    const poll = async (): Promise<void> => {
      const { ops, cursor } = await services.boardSync(conversationId, cursorRef.current);
      if (!alive || ops.length === 0) return;
      opsRef.current = mergeOps(opsRef.current, ops as BoardOp[]);
      cursorRef.current = Math.max(cursorRef.current, cursor);
      redraw();
    };
    void poll();
    const h = window.setInterval(() => void poll(), 1200);
    return () => {
      alive = false;
      window.clearInterval(h);
    };
  }, [services, conversationId, redraw]);

  const posOf = (e: ReactPointerEvent<HTMLCanvasElement>): [number, number] => {
    const r = e.currentTarget.getBoundingClientRect();
    return [(e.clientX - r.left) / r.width, (e.clientY - r.top) / r.height];
  };
  const down = (e: ReactPointerEvent<HTMLCanvasElement>): void => {
    e.currentTarget.setPointerCapture(e.pointerId);
    const [x, y] = posOf(e);
    drawingRef.current = { points: [x, y] };
  };
  const move = (e: ReactPointerEvent<HTMLCanvasElement>): void => {
    if (!drawingRef.current) return;
    const [x, y] = posOf(e);
    drawingRef.current.points.push(x, y);
    redraw();
  };
  const commit = (op: BoardOp): void => {
    opsRef.current = mergeOps(opsRef.current, [op]);
    cursorRef.current = Math.max(cursorRef.current, op.seq);
    redraw();
    void services.boardAppend(conversationId, [op]).catch(() => {});
  };
  const up = (): void => {
    const d = drawingRef.current;
    drawingRef.current = null;
    if (!d || d.points.length < 2) {
      redraw();
      return;
    }
    commit(makeStroke(opsRef.current, me, crypto.randomUUID(), color, width, d.points));
  };

  return (
    <div className="pane">
      <div className="pane-head">
        <button className="wa-icon wa-back" onClick={onBack} aria-label="Back" title="Back">
          <Icon name="back" size={24} />
        </button>
        <span className="pane-head-title">Whiteboard</span>
      </div>
      <div className="wb-toolbar">
        {colors.map((c) => (
          <button key={c} className={`wb-swatch${color === c ? " on" : ""}`} style={{ background: c }} onClick={() => setColor(c)} aria-label={`Pen colour ${c}`} />
        ))}
        <input type="range" min={1} max={12} value={width} onChange={(e) => setWidth(Number(e.target.value))} aria-label="Pen width" />
        <button className="btn small ghost" onClick={() => commit(makeClear(opsRef.current, me, crypto.randomUUID()))}>Clear</button>
      </div>
      <div className="wb-stage">
        <canvas ref={canvasRef} className="wb-canvas" onPointerDown={down} onPointerMove={move} onPointerUp={up} onPointerLeave={up} />
      </div>
    </div>
  );
}

/** NoteEditor edits a shared note under optimistic version concurrency, with an
 *  approval workflow, comments, and revision history. */
function NoteEditor({ conversationId, note, onClose }: { conversationId: string; note: CollabNote | null; onClose: () => void }) {
  const { services } = useServices();
  const [title, setTitle] = useState(note?.title ?? "");
  const [body, setBody] = useState(note?.body ?? "");
  const [cur, setCur] = useState<CollabNote | null>(note);
  const [comments, setComments] = useState<CollabComment[]>([]);
  const [comment, setComment] = useState("");
  const [revs, setRevs] = useState<CollabRevision[] | null>(null);
  const [err, setErr] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const name = (id: string): string => services.nameForUser(id);

  useEffect(() => {
    if (cur) void services.noteComments(cur.id).then(setComments).catch(() => {});
  }, [services, cur]);

  async function save(): Promise<void> {
    setErr(null);
    setBusy(true);
    try {
      setCur(cur ? await services.updateNote(cur.id, title, body, cur.version) : await services.createNote(conversationId, title, body));
    } catch (e) {
      setErr(messageOf(e));
    } finally {
      setBusy(false);
    }
  }
  async function addComment(): Promise<void> {
    if (!cur || comment.trim() === "") return;
    try {
      await services.addNoteComment(cur.id, comment.trim());
      setComment("");
      setComments(await services.noteComments(cur.id));
    } catch (e) {
      setErr(messageOf(e));
    }
  }
  async function approval(action: "request" | "approve" | "reject"): Promise<void> {
    if (!cur) return;
    try {
      if (action === "request") await services.requestApproval(cur.id);
      else await services.decideApproval(cur.id, action === "approve");
      const all = await services.collabNotes(conversationId);
      setCur(all.find((x) => x.id === cur.id) ?? cur);
    } catch (e) {
      setErr(messageOf(e));
    }
  }

  return (
    <div className="sheet-backdrop" role="dialog" aria-modal="true" onClick={onClose}>
      <div className="sheet collab-editor" onClick={(e) => e.stopPropagation()}>
        <input className="input" value={title} placeholder="Note title" onChange={(e) => setTitle(e.target.value)} maxLength={200} />
        <textarea className="input" rows={6} value={body} placeholder="Write…" onChange={(e) => setBody(e.target.value)} />
        {err ? <p className="error">{err}</p> : null}
        <div style={{ display: "flex", gap: 8, alignItems: "center", flexWrap: "wrap" }}>
          <button className="btn" onClick={() => void save()} disabled={busy}>{busy ? "Saving…" : cur ? `Save (v${cur.version})` : "Create"}</button>
          {cur ? <span className="row-sub">v{cur.version} · {cur.approval}</span> : null}
        </div>
        {cur ? (
          <div style={{ display: "flex", gap: 8, flexWrap: "wrap" }}>
            {cur.approval === "none" || cur.approval === "rejected" ? <button className="btn small ghost" onClick={() => void approval("request")}>Request approval</button> : null}
            {cur.approval === "pending" ? (
              <>
                <button className="btn small" onClick={() => void approval("approve")}>Approve</button>
                <button className="btn small ghost" onClick={() => void approval("reject")}>Reject</button>
              </>
            ) : null}
            <button className="btn small ghost" onClick={() => void services.noteRevisions(cur.id).then(setRevs)}>History</button>
          </div>
        ) : null}
        {revs ? (
          <div className="collab-revs">
            <strong style={{ fontSize: "0.8rem" }}>Version history</strong>
            {revs.map((r) => (
              <div key={r.version} className="row-sub">v{r.version} · {name(r.author)} · {formatLastSeen(r.created_at_ms)}</div>
            ))}
          </div>
        ) : null}
        {cur ? (
          <div className="collab-comments">
            <strong style={{ fontSize: "0.8rem" }}>Comments</strong>
            {comments.map((c) => (
              <div key={c.id} className="row-sub"><b>{name(c.author)}:</b> {c.body}</div>
            ))}
            <div style={{ display: "flex", gap: 8 }}>
              <input className="input" value={comment} placeholder="Add a comment…" onChange={(e) => setComment(e.target.value)} onKeyDown={(e) => e.key === "Enter" && void addComment()} />
              <button className="btn small" onClick={() => void addComment()}>Send</button>
            </div>
          </div>
        ) : null}
        <button className="btn ghost" onClick={onClose}>Done</button>
      </div>
    </div>
  );
}

export function Thread({
  conversationId,
  onBack,
  onGroupInfo,
  onSearchInChat,
  onCollab,
  onBoard,
  focusMsgUuid,
}: {
  conversationId: string;
  onBack: () => void;
  onGroupInfo: (id: string) => void;
  onSearchInChat?: (id: string) => void;
  onCollab?: (id: string) => void;
  onBoard?: (id: string) => void;
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
  const [showSecret, setShowSecret] = useState(false); // disappearing/secret-chat sheet (T10.01)
  const [unlocked, setUnlocked] = useState(false); // locked-chat gate (per open)
  const [flashId, setFlashId] = useState<string | null>(null); // jump-to-original highlight
  const focusedRef = useRef<string | null>(null); // search jump-to-result (once per target)
  const [forwardMsg, setForwardMsg] = useState<ThreadMessage | null>(null); // message being forwarded
  const [reportMsg, setReportMsg] = useState<ThreadMessage | null>(null); // message being reported (T10.03)
  const [summary, setSummary] = useState<MeetingSummary | null>(null); // AI conversation summary (T11.02/T11.03)
  const [picker, setPicker] = useState<"emoji" | "gif" | "sticker" | "tools" | null>(null); // composer picker
  const [showPoll, setShowPoll] = useState(false); // poll-creation modal
  const [showLocation, setShowLocation] = useState(false); // location-share sheet
  const [showContact, setShowContact] = useState(false); // contact-share picker
  const [showSchedule, setShowSchedule] = useState(false); // schedule-message sheet
  const [showTemplates, setShowTemplates] = useState(false); // saved-reply picker
  const [showInteractive, setShowInteractive] = useState(false); // interactive-message composer
  const [headMenu, setHeadMenu] = useState(false); // thread-header overflow menu
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
    setUnlocked(false); // re-lock a locked chat when switching to it (T10.01)
    void services.loadDisappearing(conversationId); // pull the shared disappearing timer
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
    // On-device toxicity nudge (T11.03): warn before sending hostile wording.
    if (services.aiFeaturesOn() && detectToxicity(text).level === "high") {
      if (!window.confirm("This message may come across as offensive. Send anyway?")) return;
    }
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

  // Disappearing-messages sweep (T10.01): once the chat timer elapses for a
  // message (counted from sent), hide it locally. Runs on load + every 5s. The
  // server purges its relay copy independently as a backstop.
  useEffect(() => {
    const ttl = services.disappearingSeconds(conversationId);
    if (ttl <= 0) return;
    const run = (): void => {
      const items: EphemeralMessage[] = messages.filter((m) => !m.deleted).map((m) => ({ id: m.msgUuid, sentMs: m.createdAt }));
      for (const id of sweepExpired(items, ttl, Date.now())) void services.deleteForMe(id);
    };
    run();
    const h = window.setInterval(run, 5000);
    return () => window.clearInterval(h);
  }, [services, conversationId, messages]);

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

  // On-device messaging AI (T11.02) — gated by the AI settings + kill-switch.
  const aiOn = services.aiFeaturesOn();
  const lastReceived =
    aiOn && draft.trim() === "" && !editing
      ? [...messages].reverse().find((m) => !m.mine && !m.deleted && parseTextMessage(m.body).text.trim() !== "")
      : undefined;
  const replyChips = lastReceived ? smartReplies(parseTextMessage(lastReceived.body).text) : [];

  function sendQuick(text: string): void {
    services.sendTextWithUndo(conversationId, text); // send a smart-reply directly
  }
  function runSummary(): void {
    const lines = messages
      .filter((m) => !m.deleted)
      .map((m) => {
        const t = parseTextMessage(m.body).text;
        if (!t) return "";
        const speaker = m.mine ? "You" : services.peerNameOf(conversationId) || "Them";
        return `${speaker}: ${t}`;
      })
      .filter(Boolean);
    setSummary(lines.length >= 2 ? meetingSummary(lines) : { summary: "Not enough messages to summarize yet.", actionItems: [], topics: [] });
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
    report: (m) => setReportMsg(m),
    readAloud: aiOn
      ? (m) => {
          const t = parseTextMessage(m.body).text;
          if (t && "speechSynthesis" in window) window.speechSynthesis.speak(new SpeechSynthesisUtterance(t));
        }
      : undefined,
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
        <button className="wa-icon wa-back" onClick={onBack} aria-label="Back" title="Back">
          <Icon name="back" size={24} />
        </button>
        <Avatar
          name={group ? group.name : services.peerNameOf(conversationId) || conversationId}
          id={conversationId}
          emoji={group ? "👥" : undefined}
          size={40}
        />
        <div
          className="thread-title"
          role={group ? "button" : undefined}
          tabIndex={group ? 0 : undefined}
          style={group ? { cursor: "pointer" } : undefined}
          onClick={group ? () => onGroupInfo(conversationId) : undefined}
          onKeyDown={group ? onActivate(() => onGroupInfo(conversationId)) : undefined}
          title={group ? "Group info" : undefined}
        >
          <span className={group || services.peerNameOf(conversationId) ? "thread-name" : "thread-name mono"}>
            {group ? group.name : services.peerNameOf(conversationId) || conversationId.slice(0, 12)}
          </span>
          {group ? (
            <span className="thread-status">
              {group.settings.announcements ? "📢 Announcements" : "Group"} · tap for info
            </span>
          ) : statusLine ? (
            <span className="thread-status">{statusLine}</span>
          ) : null}
        </div>
        {group ? (
          <button className="wa-icon" title="Group info" aria-label="Group info" onClick={() => onGroupInfo(conversationId)}>
            <Icon name="info" size={22} />
          </button>
        ) : (
          <>
            <button className="wa-icon" title="Video call" aria-label="Start video call" disabled={!peerId} onClick={() => peerId && void call.startCall(peerId, "video")}>
              <Icon name="video" size={22} />
            </button>
            <button className="wa-icon" title="Voice call" aria-label="Start voice call" disabled={!peerId} onClick={() => peerId && void call.startCall(peerId, "voice")}>
              <Icon name="phone" size={21} />
            </button>
          </>
        )}
        {onSearchInChat && (
          <button className="wa-icon" title="Search in this chat" aria-label="Search in this chat" onClick={() => onSearchInChat(conversationId)}>
            <Icon name="search" size={21} />
          </button>
        )}
        {/* Secondary chat actions live behind one overflow menu so the header
            stays readable instead of showing eleven icons at once. */}
        <div className="wa-menu-wrap">
          <button className="wa-icon" title="More" aria-label="More chat actions" aria-haspopup="menu" aria-expanded={headMenu} onClick={() => setHeadMenu((v) => !v)}>
            <Icon name="menu" size={21} />
          </button>
          {headMenu ? (
            <>
              <div className="wa-menu-backdrop" onClick={() => setHeadMenu(false)} />
              <div className="wa-menu" role="menu">
                <button
                  className="menu-item"
                  onClick={() => {
                    setHeadMenu(false);
                    services.toggleMute(conversationId);
                    setMuted(services.isMuted(conversationId));
                  }}
                >
                  <Icon name={muted ? "mute" : "bell"} size={18} /> {muted ? "Unmute notifications" : "Mute notifications"}
                </button>
                <button className="menu-item" onClick={() => { setHeadMenu(false); setShowSecret(true); }}>
                  <Icon name="clock" size={18} /> Disappearing &amp; privacy
                  {services.disappearingSeconds(conversationId) > 0 ? <span className="badge accent">on</span> : null}
                </button>
                <button className="menu-item" onClick={() => { setHeadMenu(false); setShowWallpaper(true); }}>
                  <Icon name="wallpaper" size={18} /> Chat wallpaper
                </button>
                <button className="menu-item" onClick={() => { setHeadMenu(false); void exportChat(); }}>
                  <Icon name="download" size={18} /> Export chat
                </button>
                {onCollab ? (
                  <button className="menu-item" onClick={() => { setHeadMenu(false); onCollab(conversationId); }}>
                    <Icon name="copy" size={18} /> Notes &amp; tasks
                  </button>
                ) : null}
                {onBoard ? (
                  <button className="menu-item" onClick={() => { setHeadMenu(false); onBoard(conversationId); }}>
                    <Icon name="wallpaper" size={18} /> Whiteboard
                  </button>
                ) : null}
                {aiOn ? (
                  <button className="menu-item" onClick={() => { setHeadMenu(false); runSummary(); }}>
                    <span aria-hidden>✨</span> Summarize chat
                  </button>
                ) : null}
              </div>
            </>
          ) : null}
        </div>
      </div>
      {services.isLocked(conversationId) && !unlocked ? <LockGate onUnlock={() => setUnlocked(true)} /> : null}
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
              onQuickReply={sendQuick}
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
      {showSecret ? <SecretChatSheet conversationId={conversationId} onClose={() => setShowSecret(false)} /> : null}
      {reportMsg ? <ReportSheet conversationId={conversationId} onClose={() => setReportMsg(null)} /> : null}
      {summary !== null ? (
        <div className="sheet-backdrop" role="dialog" aria-modal="true" onClick={() => setSummary(null)}>
          <div className="sheet" onClick={(e) => e.stopPropagation()}>
            <strong>✨ Summary</strong>
            <p style={{ fontSize: "0.9rem", lineHeight: 1.5 }}>{summary.summary}</p>
            {summary.actionItems.length > 0 ? (
              <>
                <strong style={{ fontSize: "0.85rem" }}>Action items</strong>
                <ul style={{ margin: "2px 0", paddingLeft: 18, fontSize: "0.82rem", lineHeight: 1.5 }}>
                  {summary.actionItems.map((a, i) => (
                    <li key={i}>{a}</li>
                  ))}
                </ul>
              </>
            ) : null}
            {summary.topics.length > 0 ? (
              <div style={{ display: "flex", flexWrap: "wrap", gap: 6 }}>
                {summary.topics.map((t) => (
                  <span key={t} className="topic-tag">#{t}</span>
                ))}
              </div>
            ) : null}
            <p className="muted" style={{ fontSize: "0.72rem" }}>Generated on your device — nothing was sent anywhere.</p>
            <button className="btn" onClick={() => setSummary(null)}>Done</button>
          </div>
        </div>
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
      {showInteractive ? (
        <InteractiveComposer
          onClose={() => setShowInteractive(false)}
          onSend={(text, buttons) => {
            setShowInteractive(false);
            void services.sendInteractive(conversationId, text, buttons).catch((e) => window.alert(messageOf(e)));
          }}
        />
      ) : null}
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
          {picker === "tools" ? (
            <>
              <div className="wa-menu-backdrop" onClick={() => setPicker(null)} />
              <div className="tools-pop" role="menu">
                <button className="tool-item" onClick={() => { setPicker(null); fileRef.current?.click(); }} disabled={sendingMedia || !!editing}>
                  <span className="tool-ic tool-photo"><Icon name="camera" size={20} /></span>Photo &amp; video
                </button>
                <button className="tool-item" onClick={() => { setPicker(null); fileRef.current?.click(); }} disabled={sendingMedia || !!editing}>
                  <span className="tool-ic tool-doc"><Icon name="download" size={20} /></span>Document
                </button>
                <button className="tool-item" onClick={() => setPicker("gif")} disabled={!!editing}>
                  <span className="tool-ic tool-gif">GIF</span>GIF
                </button>
                <button className="tool-item" onClick={() => setPicker("sticker")} disabled={!!editing}>
                  <span className="tool-ic tool-sticker"><Icon name="star" size={18} /></span>Sticker
                </button>
                <button className="tool-item" onClick={() => { setPicker(null); setShowPoll(true); }} disabled={!!editing}>
                  <span className="tool-ic tool-poll"><Icon name="updates" size={18} /></span>Poll
                </button>
                <button className="tool-item" onClick={() => { setPicker(null); setShowLocation(true); }} disabled={!!editing}>
                  <span className="tool-ic tool-loc"><Icon name="info" size={18} /></span>Location
                </button>
                <button className="tool-item" onClick={() => { setPicker(null); setShowContact(true); }} disabled={!!editing}>
                  <span className="tool-ic tool-contact"><Icon name="contacts" size={18} /></span>Contact
                </button>
                <button className="tool-item" onClick={() => { setPicker(null); setShowInteractive(true); }} disabled={!!editing}>
                  <span className="tool-ic tool-poll">🔘</span>Buttons
                </button>
                <button className="tool-item" onClick={() => { setPicker(null); setShowTemplates(true); }} disabled={!!editing}>
                  <span className="tool-ic tool-tpl"><Icon name="copy" size={18} /></span>Saved replies
                </button>
                <button className="tool-item" onClick={() => { setPicker(null); setShowSchedule(true); }} disabled={!!editing || draft.trim() === ""}>
                  <span className="tool-ic tool-sch"><Icon name="clock" size={18} /></span>Schedule
                </button>
                <button className="tool-item" onClick={() => { setPicker(null); wrapSelection("*"); }} disabled={!!editing}>
                  <span className="tool-ic tool-fmt"><b>B</b></span>Bold
                </button>
                <button className="tool-item" onClick={() => { setPicker(null); wrapSelection("_"); }} disabled={!!editing}>
                  <span className="tool-ic tool-fmt"><i>I</i></span>Italic
                </button>
              </div>
            </>
          ) : null}
          {replyChips.length > 0 ? (
            <div className="smart-replies" role="group" aria-label="Suggested replies">
              {replyChips.map((r) => (
                <button key={r} type="button" className="smart-reply" onClick={() => sendQuick(r)}>
                  {r}
                </button>
              ))}
            </div>
          ) : null}
          <form className="composer" onSubmit={send}>
            <input ref={fileRef} type="file" hidden onChange={onPickFile} aria-hidden />
            <button className="wa-icon" type="button" aria-label="Emoji" title="Emoji" onClick={() => setPicker((p) => (p === "emoji" ? null : "emoji"))}>
              <Icon name="emoji" size={24} />
            </button>
            <button className="wa-icon" type="button" aria-label="Attach" title="Attach" disabled={sendingMedia} onClick={() => setPicker((p) => (p === "tools" ? null : "tools"))}>
              {sendingMedia ? <span className="spinner tiny" /> : <Icon name="attach" size={23} />}
            </button>
            {aiOn && draft.trim() !== "" && !editing ? (
              <button
                className="wa-icon"
                type="button"
                aria-label="Fix grammar"
                title="Fix grammar (on-device AI)"
                onClick={() => onDraftChange(correctGrammar(draft).text)}
              >
                ✨
              </button>
            ) : null}
            <input
              ref={composerRef}
              className="input"
              value={draft}
              onChange={(e) => onDraftChange(e.target.value)}
              placeholder={editing ? "Edit message" : sendingMedia ? "Uploading…" : "Type a message"}
              aria-label="Type a message"
            />
            <button className="wa-send" type="submit" aria-label={draft.trim() !== "" || editing ? "Send" : "Voice message"} title={draft.trim() !== "" || editing ? "Send" : "Voice message"}>
              <Icon name={draft.trim() !== "" || editing ? "send" : "mic"} size={23} />
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
  report?(m: ThreadMessage): void;
  readAloud?(m: ThreadMessage): void;
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
  const interactive = parseInteractive(m.body);
  if (interactive) return `🔘 ${interactive.text.slice(0, 60)}`;
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
  onQuickReply,
}: {
  message: ThreadMessage;
  actions: MessageActions;
  onOpen: (env: MediaEnvelope) => void;
  bubbleRef?: (el: HTMLDivElement | null) => void;
  flash?: boolean;
  onJump?: (msgUuid: string) => void;
  onQuickReply?: (text: string) => void;
}) {
  const [menu, setMenu] = useState(false);
  const media = message.deleted ? null : parseMediaMessage(message.body);
  const sticker = media || message.deleted ? null : parseSticker(message.body);
  const poll = media || sticker || message.deleted ? null : parsePoll(message.body);
  const location = media || sticker || poll || message.deleted ? null : parseLocation(message.body);
  const live = media || sticker || poll || location || message.deleted ? null : parseLiveLocation(message.body);
  const contact = media || sticker || poll || location || live || message.deleted ? null : parseContactCard(message.body);
  const interactive = media || sticker || poll || location || live || contact || message.deleted ? null : parseInteractive(message.body);
  const special = !!(media || sticker || poll || location || live || contact || interactive);
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
          {text && actions.readAloud ? <button className="menu-item" onClick={run(actions.readAloud)}>🔊 Read aloud</button> : null}
          <button className="menu-item" onClick={run(actions.toggleStar)}>{message.starred ? "☆ Unstar" : "⭐ Star"}</button>
          <button className="menu-item" onClick={run(actions.togglePin)}>{message.pinned ? "📌 Unpin" : "📌 Pin"}</button>
          {canEdit ? <button className="menu-item" onClick={run(actions.edit)}>✎ Edit</button> : null}
          {canDeleteAll ? <button className="menu-item danger" onClick={run(actions.deleteForEveryone)}>🗑 Delete for everyone</button> : null}
          <button className="menu-item danger" onClick={run(actions.deleteForMe)}>🗑 Delete for me</button>
          {!message.mine && actions.report ? <button className="menu-item danger" onClick={run(actions.report)}>⚠ Report</button> : null}
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
      ) : interactive ? (
        <InteractiveBubble msg={interactive} onQuickReply={onQuickReply} />
      ) : (
        <>
          <span>{message.deleted ? <em style={{ opacity: 0.7 }}>This message was deleted</em> : text ? <RichText text={text.text} /> : null}</span>
          {message.edited && !message.deleted ? <span style={{ fontSize: "0.68rem", opacity: 0.6, marginLeft: 4 }}>(edited)</span> : null}
          {text?.linkPreview ? <LinkPreviewCard preview={text.linkPreview} /> : null}
        </>
      )}
      {!message.deleted || message.mine ? (
        <span className="bubble-meta">
          {fmtClock(message.createdAt)}
          {message.mine && !message.deleted ? <StatusTicks state={message.state} /> : null}
        </span>
      ) : null}
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
    </div>
  );
}

/** StatusTicks renders WhatsApp-style delivery state on my own bubbles:
 *  clock sending · ✓ sent · ✓✓ delivered · ✓✓ (blue) read. */
function StatusTicks({ state }: { state: string }) {
  if (state === "sending") return <span className="state" title="Sending" aria-label="Sending"><Icon name="clock" size={15} /></span>;
  if (state === "delivered") return <span className="state" title="Delivered" aria-label="Delivered"><Icon name="checkDouble" size={17} /></span>;
  if (state === "read")
    return (
      <span className="state state-read" title="Read" aria-label="Read">
        <Icon name="checkDouble" size={17} />
      </span>
    );
  return <span className="state" title="Sent" aria-label="Sent"><Icon name="check" size={16} /></span>; // sent (default)
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

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
} from "@wa/media-pipeline";
import { useEffect, useRef, useState, type FormEvent, type KeyboardEvent, type ReactNode } from "react";
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
}: {
  onOpen: (id: string) => void;
  onNew: () => void;
  onSearch: () => void;
}) {
  const { services } = useServices();
  const [items, setItems] = useState<ChatSummary[]>([]);

  useEffect(() => {
    let alive = true;
    const tick = (): void => {
      services
        .conversations()
        .then((c) => {
          if (alive) setItems(c);
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
              <div className="row-title">{c.title}</div>
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
    await services.sendText(conversationId, text);
    const next = await services.thread(conversationId);
    setMessages(next);
  }

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
          <span className="mono">{conversationId.slice(0, 12)}</span>
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
          <MessageBubble key={m.msgUuid} message={m} onOpen={(env) => setGallery({ items: visuals, startKey: env.objectKey })} />
        ))}
      </div>
      <DownloadsPanel />
      <form className="composer" onSubmit={send}>
        <input
          className="input"
          value={draft}
          onChange={(e) => onDraftChange(e.target.value)}
          placeholder="Message"
          aria-label="Type a message"
        />
        <button className="btn" type="submit">
          Send
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

/** MessageBubble renders a text message, or — when the decrypted body carries a
 *  media envelope — the attachment(s) plus any caption. */
function MessageBubble({ message, onOpen }: { message: ThreadMessage; onOpen: (env: MediaEnvelope) => void }) {
  const media = message.deleted ? null : parseMediaMessage(message.body);
  const text = media || message.deleted ? null : parseTextMessage(message.body);

  return (
    <div className={message.mine ? "bubble mine" : "bubble theirs"}>
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
          <span>{message.deleted ? "This message was deleted" : text?.text}</span>
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

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
import { useEffect, useState, type FormEvent, type ReactNode } from "react";
import { registerWebPush } from "../push";
import { messageOf } from "./errors";
import { useCall } from "../call/CallContext";
import { DownloadsPanel } from "./media/DownloadsPanel";
import { Gallery } from "./media/Gallery";
import { MediaMessage } from "./media/MediaMessage";
import { useServices } from "./ServicesContext";

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
        inputMode="tel"
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
          inputMode="numeric"
          type="password"
          autoFocus
          disabled={busy}
        />
      ) : (
        <input
          className="input code"
          value={code}
          onChange={(e) => setCode(e.target.value)}
          placeholder="123456"
          inputMode="numeric"
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
    const handle = setInterval(tick, 1500);
    return () => {
      alive = false;
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
            <li key={c.conversationId} className="row" onClick={() => onOpen(c.conversationId)}>
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
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="Search messages"
          autoFocus
        />
      </div>
      {query.trim() && hits.length === 0 ? <p className="muted center">No matches.</p> : null}
      <ul className="list">
        {hits.map((h) => (
          <li key={h.msgUuid} className="row" onClick={() => onOpen(h.conversationId)}>
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

  useEffect(() => {
    let alive = true;
    const tick = (): void => {
      services
        .thread(conversationId)
        .then((m) => {
          if (alive) setMessages(m);
        })
        .catch(() => {});
    };
    tick();
    const handle = setInterval(tick, 1000);
    return () => {
      alive = false;
      clearInterval(handle);
    };
  }, [services, conversationId]);

  async function send(e: FormEvent): Promise<void> {
    e.preventDefault();
    const text = draft.trim();
    if (!text) return;
    setDraft("");
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
        <span className="mono">{conversationId.slice(0, 12)}</span>
        <button className="btn small ghost call-btn" title="Voice call" onClick={() => void call.startCall(conversationId, "voice")}>
          📞
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
        <input className="input" value={draft} onChange={(e) => setDraft(e.target.value)} placeholder="Message" />
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
      {message.mine ? <span className="state">{message.state}</span> : null}
    </div>
  );
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

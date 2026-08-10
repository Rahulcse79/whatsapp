import { isValidPhone, type ChatSummary, type ThreadMessage } from "@wa/client-core";
import { useEffect, useState, type FormEvent } from "react";
import { registerWebPush } from "../push";
import { messageOf } from "./errors";
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

export function ChatList({ onOpen, onNew }: { onOpen: (id: string) => void; onNew: () => void }) {
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
        <button className="btn small" onClick={onNew}>
          ＋ New
        </button>
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

export function Thread({ conversationId, onBack }: { conversationId: string; onBack: () => void }) {
  const { services } = useServices();
  const [messages, setMessages] = useState<ThreadMessage[]>([]);
  const [draft, setDraft] = useState("");

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

  return (
    <div className="pane">
      <div className="pane-head">
        <button className="btn small ghost" onClick={onBack}>
          ‹ Back
        </button>
        <span className="mono">{conversationId.slice(0, 12)}</span>
      </div>
      <div className="messages">
        {messages.length === 0 ? <p className="muted center">Say hello 👋</p> : null}
        {messages.map((m) => (
          <div key={m.msgUuid} className={m.mine ? "bubble mine" : "bubble theirs"}>
            <span>{m.deleted ? "This message was deleted" : m.body}</span>
            {m.mine ? <span className="state">{m.state}</span> : null}
          </div>
        ))}
      </div>
      <form className="composer" onSubmit={send}>
        <input className="input" value={draft} onChange={(e) => setDraft(e.target.value)} placeholder="Message" />
        <button className="btn" type="submit">
          Send
        </button>
      </form>
    </div>
  );
}

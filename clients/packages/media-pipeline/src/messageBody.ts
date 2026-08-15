// The wire shape of a media message's *plaintext* body. The whole object is what
// gets sealed by the Signal session (like a text body), so the server never sees
// the attachments' keys or captions. Both clients encode on send and parse on
// receive through here, so a media message renders identically on each.

import type { MediaEnvelope } from "./envelope";
import type { LinkPreview } from "./linkPreview";

export interface MediaMessageBody {
  attachments: MediaEnvelope[];
  caption?: string;
}

/** A quoted reply reference carried in the sealed body (FR-MSG-04): the id of the
 *  message being replied to plus a short snippet, so the reply renders its quote
 *  even if the original isn't loaded. */
export interface QuotedRef {
  msgUuid: string;
  snippet: string;
  mine: boolean;
}

/** The decrypted view of a text message: the text plus an optional sender-side
 *  link preview (FR-MSG-08) and an optional quoted reply, all in the sealed body. */
export interface TextMessageBody {
  text: string;
  linkPreview?: LinkPreview;
  reply?: QuotedRef;
}

/** encodeMediaMessage produces the plaintext body string to seal + send. */
export function encodeMediaMessage(attachments: MediaEnvelope[], caption?: string): string {
  const body: { t: "media"; v: 1; a: MediaEnvelope[]; c?: string } = { t: "media", v: 1, a: attachments };
  if (caption && caption.trim()) body.c = caption;
  return JSON.stringify(body);
}

/** parseMediaMessage returns the attachments carried by a decrypted body, or
 *  null when the body is plain text (the common case). Tolerant of malformed
 *  input — anything that isn't a well-formed media envelope reads as text. */
export function parseMediaMessage(body: string): MediaMessageBody | null {
  if (!body || body.charAt(0) !== "{") return null;
  let parsed: unknown;
  try {
    parsed = JSON.parse(body);
  } catch {
    return null;
  }
  if (typeof parsed !== "object" || parsed === null) return null;
  const obj = parsed as Record<string, unknown>;
  if (obj.t !== "media" || !Array.isArray(obj.a)) return null;
  const attachments = obj.a.filter(isEnvelope);
  if (attachments.length === 0) return null;
  const caption = typeof obj.c === "string" ? obj.c : undefined;
  return caption !== undefined ? { attachments, caption } : { attachments };
}

/** encodeTextMessage produces the plaintext body to seal for a text message. No
 *  preview and no reply → a bare string (back-compat, the common case).
 *  Otherwise → a small tagged JSON object carrying the text, the self-contained
 *  preview, and/or the quoted reply. */
export function encodeTextMessage(text: string, preview?: LinkPreview, reply?: QuotedRef): string {
  if (!preview && !reply) return text;
  const body: { t: "text"; v: 1; text: string; lp?: LinkPreview; re?: QuotedRef } = { t: "text", v: 1, text };
  if (preview) body.lp = preview;
  if (reply) body.re = reply;
  return JSON.stringify(body);
}

/** parseTextMessage reads a decrypted text body: a plain string, or a tagged
 *  text+preview object. Media bodies are handled by parseMediaMessage first; a
 *  body that isn't a well-formed text object reads as its raw text. */
export function parseTextMessage(body: string): TextMessageBody {
  if (!body || body.charAt(0) !== "{") return { text: body };
  let parsed: unknown;
  try {
    parsed = JSON.parse(body);
  } catch {
    return { text: body };
  }
  if (typeof parsed !== "object" || parsed === null) return { text: body };
  const obj = parsed as Record<string, unknown>;
  if (obj.t !== "text" || typeof obj.text !== "string") return { text: body };
  const lp = isLinkPreview(obj.lp) ? obj.lp : undefined;
  const re = isQuotedRef(obj.re) ? obj.re : undefined;
  const out: TextMessageBody = { text: obj.text };
  if (lp) out.linkPreview = lp;
  if (re) out.reply = re;
  return out;
}

/** ReactionBody is the sealed content of a REACTION overlay: the emoji plus
 *  whether it adds or removes the reactor's reaction on the target message. */
export interface ReactionBody {
  emoji: string;
  op: "add" | "remove";
}

/** encodeReaction seals a reaction overlay's content (emoji + add/remove). */
export function encodeReaction(emoji: string, op: "add" | "remove"): string {
  return JSON.stringify({ t: "react", v: 1, emoji, op });
}

/** parseReaction reads a decrypted reaction overlay body, or null if malformed. */
export function parseReaction(body: string): ReactionBody | null {
  if (!body || body.charAt(0) !== "{") return null;
  let parsed: unknown;
  try {
    parsed = JSON.parse(body);
  } catch {
    return null;
  }
  if (typeof parsed !== "object" || parsed === null) return null;
  const obj = parsed as Record<string, unknown>;
  if (obj.t !== "react" || typeof obj.emoji !== "string") return null;
  const op = obj.op === "remove" ? "remove" : "add";
  return { emoji: obj.emoji, op };
}

function isLinkPreview(v: unknown): v is LinkPreview {
  if (typeof v !== "object" || v === null) return false;
  const p = v as Record<string, unknown>;
  return typeof p.url === "string" && typeof p.title === "string";
}

function isQuotedRef(v: unknown): v is QuotedRef {
  if (typeof v !== "object" || v === null) return false;
  const q = v as Record<string, unknown>;
  return typeof q.msgUuid === "string" && typeof q.snippet === "string" && typeof q.mine === "boolean";
}

function isEnvelope(v: unknown): v is MediaEnvelope {
  if (typeof v !== "object" || v === null) return false;
  const e = v as Record<string, unknown>;
  return (
    typeof e.objectKey === "string" &&
    typeof e.fileKey === "string" &&
    typeof e.contentHash === "string" &&
    typeof e.sizeBytes === "number" &&
    typeof e.mime === "string"
  );
}

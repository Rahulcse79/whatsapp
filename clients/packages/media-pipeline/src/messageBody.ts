// The wire shape of a media message's *plaintext* body. The whole object is what
// gets sealed by the Signal session (like a text body), so the server never sees
// the attachments' keys or captions. Both clients encode on send and parse on
// receive through here, so a media message renders identically on each.

import type { MediaEnvelope } from "./envelope";

export interface MediaMessageBody {
  attachments: MediaEnvelope[];
  caption?: string;
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

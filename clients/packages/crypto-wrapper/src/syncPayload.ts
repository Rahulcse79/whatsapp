// Multi-device payload framing (e2ee-design §2). The sealed plaintext tells the
// receiving device which kind of copy it got:
//
//   • DIRECT   — a copy for a recipient device; the conversation is the SENDER.
//   • SELF-SYNC — a copy for the sender's OWN other device; it carries `sentTo`
//     so that device knows which conversation the sent message belongs to.
//     (Signal calls this a SyncMessage.Sent.)
//
// The framing lives inside the E2EE plaintext, so the relay server never sees the
// kind or the destination — only opaque ciphertext.

const VERSION = 1;
const KIND_DIRECT = 0;
const KIND_SELF_SYNC = 1;

/** encodeDirect frames a recipient copy: [version][kind=direct][content]. */
export function encodeDirect(content: Uint8Array): Uint8Array {
  const out = new Uint8Array(2 + content.length);
  out[0] = VERSION;
  out[1] = KIND_DIRECT;
  out.set(content, 2);
  return out;
}

/** encodeSelfSync frames a self-copy: [version][kind=self][u16 len][sentTo][content]. */
export function encodeSelfSync(sentTo: string, content: Uint8Array): Uint8Array {
  const to = new TextEncoder().encode(sentTo);
  if (to.length > 0xffff) throw new Error("sync payload: sentTo too long");
  const out = new Uint8Array(4 + to.length + content.length);
  out[0] = VERSION;
  out[1] = KIND_SELF_SYNC;
  out[2] = (to.length >>> 8) & 0xff;
  out[3] = to.length & 0xff;
  out.set(to, 4);
  out.set(content, 4 + to.length);
  return out;
}

export interface DecodedPayload {
  selfSync: boolean;
  /** Present only for a self-sync copy: the recipient the message was sent to. */
  sentTo?: string;
  content: Uint8Array;
}

/** decodePayload parses a sealed plaintext back into its kind + content. */
export function decodePayload(bytes: Uint8Array): DecodedPayload {
  if (bytes.length < 2 || bytes[0] !== VERSION) throw new Error("sync payload: bad header");
  const kind = bytes[1];
  if (kind === KIND_DIRECT) {
    return { selfSync: false, content: bytes.subarray(2) };
  }
  if (kind === KIND_SELF_SYNC) {
    if (bytes.length < 4) throw new Error("sync payload: truncated self-sync header");
    const len = (bytes[2]! << 8) | bytes[3]!;
    if (4 + len > bytes.length) throw new Error("sync payload: truncated sentTo");
    const sentTo = new TextDecoder().decode(bytes.subarray(4, 4 + len));
    return { selfSync: true, sentTo, content: bytes.subarray(4 + len) };
  }
  throw new Error(`sync payload: unknown kind ${kind}`);
}

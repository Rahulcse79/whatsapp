// Frame-key derivation (rtc-lld §2: "frame keys derived from participants' Signal
// sessions"). HKDF-SHA256 turns the call's root secret — the shared secret of the
// peers' Signal session, distributed via WS E2EE signaling, never seen by the
// SFU — into a distinct AES key per (room, epoch, sender). Both peers derive the
// same key for a given sender, so no key bytes cross the wire; the epoch bumps on
// participant join/leave to give forward/backward secrecy across membership
// changes.

const SALT = utf8("wa-call-sframe-v1");

/** Names one frame key: which call, which epoch, and which participant sends
 *  under it. */
export interface FrameKeyContext {
  roomId: string;
  epoch: number;
  senderId: string;
}

/** deriveFrameKey derives a 32-byte AES key from the call root secret + context.
 *  Deterministic: identical inputs yield identical keys on both peers. */
export async function deriveFrameKey(rootSecret: Uint8Array, ctx: FrameKeyContext): Promise<Uint8Array> {
  const base = await crypto.subtle.importKey("raw", rootSecret, "HKDF", false, ["deriveBits"]);
  const info = utf8(`wa-call|${ctx.roomId}|${ctx.epoch}|${ctx.senderId}`);
  const bits = await crypto.subtle.deriveBits({ name: "HKDF", hash: "SHA-256", salt: SALT, info }, base, 256);
  return new Uint8Array(bits);
}

function utf8(s: string): Uint8Array {
  return new TextEncoder().encode(s);
}

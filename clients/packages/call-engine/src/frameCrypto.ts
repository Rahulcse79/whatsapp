// SFrame-style end-to-end media-frame encryption (rtc-lld §2). The SFU forwards
// packets it cannot read: each media frame is sealed with AES-256-GCM under a
// per-epoch frame key derived from the participants' Signal session, so only the
// call peers can decrypt. This is REAL cryptography (WebCrypto), like
// @wa/media-pipeline — not a dev double.
//
// Wire layout of a sealed frame (the header is cleartext but authenticated):
//
//     header := keyId(1) ‖ counter(8, big-endian)
//     sealed := header ‖ AES-GCM(key, iv, plaintext, aad = header)
//     iv     := baseIV(12) with its trailing 8 bytes XOR the counter
//
// The keyId selects the key on receive (so frames still in flight under the old
// key decrypt across a rotation); the monotonic per-key counter guarantees a
// unique (key, iv) per frame. baseIV is derived from the key, so both peers
// reconstruct the same nonce with no extra signaling. Each sender uses its OWN
// key (keyed by sender id in the derivation), so two peers sharing a session
// never collide on (key, iv).

const HEADER_LEN = 9; // keyId(1) + counter(8)
const IV_LEN = 12;
const KEY_BYTES = 32;

interface KeyMaterial {
  key: CryptoKey;
  baseIV: Uint8Array;
}

/** FrameCryptor seals outbound frames under the current send key and opens
 *  inbound frames by selecting the peer key named in each frame's header. */
export class FrameCryptor {
  private send: (KeyMaterial & { keyId: number }) | null = null;
  private counter = 0;
  private readonly recv = new Map<number, KeyMaterial>();

  /** setSendKey installs (or rotates) this participant's send key under keyId.
   *  The frame counter restarts, opening a fresh nonce space for the new key. */
  async setSendKey(keyId: number, raw: Uint8Array): Promise<void> {
    this.send = { keyId: keyId & 0xff, ...(await material(raw, ["encrypt"])) };
    this.counter = 0;
  }

  /** addRecvKey registers a peer key for a keyId (kept across a rotation so
   *  older in-flight frames still open). */
  async addRecvKey(keyId: number, raw: Uint8Array): Promise<void> {
    this.recv.set(keyId & 0xff, await material(raw, ["decrypt"]));
  }

  /** removeRecvKey drops a superseded peer key once its epoch is fully retired. */
  removeRecvKey(keyId: number): void {
    this.recv.delete(keyId & 0xff);
  }

  /** seal encrypts one media frame. Throws if no send key is set. */
  async seal(plaintext: Uint8Array): Promise<Uint8Array> {
    if (!this.send) throw new Error("call frame cryptor: no send key");
    const counter = this.counter++;
    const header = frameHeader(this.send.keyId, counter);
    const iv = nonce(this.send.baseIV, counter);
    const sealed = new Uint8Array(await crypto.subtle.encrypt({ name: "AES-GCM", iv, additionalData: header }, this.send.key, plaintext));
    const out = new Uint8Array(HEADER_LEN + sealed.length);
    out.set(header, 0);
    out.set(sealed, HEADER_LEN);
    return out;
  }

  /** open decrypts one sealed frame, selecting the key by its header keyId. A
   *  frame under an unknown key, or one that fails its tag, rejects. */
  async open(frame: Uint8Array): Promise<Uint8Array> {
    if (frame.length < HEADER_LEN) throw new Error("call frame too short");
    const keyId = frame[0]!;
    const counter = readU64(frame, 1);
    const km = this.recv.get(keyId);
    if (!km) throw new Error(`call frame: no key for keyId ${keyId}`);
    const header = frame.subarray(0, HEADER_LEN);
    const iv = nonce(km.baseIV, counter);
    const ct = frame.subarray(HEADER_LEN);
    return new Uint8Array(await crypto.subtle.decrypt({ name: "AES-GCM", iv, additionalData: header }, km.key, ct));
  }
}

async function material(raw: Uint8Array, usages: KeyUsage[]): Promise<KeyMaterial> {
  if (raw.length !== KEY_BYTES) throw new Error(`call frame key must be ${KEY_BYTES} bytes, got ${raw.length}`);
  const key = await crypto.subtle.importKey("raw", raw, "AES-GCM", false, usages);
  // baseIV = first 12 bytes of SHA-256(key) — deterministic, so both peers agree
  // without transmitting it.
  const digest = new Uint8Array(await crypto.subtle.digest("SHA-256", raw));
  return { key, baseIV: digest.subarray(0, IV_LEN) };
}

function frameHeader(keyId: number, counter: number): Uint8Array {
  const h = new Uint8Array(HEADER_LEN);
  h[0] = keyId & 0xff;
  writeU64(h, 1, counter);
  return h;
}

/** nonce XORs the counter into the trailing 8 bytes of baseIV (counter-mode
 *  nonce construction — unique per (key, counter)). */
function nonce(baseIV: Uint8Array, counter: number): Uint8Array {
  const iv = new Uint8Array(baseIV); // copy
  const c = new Uint8Array(8);
  writeU64(c, 0, counter);
  for (let i = 0; i < 8; i++) iv[IV_LEN - 8 + i] = iv[IV_LEN - 8 + i]! ^ c[i]!;
  return iv;
}

function writeU64(buf: Uint8Array, off: number, n: number): void {
  // n is a JS number (< 2^53); the high bytes are 0 in practice but written full.
  const hi = Math.floor(n / 0x100000000);
  const lo = n >>> 0;
  buf[off] = (hi >>> 24) & 0xff;
  buf[off + 1] = (hi >>> 16) & 0xff;
  buf[off + 2] = (hi >>> 8) & 0xff;
  buf[off + 3] = hi & 0xff;
  buf[off + 4] = (lo >>> 24) & 0xff;
  buf[off + 5] = (lo >>> 16) & 0xff;
  buf[off + 6] = (lo >>> 8) & 0xff;
  buf[off + 7] = lo & 0xff;
}

function readU64(buf: Uint8Array, off: number): number {
  let hi = 0;
  let lo = 0;
  for (let i = 0; i < 4; i++) hi = hi * 256 + buf[off + i]!;
  for (let i = 4; i < 8; i++) lo = lo * 256 + buf[off + i]!;
  return hi * 0x100000000 + lo;
}

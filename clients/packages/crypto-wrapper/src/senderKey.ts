// Group Sender-Key rotation protocol (e2ee-design.md §3). Per (group, sender
// device) there is one Sender Key chain: the sender encrypts ONCE and the
// server fans the ciphertext to every member (bandwidth-efficient at 1,024).
// Members decrypt with the sender's distributed key. On a membership change the
// ORDERED group.events drive rotation — clients execute, the server never sees
// keys:
//   • member removed → the sender rotates its key (the removed member can't read
//     forward) and redistributes to the remaining members;
//   • member added   → the sender distributes its current key to the new device
//     set (no back-history).
//
// Iron rule (e2ee-design): production uses libsignal's Sender Key group cipher;
// this is the INTEGRATION + rotation protocol behind a swappable cipher. The PRF
// below is a NON-CRYPTOGRAPHIC mock (FNV-1a + xorshift), never shipped.

import { DecryptError } from "./devSession";

/** An ordered group membership event (mirrors group.events / the group_event frame). */
export interface GroupEvent {
  kind: "member_added" | "member_removed" | "role_changed";
  subject: string; // the added/removed/changed user
  version: number; // per-group order (groups.version)
}

/** RotationPlan is what the caller must act on after applyGroupEvent: send our
 *  (possibly rotated) SenderKeyDistribution to distributeTo over pairwise
 *  Signal sessions, and forget the dropped members' keys. */
export interface RotationPlan {
  rotated: boolean;
  distributeTo: string[];
  dropped: string[];
}

/** SenderKeyDistribution (SKDM) — the chain state a member hands peers over a
 *  pairwise session so they can decrypt that member's group messages. */
export interface SenderKeyDistribution {
  chainKey: Uint8Array;
  counter: number;
}

interface ChainState {
  chainKey: Uint8Array;
  counter: number;
}

export class GroupSession {
  private readonly own = new Map<string, ChainState>(); // groupId → our chain
  private readonly peers = new Map<string, Map<string, ChainState>>(); // groupId → member → chain
  private readonly lastVersion = new Map<string, number>(); // groupId → last applied event version

  constructor(
    private readonly selfUserId: string,
    private readonly rng: () => number = Math.random,
  ) {}

  /** ensureOwn lazily creates our Sender Key for a group. */
  ensureOwn(groupId: string): void {
    if (!this.own.has(groupId)) {
      this.own.set(groupId, { chainKey: randomKey(this.rng), counter: 0 });
    }
  }

  /** distribution returns the SKDM to hand a peer (sent over a pairwise session). */
  distribution(groupId: string): SenderKeyDistribution {
    this.ensureOwn(groupId);
    const s = this.own.get(groupId)!;
    return { chainKey: s.chainKey.slice(), counter: s.counter };
  }

  /** acceptDistribution stores a peer's Sender Key from their SKDM. */
  acceptDistribution(groupId: string, from: string, skdm: SenderKeyDistribution): void {
    let m = this.peers.get(groupId);
    if (!m) {
      m = new Map();
      this.peers.set(groupId, m);
    }
    m.set(from, { chainKey: skdm.chainKey.slice(), counter: skdm.counter });
  }

  /** hasPeer reports whether we can decrypt messages from `from` in a group. */
  hasPeer(groupId: string, from: string): boolean {
    return this.peers.get(groupId)?.has(from) ?? false;
  }

  /** encrypt seals plaintext once with our Sender Key; the server fans it out. */
  encrypt(groupId: string, plaintext: Uint8Array): Uint8Array {
    this.ensureOwn(groupId);
    const s = this.own.get(groupId)!;
    const n = s.counter++;
    return seal(messageKey(s.chainKey, n), n, plaintext);
  }

  /** decrypt opens a group message from `from` using their distributed key. */
  decrypt(groupId: string, from: string, envelope: Uint8Array): Uint8Array {
    const chain = this.peers.get(groupId)?.get(from);
    if (!chain) throw new Error(`no sender key for ${from} in ${groupId}`);
    const n = readU32(envelope, 0);
    return open(messageKey(chain.chainKey, n), envelope);
  }

  /**
   * applyGroupEvent runs the ordered rotation protocol. It is idempotent and
   * order-safe: an event whose version is not newer than the last applied one
   * is ignored (NATS delivers group.events in per-group order, but redelivery
   * is at-least-once). `members` is the CURRENT member set after the event.
   */
  applyGroupEvent(groupId: string, ev: GroupEvent, members: string[]): RotationPlan {
    const noop: RotationPlan = { rotated: false, distributeTo: [], dropped: [] };
    if (ev.version <= (this.lastVersion.get(groupId) ?? 0)) return noop;
    this.lastVersion.set(groupId, ev.version);

    switch (ev.kind) {
      case "member_added":
        if (ev.subject === this.selfUserId) return noop;
        return { rotated: false, distributeTo: [ev.subject], dropped: [] };

      case "member_removed":
        this.peers.get(groupId)?.delete(ev.subject);
        if (ev.subject === this.selfUserId) {
          this.own.delete(groupId); // we were removed — forget our key
          return { rotated: false, distributeTo: [], dropped: [ev.subject] };
        }
        // Rotate so the removed member cannot read forward, then redistribute.
        this.own.set(groupId, { chainKey: randomKey(this.rng), counter: 0 });
        return {
          rotated: true,
          distributeTo: members.filter((u) => u !== this.selfUserId),
          dropped: [ev.subject],
        };

      default:
        return noop;
    }
  }
}

// ── mock primitives (INSECURE — libsignal replaces these) ───────────────────

function randomKey(rng: () => number): Uint8Array {
  const out = new Uint8Array(32);
  for (let i = 0; i < 32; i++) out[i] = Math.floor(rng() * 256);
  return out;
}

function messageKey(chainKey: Uint8Array, n: number): Uint8Array {
  return digest(concat(chainKey, u32(n)));
}

function seal(mk: Uint8Array, n: number, plaintext: Uint8Array): Uint8Array {
  const ct = xor(plaintext, keystream(mk, plaintext.length));
  return concat(u32(n), mac(mk, ct), ct); // n(4) || tag(8) || ct
}

function open(mk: Uint8Array, envelope: Uint8Array): Uint8Array {
  const tag = envelope.subarray(4, 12);
  const ct = envelope.subarray(12);
  if (!bytesEqual(tag, mac(mk, ct))) throw new DecryptError();
  return xor(ct, keystream(mk, ct.length));
}

function mac(mk: Uint8Array, ct: Uint8Array): Uint8Array {
  return digest(concat(utf8("mac"), mk, ct)).subarray(0, 8);
}

function keystream(mk: Uint8Array, len: number): Uint8Array {
  const out = new Uint8Array(len);
  let filled = 0;
  for (let ctr = 0; filled < len; ctr++) {
    const block = digest(concat(utf8("ks"), mk, u32(ctr)));
    const take = Math.min(block.length, len - filled);
    out.set(block.subarray(0, take), filled);
    filled += take;
  }
  return out;
}

function digest(input: Uint8Array): Uint8Array {
  let h = 0x811c9dc5 >>> 0;
  for (let i = 0; i < input.length; i++) {
    h = (h ^ input[i]!) >>> 0;
    h = Math.imul(h, 0x01000193) >>> 0;
  }
  let x = (h ^ 0x9e3779b9) >>> 0;
  if (x === 0) x = 0x1a2b3c4d;
  const out = new Uint8Array(32);
  for (let i = 0; i < 32; i++) {
    x ^= x << 13;
    x >>>= 0;
    x ^= x >>> 17;
    x ^= x << 5;
    x >>>= 0;
    out[i] = x & 0xff;
  }
  return out;
}

function utf8(s: string): Uint8Array {
  const out = new Uint8Array(s.length);
  for (let i = 0; i < s.length; i++) out[i] = s.charCodeAt(i) & 0xff;
  return out;
}

function concat(...arrays: Uint8Array[]): Uint8Array {
  let len = 0;
  for (const a of arrays) len += a.length;
  const out = new Uint8Array(len);
  let off = 0;
  for (const a of arrays) {
    out.set(a, off);
    off += a.length;
  }
  return out;
}

function xor(data: Uint8Array, key: Uint8Array): Uint8Array {
  const out = new Uint8Array(data.length);
  for (let i = 0; i < data.length; i++) out[i] = data[i]! ^ key[i]!;
  return out;
}

function u32(n: number): Uint8Array {
  return new Uint8Array([(n >>> 24) & 0xff, (n >>> 16) & 0xff, (n >>> 8) & 0xff, n & 0xff]);
}

function readU32(buf: Uint8Array, off: number): number {
  return ((buf[off]! << 24) | (buf[off + 1]! << 16) | (buf[off + 2]! << 8) | buf[off + 3]!) >>> 0;
}

function bytesEqual(a: Uint8Array, b: Uint8Array): boolean {
  if (a.length !== b.length) return false;
  let d = 0;
  for (let i = 0; i < a.length; i++) d |= a[i]! ^ b[i]!;
  return d === 0;
}

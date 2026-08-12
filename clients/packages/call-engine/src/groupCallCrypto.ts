// GroupCallCrypto — E2EE frame crypto for a group call (≤ 32; HLD §10.4,
// e2ee-design §7). Each participant seals its own frames with a per-(room, epoch,
// self) key; every other member derives that participant's key to open them
// ("keys derive from pairwise sessions" — here a shared group root stands in for
// the distributed sender keys, the same dev-double seam as 1:1). Because
// FrameCryptor selects a receive key by keyId=epoch alone, each remote sender
// needs its OWN receive cryptor (two senders share an epoch keyId, so a single
// map would collide). Epochs bump on join/leave (call-ctl signals the epoch),
// which is what gives forward secrecy: a member who left cannot open the new
// epoch, and a member who joined cannot open prior ones. The previous epoch's
// keys are retained one generation so frames already in flight during a rotation
// still decrypt.

import { FrameCryptor } from "./frameCrypto";
import { deriveFrameKey } from "./keyDerivation";

export interface GroupCallContext {
  roomId: string;
  selfId: string;
}

export class GroupCallCrypto {
  private readonly send = new FrameCryptor();
  private readonly recv = new Map<string, FrameCryptor>();
  private readonly members = new Set<string>();
  private epoch = -1;

  constructor(
    private readonly root: Uint8Array,
    private readonly ctx: GroupCallContext,
    initialPeers: string[] = [],
  ) {
    for (const p of initialPeers) this.members.add(p);
  }

  /** start installs `epoch` (default 0) for self + all current peers. */
  start(epoch = 0): Promise<void> {
    return this.rotate(epoch);
  }

  /** memberJoined adds a peer and rotates to the call-ctl-signalled epoch. The
   *  joiner only gets keys from this epoch on (it cannot open prior frames). */
  async memberJoined(peerId: string, epoch: number): Promise<void> {
    this.members.add(peerId);
    await this.rotate(epoch);
  }

  /** memberLeft drops a peer's keys and rotates to the new epoch, so the leaver's
   *  now-stale keys cannot open anything from here on (forward secrecy). */
  async memberLeft(peerId: string, epoch: number): Promise<void> {
    this.members.delete(peerId);
    this.recv.delete(peerId);
    await this.rotate(epoch);
  }

  /** rotate re-derives the send key and every current peer's receive key for
   *  `epoch`, retiring keys two generations back. */
  async rotate(epoch: number): Promise<void> {
    const { roomId, selfId } = this.ctx;
    await this.send.setSendKey(epoch, await deriveFrameKey(this.root, { roomId, epoch, senderId: selfId }));
    for (const p of this.members) {
      const cryptor = this.recv.get(p) ?? this.wire(p);
      await cryptor.addRecvKey(epoch, await deriveFrameKey(this.root, { roomId, epoch, senderId: p }));
      if (epoch >= 2) cryptor.removeRecvKey(epoch - 2);
    }
    this.epoch = epoch;
  }

  currentEpoch(): number {
    return this.epoch;
  }

  roster(): string[] {
    return [...this.members];
  }

  /** seal encrypts an outbound frame with self's current-epoch key. */
  seal(frame: Uint8Array): Promise<Uint8Array> {
    return this.send.seal(frame);
  }

  /** openFrom decrypts a frame received on `peerId`'s track. */
  openFrom(peerId: string, frame: Uint8Array): Promise<Uint8Array> {
    const cryptor = this.recv.get(peerId);
    if (!cryptor) return Promise.reject(new Error(`group-call: no key for peer ${peerId}`));
    return cryptor.open(frame);
  }

  private wire(peerId: string): FrameCryptor {
    const c = new FrameCryptor();
    this.recv.set(peerId, c);
    return c;
  }
}

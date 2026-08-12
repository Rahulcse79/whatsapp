// CallCrypto is the per-call E2EE surface the media transport talks to: it owns
// the current epoch's frame keys and exposes seal()/open() for the insertable-
// streams transforms. On a membership change (rtc-lld §2: "key rotation on
// participant join/leave") the app bumps the epoch; the previous receive key is
// retained one generation so frames already in flight still open.

import { FrameCryptor } from "./frameCrypto";
import { deriveFrameKey } from "./keyDerivation";

/** Identity of the two ends of a 1:1 call, for per-sender key derivation. */
export interface CallPeers {
  roomId: string;
  selfId: string;
  peerId: string;
}

export class CallCrypto {
  private readonly cryptor = new FrameCryptor();
  private epoch = -1;

  constructor(
    private readonly rootSecret: Uint8Array,
    private readonly peers: CallPeers,
  ) {}

  /** start installs the initial (epoch 0) keys. Call once before media flows. */
  start(): Promise<void> {
    return this.rotate(0);
  }

  /** rotate installs the keys for `epoch`, keeping the immediately-previous
   *  receive key so in-flight frames under it still decrypt. */
  async rotate(epoch: number): Promise<void> {
    const { roomId, selfId, peerId } = this.peers;
    const sendKey = await deriveFrameKey(this.rootSecret, { roomId, epoch, senderId: selfId });
    const recvKey = await deriveFrameKey(this.rootSecret, { roomId, epoch, senderId: peerId });
    await this.cryptor.setSendKey(epoch, sendKey);
    await this.cryptor.addRecvKey(epoch, recvKey);
    // Retire the key two generations back (the previous one stays for in-flight).
    if (epoch >= 2) this.cryptor.removeRecvKey(epoch - 2);
    this.epoch = epoch;
  }

  /** currentEpoch is the installed epoch (-1 before start). */
  currentEpoch(): number {
    return this.epoch;
  }

  /** seal encrypts an outbound media frame. */
  seal(frame: Uint8Array): Promise<Uint8Array> {
    return this.cryptor.seal(frame);
  }

  /** open decrypts an inbound media frame. */
  open(frame: Uint8Array): Promise<Uint8Array> {
    return this.cryptor.open(frame);
  }
}

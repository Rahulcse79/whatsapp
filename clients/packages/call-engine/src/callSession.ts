// CallSession orchestrates a 1:1 call: it drives the control-plane REST
// (call-ctl), the media transport (LiveKit + insertable-streams E2EE), and the
// per-call frame crypto off the pure state machine (callState.ts). The app feeds
// it local actions (startCall/accept/decline/hangup) and relayed server signals
// (onOffer/onRing/onRemoteEnd); it emits the current CallState to a listener.

import { CallCrypto } from "./callCrypto";
import { active, IDLE, reduce, type CallEvent, type CallKind, type CallState, type RingSignal } from "./callState";

/** CreateResult mirrors POST /v1/calls. */
export interface CreateResult {
  roomId: string;
  ringId: string;
  joinToken: string;
}

/** CallControl is the call-ctl REST surface (calls-ptt-api.md §Calls). */
export interface CallControl {
  create(peerId: string, kind: CallKind): Promise<CreateResult>;
  answer(ringId: string): Promise<string>; // → join token
  decline(ringId: string): Promise<void>;
  rejoin(roomId: string): Promise<string>; // → fresh join token (ICE recovery)
}

/** MediaConnectOptions carry everything the transport needs to join the SFU room
 *  and install the E2EE frame transforms. */
export interface MediaConnectOptions {
  roomId: string;
  joinToken: string;
  crypto: CallCrypto;
}

/** MediaTransport is the LiveKit/WebRTC seam. connect resolves once media is
 *  flowing; it rejects if the media path can't be established. */
export interface MediaTransport {
  connect(opts: MediaConnectOptions): Promise<void>;
  disconnect(): Promise<void>;
}

/** RootSecretProvider yields the call's E2EE root secret from the peer's Signal
 *  session (crypto-wrapper). The SFU never sees it. */
export interface RootSecretProvider {
  callSecret(peerId: string, roomId: string): Promise<Uint8Array>;
}

export class CallSession {
  private state: CallState = IDLE;
  private crypto: CallCrypto | null = null;
  private joinToken = "";

  constructor(
    private readonly control: CallControl,
    private readonly media: MediaTransport,
    private readonly secrets: RootSecretProvider,
    private readonly selfId: string,
    private readonly listener?: (s: CallState) => void,
  ) {}

  getState(): CallState {
    return this.state;
  }

  /** startCall (caller) creates the call and begins ringing. Media connects once
   *  the callee answers (onRing "answered"). */
  async startCall(peerId: string, kind: CallKind): Promise<void> {
    if (this.state.phase !== "idle") return;
    const res = await this.control.create(peerId, kind);
    this.joinToken = res.joinToken;
    this.dispatch({ t: "start", peerId, roomId: res.roomId, ringId: res.ringId, kind });
  }

  /** onOffer (callee) surfaces an incoming call. */
  onOffer(peerId: string, roomId: string, ringId: string, kind: CallKind): void {
    this.dispatch({ t: "offer", peerId, roomId, ringId, kind });
  }

  /** accept (callee) answers, fetches a join token, and connects media. */
  async accept(): Promise<void> {
    if (this.state.phase !== "incoming" || !this.state.ringId) return;
    this.joinToken = await this.control.answer(this.state.ringId);
    this.dispatch({ t: "accept" });
    await this.connectMedia();
  }

  /** decline (callee) rejects the call. */
  async decline(): Promise<void> {
    const ringId = this.state.ringId;
    if (this.state.phase !== "incoming" || !ringId) return;
    this.dispatch({ t: "ring", state: "declined" });
    await this.control.decline(ringId);
  }

  /** onRing applies a relayed server ring update. When the caller learns the
   *  call was answered, media connects. */
  async onRing(signal: RingSignal): Promise<void> {
    const wasOutgoing = this.state.phase === "outgoing";
    this.dispatch({ t: "ring", state: signal });
    if (signal === "answered" && wasOutgoing && this.state.phase === "connecting") {
      await this.connectMedia();
    }
  }

  /** onRemoteEnd handles a peer/server end (CallEnd frame). */
  async onRemoteEnd(reason: string): Promise<void> {
    if (this.state.phase === "ended") return;
    this.dispatch({ t: "remote-end", reason });
    await this.media.disconnect();
  }

  /** hangup ends the call locally and tears down media. */
  async hangup(): Promise<void> {
    if (!active(this.state)) return;
    this.dispatch({ t: "hangup" });
    await this.media.disconnect();
  }

  /** rotateKeys bumps the E2EE epoch (participant join/leave). */
  async rotateKeys(epoch: number): Promise<void> {
    if (this.crypto) await this.crypto.rotate(epoch);
  }

  private async connectMedia(): Promise<void> {
    const { peerId, roomId } = this.state;
    if (!peerId || !roomId) return;
    try {
      const secret = await this.secrets.callSecret(peerId, roomId);
      this.crypto = new CallCrypto(secret, { roomId, selfId: this.selfId, peerId });
      await this.crypto.start();
      await this.media.connect({ roomId, joinToken: this.joinToken, crypto: this.crypto });
      this.dispatch({ t: "media-connected" });
    } catch {
      this.dispatch({ t: "media-failed" });
      await this.media.disconnect();
    }
  }

  private dispatch(ev: CallEvent): void {
    const next = reduce(this.state, ev);
    if (next === this.state) return;
    this.state = next;
    this.listener?.(next);
  }
}

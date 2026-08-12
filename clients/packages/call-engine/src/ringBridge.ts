// RingBridge wires the native lock-screen call UI (iOS CallKit / Android
// ConnectionService, via react-native-callkeep) to the CallSession
// (mobile-app-architecture.md §3). It is framework-free so the routing is tested
// here; the platform adapter supplies the NativeRinger.
//
// The hard iOS constraint drives the shape: a PushKit VoIP push MUST report an
// incoming call to CallKit *immediately and synchronously*, or iOS terminates the
// app and stops delivering VoIP pushes. So `incoming()` calls reportIncoming
// FIRST, before surfacing the offer to the (async) session.

import type { CallKind, CallPhase, CallState } from "./callState";

/** The minimum a VoIP push carries to ring a device. */
export interface IncomingCall {
  ringId: string;
  roomId: string;
  peerId: string;
  kind: CallKind;
}

/** NativeRinger is the CallKit/ConnectionService seam (react-native-callkeep). */
export interface NativeRinger {
  /** Show the native incoming-call UI. MUST be safe to call synchronously. */
  reportIncoming(call: IncomingCall): void;
  /** Show an outgoing/active call in the OS call UI. */
  reportOutgoing(call: IncomingCall): void;
  /** Mark the call connected (CallKit "connected" state). */
  reportConnected(ringId: string): void;
  /** Tear down the native call UI. */
  reportEnded(ringId: string, reason: string): void;
  /** The user answered from the lock screen. */
  onAnswer(cb: (ringId: string) => void): void;
  /** The user ended/declined from the lock screen. */
  onEnd(cb: (ringId: string) => void): void;
}

/** CallActions is the slice of CallSession the bridge drives. */
export interface CallActions {
  accept(): Promise<void>;
  decline(): Promise<void>;
  hangup(): Promise<void>;
  onOffer(peerId: string, roomId: string, ringId: string, kind: CallKind): void;
}

export class RingBridge {
  private phase: CallPhase = "idle";
  private ringId = "";

  constructor(
    private readonly ringer: NativeRinger,
    private readonly actions: CallActions,
  ) {
    this.ringer.onAnswer(() => {
      void this.actions.accept();
    });
    this.ringer.onEnd(() => {
      // Ending while still ringing is a decline (notifies the caller via the
      // server); ending an active call is a hangup.
      void (this.phase === "incoming" ? this.actions.decline() : this.actions.hangup());
    });
  }

  /** incoming is called by the VoIP-push handler. It reports to CallKit first
   *  (the iOS immediacy rule), then surfaces the offer to the session. */
  incoming(call: IncomingCall): void {
    this.ringId = call.ringId;
    this.phase = "incoming";
    this.ringer.reportIncoming(call);
    this.actions.onOffer(call.peerId, call.roomId, call.ringId, call.kind);
  }

  /** onState mirrors CallSession state changes onto the native call UI. */
  onState(state: CallState): void {
    const prev = this.phase;
    this.phase = state.phase;
    if (state.ringId) this.ringId = state.ringId;

    if (state.phase === "outgoing" && prev !== "outgoing" && state.roomId && state.ringId && state.peerId && state.kind) {
      this.ringer.reportOutgoing({ ringId: state.ringId, roomId: state.roomId, peerId: state.peerId, kind: state.kind });
    } else if (state.phase === "connected" && prev !== "connected") {
      this.ringer.reportConnected(this.ringId);
    } else if (state.phase === "ended" && prev !== "ended") {
      this.ringer.reportEnded(this.ringId, state.endReason ?? "");
    }
  }
}

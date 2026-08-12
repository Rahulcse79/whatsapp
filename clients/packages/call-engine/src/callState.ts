// The client-side 1:1 call lifecycle (sequence-diagrams §6). It mirrors the
// server ring machine (call-ctl) and adds the client's media phases: a call goes
// ringing → connecting (once answered) → connected (once media is up), or to a
// terminal ended with a reason. Pure reducer — no I/O — so every path is tested;
// the orchestrator (callSession.ts) drives ports off the state it returns.

export type CallPhase = "idle" | "outgoing" | "incoming" | "connecting" | "connected" | "ended";
export type CallDirection = "outgoing" | "incoming";
export type CallKind = "voice" | "video";

/** A ring update relayed from call-ctl (values match the server's RingState). */
export type RingSignal = "ringing" | "answered" | "answered_elsewhere" | "declined" | "busy" | "missed" | "ended";

export interface CallState {
  phase: CallPhase;
  direction?: CallDirection;
  peerId?: string;
  roomId?: string;
  ringId?: string;
  kind?: CallKind;
  /** Set when phase === "ended": why the call ended. */
  endReason?: string;
}

export const IDLE: CallState = { phase: "idle" };

export type CallEvent =
  | { t: "start"; peerId: string; roomId: string; ringId: string; kind: CallKind } // local: caller created the call
  | { t: "offer"; peerId: string; roomId: string; ringId: string; kind: CallKind } // incoming offer arrived
  | { t: "accept" } // local: callee accepted
  | { t: "media-connected" } // media path is up
  | { t: "media-failed" } // media path failed
  | { t: "ring"; state: RingSignal } // server ring update
  | { t: "hangup" } // local: user hung up
  | { t: "remote-end"; reason: string }; // call ended by peer/server

/** reduce applies one event to the call state. From "ended" (terminal) nothing
 *  changes. Unhandled events for a phase are no-ops (defensive against races). */
export function reduce(state: CallState, ev: CallEvent): CallState {
  if (state.phase === "ended") return state;

  switch (ev.t) {
    case "start":
      if (state.phase !== "idle") return state;
      return { phase: "outgoing", direction: "outgoing", peerId: ev.peerId, roomId: ev.roomId, ringId: ev.ringId, kind: ev.kind };

    case "offer":
      if (state.phase !== "idle") return state; // already in a call → ignore (caller sees busy)
      return { phase: "incoming", direction: "incoming", peerId: ev.peerId, roomId: ev.roomId, ringId: ev.ringId, kind: ev.kind };

    case "accept":
      return state.phase === "incoming" ? { ...state, phase: "connecting" } : state;

    case "media-connected":
      return state.phase === "connecting" ? { ...state, phase: "connected" } : state;

    case "media-failed":
      return ended(state, "media_failed");

    case "hangup":
      return ended(state, "hangup");

    case "remote-end":
      return ended(state, ev.reason);

    case "ring":
      return onRing(state, ev.state);
  }
}

function onRing(state: CallState, signal: RingSignal): CallState {
  switch (signal) {
    case "answered":
      // The caller learns the callee accepted → move to connecting.
      return state.phase === "outgoing" ? { ...state, phase: "connecting" } : state;
    case "answered_elsewhere":
      // A sibling device answered — this device stops ringing.
      return state.phase === "incoming" ? ended(state, "answered_elsewhere") : state;
    case "declined":
      // Ends both ends: the caller learns the callee declined; the callee's own
      // decline is echoed locally through the same signal.
      return state.phase === "outgoing" || state.phase === "incoming" ? ended(state, "declined") : state;
    case "busy":
      return state.phase === "outgoing" ? ended(state, "busy") : state;
    case "missed":
      return state.phase === "outgoing" || state.phase === "incoming" ? ended(state, "missed") : state;
    case "ended":
      return ended(state, "ended");
    case "ringing":
      return state; // informational
  }
}

function ended(state: CallState, reason: string): CallState {
  return { ...state, phase: "ended", endReason: reason };
}

/** active reports whether a call is in progress (any non-terminal, non-idle). */
export function active(state: CallState): boolean {
  return state.phase !== "idle" && state.phase !== "ended";
}

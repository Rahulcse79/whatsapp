// CallProvider (mobile) mirrors the web client: one CallSession per authed
// device, call actions + live state exposed via useCall. Incoming offers / ring
// updates arrive as WS call frames on dev.{deviceId}.call — surfaced through
// onOffer/onRing/onRemoteEnd once the gateway forwards that subject (wire-codec
// seam). Media is a react-native-webrtc seam (rnCallMedia); the call state
// machine + E2EE key setup are fully wired.

import {
  CallSession,
  createDevRootSecretProvider,
  IDLE,
  RingBridge,
  type CallActions,
  type CallKind,
  type CallState,
  type IncomingCall,
  type NativeRinger,
  type RingSignal,
} from "@wa/call-engine";
import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { defaultConfig } from "../services/appServices";
import { useServices } from "../ui/ServicesContext";
import { createCallControl } from "./callControl";
import { RnCallMedia, type RnRtcSession } from "./rnCallMedia";

// No-op RTC session until react-native-webrtc is wired.
const stubRtc: RnRtcSession = {
  join: () => Promise.resolve(),
  leave: () => Promise.resolve(),
};

// No-op native ringer until the callkeep + PushKit native deps are added (they
// need config plugins). Swap for createCallKeepRinger(RNCallKeep) and wire
// installVoipPush(...) → api.incoming() at app startup; the RingBridge routing
// (callKeepRinger.ts / voipPush.ts) is already complete. Until then, in-app calls
// still ring via CallOverlay.
const noopRinger: NativeRinger = {
  reportIncoming: () => {},
  reportOutgoing: () => {},
  reportConnected: () => {},
  reportEnded: () => {},
  onAnswer: () => {},
  onEnd: () => {},
};

export interface CallApi {
  state: CallState;
  startCall(peerId: string, kind?: CallKind): Promise<void>;
  accept(): Promise<void>;
  decline(): Promise<void>;
  hangup(): Promise<void>;
  onOffer(peerId: string, roomId: string, ringId: string, kind: CallKind): void;
  onRing(signal: RingSignal): Promise<void>;
  onRemoteEnd(reason: string): Promise<void>;
  /** Called by the VoIP-push handler: reports to CallKit then rings in-app. */
  incoming(call: IncomingCall): void;
}

const CallContext = createContext<CallApi | null>(null);

export function CallProvider({ children }: { children: ReactNode }) {
  const { services } = useServices();
  const [state, setState] = useState<CallState>(IDLE);

  const { session, bridge } = useMemo(() => {
    const token = (): string => services.sessions.current()?.accessJwt ?? "";
    const selfId = services.sessions.current()?.deviceId ?? "self";
    let ring: RingBridge | undefined;
    const s = new CallSession(
      createCallControl(defaultConfig.apiBaseUrl, token),
      new RnCallMedia(stubRtc),
      createDevRootSecretProvider(selfId, "dev-seed"),
      selfId,
      (next) => {
        setState(next);
        ring?.onState(next); // mirror state onto the native call UI
      },
    );
    const actions: CallActions = {
      accept: () => s.accept(),
      decline: () => s.decline(),
      hangup: () => s.hangup(),
      onOffer: (p, r, ri, k) => s.onOffer(p, r, ri, k),
    };
    ring = new RingBridge(noopRinger, actions);
    return { session: s, bridge: ring };
  }, [services]);

  useEffect(() => setState(session.getState()), [session]);

  const api: CallApi = {
    state,
    startCall: (peerId, kind = "voice") => session.startCall(peerId, kind),
    accept: () => session.accept(),
    decline: () => session.decline(),
    hangup: () => session.hangup(),
    onOffer: (peerId, roomId, ringId, kind) => session.onOffer(peerId, roomId, ringId, kind),
    onRing: (signal) => session.onRing(signal),
    onRemoteEnd: (reason) => session.onRemoteEnd(reason),
    incoming: (call) => bridge.incoming(call),
  };

  return <CallContext.Provider value={api}>{children}</CallContext.Provider>;
}

export function useCall(): CallApi {
  const ctx = useContext(CallContext);
  if (!ctx) throw new Error("useCall must be used inside <CallProvider>");
  return ctx;
}

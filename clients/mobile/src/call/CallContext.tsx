// CallProvider (mobile) mirrors the web client: one CallSession per authed
// device, call actions + live state exposed via useCall. Incoming offers / ring
// updates arrive as WS call frames on dev.{deviceId}.call — surfaced through
// onOffer/onRing/onRemoteEnd once the gateway forwards that subject (wire-codec
// seam). Media is a react-native-webrtc seam (rnCallMedia); the call state
// machine + E2EE key setup are fully wired.

import { CallSession, createDevRootSecretProvider, IDLE, type CallKind, type CallState, type RingSignal } from "@wa/call-engine";
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

export interface CallApi {
  state: CallState;
  startCall(peerId: string, kind?: CallKind): Promise<void>;
  accept(): Promise<void>;
  decline(): Promise<void>;
  hangup(): Promise<void>;
  onOffer(peerId: string, roomId: string, ringId: string, kind: CallKind): void;
  onRing(signal: RingSignal): Promise<void>;
  onRemoteEnd(reason: string): Promise<void>;
}

const CallContext = createContext<CallApi | null>(null);

export function CallProvider({ children }: { children: ReactNode }) {
  const { services } = useServices();
  const [state, setState] = useState<CallState>(IDLE);

  const session = useMemo(() => {
    const token = (): string => services.sessions.current()?.accessJwt ?? "";
    const selfId = services.sessions.current()?.deviceId ?? "self";
    return new CallSession(
      createCallControl(defaultConfig.apiBaseUrl, token),
      new RnCallMedia(stubRtc),
      createDevRootSecretProvider(selfId, "dev-seed"),
      selfId,
      setState,
    );
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
  };

  return <CallContext.Provider value={api}>{children}</CallContext.Provider>;
}

export function useCall(): CallApi {
  const ctx = useContext(CallContext);
  if (!ctx) throw new Error("useCall must be used inside <CallProvider>");
  return ctx;
}

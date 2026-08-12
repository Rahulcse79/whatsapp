// CallProvider (mobile) mirrors the web client: one CallSession per authed
// device, call actions + live state exposed via useCall. Incoming offers / ring
// updates arrive as WS call frames on dev.{deviceId}.call — surfaced through
// onOffer/onRing/onRemoteEnd once the gateway forwards that subject (wire-codec
// seam). Media is a react-native-webrtc seam (rnCallMedia); the call state
// machine + E2EE key setup are fully wired.

import {
  CallSession,
  CameraController,
  createDevRootSecretProvider,
  IDLE,
  RingBridge,
  type CallActions,
  type CallKind,
  type CallState,
  type CameraState,
  type IncomingCall,
  type NativeRinger,
  type RingSignal,
} from "@wa/call-engine";
import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { defaultConfig } from "../services/appServices";
import { useServices } from "../ui/ServicesContext";
import { createCallControl } from "./callControl";
import { RnCallMedia, type RnRtcSession } from "./rnCallMedia";
import { createRnCamera, type RnMediaDevices } from "./rnCamera";

// No-op RTC session until react-native-webrtc is wired.
const stubRtc: RnRtcSession = {
  join: () => Promise.resolve(),
  leave: () => Promise.resolve(),
  publishVideo: () => {},
};

// react-native-webrtc's `mediaDevices` (a native dep with a config plugin) is
// injected here; until it is added, capture is a no-op that keeps the camera
// state machine + simulcast params exercisable. Swap for `mediaDevices` from
// "react-native-webrtc" at app wiring time.
const stubMediaDevices: RnMediaDevices = {
  getUserMedia: () =>
    Promise.resolve({ toURL: () => "", getVideoTracks: () => [], release: () => {} }),
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
  camera: CameraState;
  /** Current self-preview stream URL for `<RTCView>`, or null when the camera is off. */
  localStreamURL(): string | null;
  startCall(peerId: string, kind?: CallKind): Promise<void>;
  accept(): Promise<void>;
  decline(): Promise<void>;
  hangup(): Promise<void>;
  toggleCamera(): Promise<void>;
  flipCamera(): Promise<void>;
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
  const [camera, setCamera] = useState<CameraState>({ enabled: false, facing: "front" });

  const { session, bridge, controller, rnCam } = useMemo(() => {
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
    const cam = createRnCamera(stubMediaDevices, (track, encodings) => stubRtc.publishVideo?.(track, encodings));
    const ctrl = new CameraController(cam.device, setCamera);
    return { session: s, bridge: ring, controller: ctrl, rnCam: cam };
  }, [services]);

  useEffect(() => setState(session.getState()), [session]);
  // Release the camera when a call ends or the screen goes idle.
  useEffect(() => {
    if (state.phase === "ended" || state.phase === "idle") void controller.disable();
  }, [state.phase, controller]);

  const api: CallApi = {
    state,
    camera,
    localStreamURL: () => rnCam.streamURL(),
    startCall: (peerId, kind = "voice") => session.startCall(peerId, kind),
    accept: () => session.accept(),
    decline: () => session.decline(),
    hangup: () => session.hangup(),
    toggleCamera: () => controller.toggle(),
    flipCamera: () => controller.flip(),
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

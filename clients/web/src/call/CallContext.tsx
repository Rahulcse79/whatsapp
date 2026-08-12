// CallProvider builds one CallSession for the authed device and exposes call
// actions + live state to the UI. Outgoing calls + answer/decline go through the
// control plane (REST); incoming offers and ring updates arrive as WS call frames
// on dev.{deviceId}.call — surfaced here via onOffer/onRing/onRemoteEnd once the
// gateway forwards that subject and the WsClient decodes call frames (the same
// wire-codec seam the rest of the client rides). Media is a documented LiveKit
// seam (T2.05); the call state machine + E2EE frame engine are fully wired.

import { CallSession, CameraController, createDevRootSecretProvider, IDLE, type CallKind, type CallState, type CameraState, type RingSignal } from "@wa/call-engine";
import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { config } from "../config";
import { useServices } from "../ui/ServicesContext";
import { createCallControl } from "./callControl";
import { createWebCamera } from "./webCamera";
import { WebCallMedia, type RtcSession } from "./webCallMedia";

// Until livekit-client is wired, media join is a no-op: the control plane, ring
// state machine, E2EE key setup, camera capture, and simulcast params all run;
// only the RTP path is absent.
const stubRtc: RtcSession = {
  join: () => Promise.resolve(),
  senders: () => [],
  receivers: () => [],
  onTrackSubscribed: () => {},
  publishVideo: () => {},
  leave: () => Promise.resolve(),
};

export interface CallApi {
  state: CallState;
  camera: CameraState;
  /** The local camera track, for a self-preview element. */
  localVideo(): MediaStreamTrack | null;
  startCall(peerId: string, kind?: CallKind): Promise<void>;
  accept(): Promise<void>;
  decline(): Promise<void>;
  hangup(): Promise<void>;
  toggleCamera(): Promise<void>;
  flipCamera(): Promise<void>;
  /** Signaling hooks the WS layer calls when call frames arrive. */
  onOffer(peerId: string, roomId: string, ringId: string, kind: CallKind): void;
  onRing(signal: RingSignal): Promise<void>;
  onRemoteEnd(reason: string): Promise<void>;
}

const CallContext = createContext<CallApi | null>(null);

export function CallProvider({ children }: { children: ReactNode }) {
  const { services } = useServices();
  const [state, setState] = useState<CallState>(IDLE);
  const [camera, setCamera] = useState<CameraState>({ enabled: false, facing: "front" });

  const { session, controller, webCam } = useMemo(() => {
    const token = (): string => services.sessions.current()?.accessJwt ?? "";
    const selfId = services.sessions.current()?.deviceId ?? "self";
    const s = new CallSession(
      createCallControl(config.apiBaseUrl, token),
      new WebCallMedia(stubRtc),
      createDevRootSecretProvider(selfId, "dev-seed"),
      selfId,
      setState,
    );
    const cam = createWebCamera((track, encodings) => stubRtc.publishVideo?.(track, encodings));
    const ctrl = new CameraController(cam.device, setCamera);
    return { session: s, controller: ctrl, webCam: cam };
  }, [services]);

  // Reset the surfaced state whenever the session is rebuilt; release the camera
  // when a call ends.
  useEffect(() => setState(session.getState()), [session]);
  useEffect(() => {
    if (state.phase === "ended" || state.phase === "idle") void controller.disable();
  }, [state.phase, controller]);

  const api: CallApi = {
    state,
    camera,
    localVideo: () => webCam.track(),
    startCall: (peerId, kind = "voice") => session.startCall(peerId, kind),
    accept: () => session.accept(),
    decline: () => session.decline(),
    hangup: () => session.hangup(),
    toggleCamera: () => controller.toggle(),
    flipCamera: () => controller.flip(),
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

// CallProvider builds one CallSession for the authed device and exposes call
// actions + live state to the UI. Outgoing calls + answer/decline go through the
// control plane (REST); incoming offers and ring updates arrive as WS call frames
// on dev.{deviceId}.call — surfaced here via onOffer/onRing/onRemoteEnd once the
// gateway forwards that subject and the WsClient decodes call frames (the same
// wire-codec seam the rest of the client rides). Media is a documented LiveKit
// seam (T2.05); the call state machine + E2EE frame engine are fully wired.

import {
  buildSimulcastEncodings,
  CallSession,
  CameraController,
  createDevRootSecretProvider,
  EffectController,
  IDLE,
  ScreenShareController,
  type CallKind,
  type CallState,
  type CameraState,
  type EffectState,
  type RingSignal,
  type ScreenShareState,
} from "@wa/call-engine";
import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { config } from "../config";
import { useServices } from "../ui/ServicesContext";
import { createCallControl } from "./callControl";
import { createWebCamera } from "./webCamera";
import { createWebScreenShare } from "./webScreenShare";
import { createWebVideoProcessor, type SegmentationBlur } from "./webVideoEffect";
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
  publishScreen: () => {},
  leave: () => Promise.resolve(),
};

// The person-segmentation model (MediaPipe/WebGL) is the on-device blur seam;
// until the model bundle is wired it passes the camera track through unchanged.
// Swap for the real segmentation pipeline at app wiring. The frames never leave
// the device either way (E2EE).
const stubBlur: SegmentationBlur = {
  start: (src) => src,
  stop: () => {},
};

export interface CallApi {
  state: CallState;
  camera: CameraState;
  screen: ScreenShareState;
  effect: EffectState;
  /** The local camera track, for a self-preview element. */
  localVideo(): MediaStreamTrack | null;
  /** The local screen-share track (null when not sharing). */
  screenVideo(): MediaStreamTrack | null;
  startCall(peerId: string, kind?: CallKind): Promise<void>;
  accept(): Promise<void>;
  decline(): Promise<void>;
  hangup(): Promise<void>;
  toggleCamera(): Promise<void>;
  flipCamera(): Promise<void>;
  toggleScreenShare(): Promise<void>;
  toggleBlur(): Promise<void>;
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
  const [screen, setScreen] = useState<ScreenShareState>({ sharing: false });
  const [effect, setEffect] = useState<EffectState>({ effect: "none" });

  const { session, controller, webCam, screenCtrl, effectCtrl, webScreen } = useMemo(() => {
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
    const screenShare = createWebScreenShare((track, encodings) => stubRtc.publishScreen?.(track, encodings));
    const sctrl = new ScreenShareController(screenShare.source, setScreen);
    // Blur swaps the published camera track for the on-device blurred one.
    const processor = createWebVideoProcessor(
      () => cam.track(),
      (track) => stubRtc.publishVideo?.(track, buildSimulcastEncodings()),
      stubBlur,
    );
    const ectrl = new EffectController(processor, setEffect);
    return { session: s, controller: ctrl, webCam: cam, screenCtrl: sctrl, effectCtrl: ectrl, webScreen: screenShare };
  }, [services]);

  // Reset the surfaced state whenever the session is rebuilt; release the camera
  // and stop sharing when a call ends.
  useEffect(() => setState(session.getState()), [session]);
  useEffect(() => {
    if (state.phase === "ended" || state.phase === "idle") {
      void controller.disable();
      void screenCtrl.stop();
      void effectCtrl.setEffect("none");
    }
  }, [state.phase, controller, screenCtrl, effectCtrl]);

  const api: CallApi = {
    state,
    camera,
    screen,
    effect,
    localVideo: () => webCam.track(),
    screenVideo: () => webScreen.track(),
    startCall: (peerId, kind = "voice") => session.startCall(peerId, kind),
    accept: () => session.accept(),
    decline: () => session.decline(),
    hangup: () => session.hangup(),
    toggleCamera: () => controller.toggle(),
    flipCamera: () => controller.flip(),
    toggleScreenShare: () => screenCtrl.toggle(),
    toggleBlur: () => effectCtrl.toggleBlur(),
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

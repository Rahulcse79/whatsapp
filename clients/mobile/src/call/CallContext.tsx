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
  EffectController,
  IDLE,
  RingBridge,
  ScreenShareController,
  type CallActions,
  type CallKind,
  type CallState,
  type CameraState,
  type EffectState,
  type IncomingCall,
  type NativeRinger,
  type RingSignal,
  type ScreenShareState,
} from "@wa/call-engine";
import { mediaDevices } from "@livekit/react-native-webrtc";
import { Track } from "livekit-client";
import { createContext, useContext, useEffect, useMemo, useState, type ReactNode } from "react";
import { useServices } from "../ui/ServicesContext";
import { createCallControl } from "./callControl";
import { RnCallMedia, createLiveKitRnRtc } from "./rnCallMedia";
import { createRnCamera, type RnMediaDevices } from "./rnCamera";
import { createRnScreenShare, type RnScreenCapturer } from "./rnScreenShare";
import { createRnVideoProcessor, type RnBackgroundBlur } from "./rnVideoEffect";

// react-native-webrtc's `mediaDevices` (from @livekit/react-native-webrtc) is
// the real capture surface — `getUserMedia` opens the camera and the resulting
// stream's toURL() feeds the <RTCView> self-preview. It's cast to the structural
// RnMediaDevices seam the camera adapter declares (the runtime shapes match).
const rnMediaDevices = mediaDevices as unknown as RnMediaDevices;

// ReplayKit / MediaProjection capture + native background blur seams — stubbed
// until the native modules are wired; the share/effect state machines run either
// way. Both process frames on-device (E2EE).
const stubScreenCapturer: RnScreenCapturer = {
  getDisplayMedia: () => Promise.resolve({ getVideoTracks: () => [], release: () => {} }),
};
const stubBlur: RnBackgroundBlur = { enable: () => {}, disable: () => {} };

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
  screen: ScreenShareState;
  effect: EffectState;
  /** Current self-preview stream URL for `<RTCView>`, or null when the camera is off. */
  localStreamURL(): string | null;
  /** The first remote participant's camera stream URL for `<RTCView>`, or null. */
  remoteStreamURL(): string | null;
  startCall(peerId: string, kind?: CallKind): Promise<void>;
  accept(): Promise<void>;
  decline(): Promise<void>;
  hangup(): Promise<void>;
  toggleCamera(): Promise<void>;
  flipCamera(): Promise<void>;
  toggleScreenShare(): Promise<void>;
  toggleBlur(): Promise<void>;
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
  const [screen, setScreen] = useState<ScreenShareState>({ sharing: false });
  const [effect, setEffect] = useState<EffectState>({ effect: "none" });

  const { session, bridge, controller, rnCam, screenCtrl, effectCtrl, rtc } = useMemo(() => {
    const token = (): string => services.sessions.current()?.accessJwt ?? "";
    const selfId = services.sessions.current()?.deviceId ?? "self";
    // One LiveKit session backs the media transport AND the camera/screen publish
    // callbacks (camera capture is the next RN seam, so publish is a no-op today).
    const rtc = createLiveKitRnRtc(services.config.livekitUrl);
    let ring: RingBridge | undefined;
    const s = new CallSession(
      createCallControl(services.config.apiBaseUrl, token),
      new RnCallMedia(rtc),
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
    const cam = createRnCamera(rnMediaDevices, (track, encodings) => rtc.publishVideo?.(track, encodings));
    const ctrl = new CameraController(cam.device, setCamera);
    const screenSource = createRnScreenShare(stubScreenCapturer, (track, encodings) => rtc.publishScreen?.(track, encodings));
    const sctrl = new ScreenShareController(screenSource, setScreen);
    const ectrl = new EffectController(createRnVideoProcessor(stubBlur), setEffect);
    return { session: s, bridge: ring, controller: ctrl, rnCam: cam, screenCtrl: sctrl, effectCtrl: ectrl, rtc };
  }, [services]);

  useEffect(() => setState(session.getState()), [session]);
  // Route incoming WS call frames (dev.{id}.call) into the session: offers show
  // the ring, ring updates connect media when answered, ends tear down.
  useEffect(() => {
    services.onCallSignal({
      onOffer: (callerUserId, roomId, ringId, kind) => session.onOffer(callerUserId, roomId, ringId, kind),
      onRing: (state) => void session.onRing(state),
      onEnd: (reason) => void session.onRemoteEnd(reason),
    });
    return () => services.onCallSignal(null);
  }, [services, session]);
  // Release the camera + stop sharing when a call ends or the screen goes idle.
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
    localStreamURL: () => rnCam.streamURL(),
    remoteStreamURL: () => {
      const room = rtc.getRoom?.();
      if (!room) return null;
      for (const p of room.remoteParticipants.values()) {
        // livekit's Track.mediaStream is typed as the DOM MediaStream; on RN it's
        // a react-native-webrtc MediaStream with toURL() for <RTCView>.
        const stream = p.getTrackPublication(Track.Source.Camera)?.videoTrack?.mediaStream as
          | { toURL(): string }
          | undefined;
        const url = stream?.toURL();
        if (url) return url;
      }
      return null;
    },
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
    incoming: (call) => bridge.incoming(call),
  };

  return <CallContext.Provider value={api}>{children}</CallContext.Provider>;
}

export function useCall(): CallApi {
  const ctx = useContext(CallContext);
  if (!ctx) throw new Error("useCall must be used inside <CallProvider>");
  return ctx;
}

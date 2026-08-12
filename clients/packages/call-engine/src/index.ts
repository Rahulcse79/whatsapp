// @wa/call-engine — the client call engine (rtc-lld §2 + sequence-diagrams §6):
// SFrame-style E2EE media-frame encryption over WebCrypto, per-epoch frame keys
// derived from the Signal session, and the 1:1 call state machine. Framework-free;
// LiveKit/WebRTC, control-plane REST, and the Signal secret are injected ports.

export { FrameCryptor } from "./frameCrypto";
export { deriveFrameKey, type FrameKeyContext } from "./keyDerivation";
export { CallCrypto, type CallPeers } from "./callCrypto";
export {
  active,
  IDLE,
  reduce,
  type CallDirection,
  type CallEvent,
  type CallKind,
  type CallPhase,
  type CallState,
  type RingSignal,
} from "./callState";
export {
  CallSession,
  type CallControl,
  type CreateResult,
  type MediaConnectOptions,
  type MediaTransport,
  type RootSecretProvider,
} from "./callSession";
export { createDevGroupRootSecret, createDevRootSecretProvider } from "./devRootSecret";
export { GroupCallCrypto, type GroupCallContext } from "./groupCallCrypto";
export { ActiveSpeakerTracker, type ActiveSpeakerOptions, type AudioLevel } from "./activeSpeaker";
export { computeLayout, desiredReceiveLayers, type GroupLayout, type Tile } from "./layout";
export {
  RingBridge,
  type CallActions,
  type IncomingCall,
  type NativeRinger,
} from "./ringBridge";
export {
  AUDIO_ONLY_FLOOR_KBPS,
  buildSimulcastEncodings,
  chooseReceiveLayer,
  SIMULCAST_LADDER,
  type LayerId,
  type ReceiveContext,
  type RtpEncoding,
  type SimulcastLayer,
} from "./simulcast";
export {
  CameraController,
  type CameraDevice,
  type CameraFacing,
  type CameraState,
} from "./camera";
export {
  EffectController,
  type BackgroundEffect,
  type EffectState,
  type VideoProcessor,
} from "./videoEffects";
export {
  buildScreenShareEncodings,
  ScreenShareController,
  SCREEN_CONTENT_HINT,
  type ScreenEncoding,
  type ScreenShareState,
  type ScreenSource,
} from "./screenShare";

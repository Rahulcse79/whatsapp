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
export { createDevRootSecretProvider } from "./devRootSecret";
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

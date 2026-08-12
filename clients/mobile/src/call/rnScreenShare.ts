// React Native screen share — a ScreenSource over ReplayKit (iOS broadcast
// extension) / MediaProjection (Android), surfaced through react-native-webrtc's
// getDisplayMedia. The captured track is published separately, content-optimized
// (rtc-lld §3). Native capture needs a config plugin / broadcast extension, so
// the surface is declared structurally and injected at app wiring (mirrors
// rnCamera). Frames are E2EE-sealed like every media path.

import { buildScreenShareEncodings, type ScreenEncoding, type ScreenSource } from "@wa/call-engine";
import type { RnVideoTrack } from "./rnCamera";

export interface RnScreenStream {
  getVideoTracks(): RnVideoTrack[];
  release?(): void;
}

/** The display-capture surface (react-native-webrtc mediaDevices.getDisplayMedia
 *  / a broadcast module). onEnded lets the native side report an OS-initiated
 *  stop (the iOS/Android system "stop sharing" affordance). */
export interface RnScreenCapturer {
  getDisplayMedia(): Promise<RnScreenStream>;
  onEnded?(cb: () => void): void;
}

export type PublishScreen = (track: RnVideoTrack | null, encodings: ScreenEncoding[]) => void;

export function createRnScreenShare(capturer: RnScreenCapturer, publish: PublishScreen): ScreenSource {
  let stream: RnScreenStream | null = null;

  return {
    async start() {
      const s = await capturer.getDisplayMedia();
      stream = s;
      publish(s.getVideoTracks()[0] ?? null, buildScreenShareEncodings());
    },
    stop() {
      stream?.getVideoTracks().forEach((t) => t.stop());
      stream?.release?.();
      stream = null;
      publish(null, []);
      return Promise.resolve();
    },
    onEnded(cb) {
      capturer.onEnded?.(cb); // the native module fires this on OS-initiated stop
    },
  };
}

// Web screen share — a ScreenSource over getDisplayMedia. It captures the screen
// as a SEPARATE, content-optimized track (rtc-lld §3: detail hint, low fps) and
// publishes it alongside the camera. Like all media it is E2EE-sealed; the SFU
// forwards a stream it cannot read.

import { buildScreenShareEncodings, SCREEN_CONTENT_HINT, type ScreenEncoding, type ScreenSource } from "@wa/call-engine";

/** Publish seam: hand the SFU the screen track + its encodings, or null to
 *  unpublish. LiveKit's Room.localParticipant satisfies this. */
export type PublishScreen = (track: MediaStreamTrack | null, encodings: ScreenEncoding[]) => void;

export interface WebScreenShare {
  source: ScreenSource;
  /** The live screen track, for a local preview (null when not sharing). */
  track(): MediaStreamTrack | null;
}

export function createWebScreenShare(publish: PublishScreen): WebScreenShare {
  let current: MediaStreamTrack | null = null;
  let onEndedCb: (() => void) | null = null;

  return {
    track: () => current,
    source: {
      async start() {
        const stream = await navigator.mediaDevices.getDisplayMedia({ video: { frameRate: 5 }, audio: false });
        const track = stream.getVideoTracks()[0];
        if (!track) throw new Error("no screen track");
        // "detail" biases the encoder toward spatial quality — shared text stays
        // legible at low framerate (rtc-lld §3).
        (track as MediaStreamTrack & { contentHint: string }).contentHint = SCREEN_CONTENT_HINT;
        // The browser's own "Stop sharing" bar ends the track directly.
        track.addEventListener("ended", () => onEndedCb?.());
        current = track;
        publish(track, buildScreenShareEncodings());
      },
      stop() {
        current?.stop();
        current = null;
        publish(null, []);
        return Promise.resolve();
      },
      onEnded(cb) {
        onEndedCb = cb;
      },
    },
  };
}

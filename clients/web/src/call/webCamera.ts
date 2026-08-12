// Web camera capture — a CameraDevice over getUserMedia. It opens the local
// video track at a facing, republishes it to the SFU with the simulcast ladder
// (rtc-lld §3), and exposes the current track for a local preview. Full media
// join is the LiveKit seam (RtcSession.publishVideo); the capture + simulcast
// params are real.

import { buildSimulcastEncodings, type CameraDevice, type CameraFacing, type RtpEncoding } from "@wa/call-engine";

/** The publish seam: hand the SFU a track + its simulcast encodings, or null to
 *  unpublish. LiveKit's Room satisfies this. */
export type PublishVideo = (track: MediaStreamTrack | null, encodings: RtpEncoding[]) => void;

export interface WebCamera {
  device: CameraDevice;
  /** The live local track, for a preview element (null when off). */
  track(): MediaStreamTrack | null;
}

export function createWebCamera(publish: PublishVideo, uplinkKbps?: number): WebCamera {
  let current: MediaStreamTrack | null = null;

  async function open(facing: CameraFacing): Promise<MediaStreamTrack> {
    const stream = await navigator.mediaDevices.getUserMedia({
      video: { facingMode: facing === "front" ? "user" : "environment", width: { ideal: 1280 }, height: { ideal: 720 } },
      audio: false,
    });
    const track = stream.getVideoTracks()[0];
    if (!track) throw new Error("no camera track");
    return track;
  }

  function close(): void {
    current?.stop();
    current = null;
  }

  return {
    track: () => current,
    device: {
      async start(facing) {
        close();
        current = await open(facing);
        publish(current, buildSimulcastEncodings(uplinkKbps));
      },
      async applyFacing(facing) {
        // getUserMedia can't re-point a live track; open a fresh one and swap.
        const next = await open(facing);
        close();
        current = next;
        publish(current, buildSimulcastEncodings(uplinkKbps));
      },
      stop() {
        close();
        publish(null, []);
        return Promise.resolve();
      },
    },
  };
}

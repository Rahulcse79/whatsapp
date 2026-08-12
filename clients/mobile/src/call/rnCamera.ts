// React Native camera capture — a CameraDevice (T2.05) over react-native-webrtc.
// It opens the local video track at a facing, publishes it to the SFU with the
// simulcast ladder (rtc-lld §3), flips front/back in place, and exposes the
// stream URL for an <RTCView> self-preview. react-native-webrtc is a native dep
// with a config plugin, so it is NOT imported here — its `mediaDevices` surface
// is declared structurally and injected at app wiring time (mirrors rnCallMedia).

import { buildSimulcastEncodings, type CameraDevice, type CameraFacing, type RtpEncoding } from "@wa/call-engine";

/** A react-native-webrtc MediaStreamTrack subset. `_switchCamera` flips a live
 *  track in place (no re-publish needed). */
export interface RnVideoTrack {
  _switchCamera?(): void;
  stop(): void;
}

/** A react-native-webrtc MediaStream subset: a URL for <RTCView> + its tracks. */
export interface RnMediaStream {
  toURL(): string;
  getVideoTracks(): RnVideoTrack[];
  release?(): void;
}

/** The react-native-webrtc `mediaDevices` surface this adapter uses. */
export interface RnMediaDevices {
  getUserMedia(constraints: { video: { facingMode: string }; audio: boolean }): Promise<RnMediaStream>;
}

/** Publish seam: hand the SFU the video track + simulcast encodings (null =
 *  unpublish). react-native-webrtc's RTCPeerConnection.addTrack + setParameters
 *  satisfies this once the dep is wired. */
export type PublishVideo = (track: RnVideoTrack | null, encodings: RtpEncoding[]) => void;

export interface RnCamera {
  device: CameraDevice;
  /** Current stream URL for `<RTCView streamURL={...} />`, or null when off. */
  streamURL(): string | null;
}

export function createRnCamera(mediaDevices: RnMediaDevices, publish: PublishVideo, uplinkKbps?: number): RnCamera {
  let stream: RnMediaStream | null = null;
  let url: string | null = null;

  async function open(facing: CameraFacing): Promise<void> {
    const s = await mediaDevices.getUserMedia({
      video: { facingMode: facing === "front" ? "user" : "environment" },
      audio: false,
    });
    stream = s;
    url = s.toURL();
    publish(s.getVideoTracks()[0] ?? null, buildSimulcastEncodings(uplinkKbps));
  }

  function close(): void {
    stream?.getVideoTracks().forEach((t) => t.stop());
    stream?.release?.();
    stream = null;
    url = null;
  }

  return {
    streamURL: () => url,
    device: {
      async start(facing) {
        close();
        await open(facing);
      },
      async applyFacing(facing) {
        // A live react-native-webrtc track flips in place — no re-publish.
        const track = stream?.getVideoTracks()[0];
        if (track?._switchCamera) {
          track._switchCamera();
          return;
        }
        await open(facing); // fallback: reopen at the new facing
      },
      stop() {
        close();
        publish(null, []);
        return Promise.resolve();
      },
    },
  };
}

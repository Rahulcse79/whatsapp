// RnCallMedia joins the LiveKit room on React Native via react-native-webrtc
// (injected here as a minimal structural RnRtcSession so this compiles without
// the native dep). It sets up the per-call CallCrypto for parity with web.
//
// KNOWN GAP (documented seam): react-native-webrtc does NOT expose insertable
// streams / encoded-transform today, so the SFrame per-frame E2EE the web client
// applies cannot be installed at the RN layer yet. Until a native SFrame module
// lands, RN calls rely on DTLS-SRTP (hop-by-hop) only — the media plane, not the
// frame layer, is encrypted. The control plane, ring state machine, and key
// derivation are identical to web.

import type { CallCrypto, MediaConnectOptions, MediaTransport, RtpEncoding, ScreenEncoding } from "@wa/call-engine";
import { AudioSession } from "@livekit/react-native";
import { Room, Track } from "livekit-client";
import type { RnVideoTrack } from "./rnCamera";

export interface RnRtcSession {
  join(roomId: string, joinToken: string): Promise<void>;
  leave(): Promise<void>;
  /** Present once react-native-webrtc gains encoded-frame transforms. */
  installFrameE2EE?(crypto: CallCrypto): void;
  /** Publish (or, with null, unpublish) the local camera track with the given
   *  simulcast encodings — the video seam (T2.05). */
  publishVideo?(track: RnVideoTrack | null, encodings: RtpEncoding[]): void;
  /** Publish (or, with null, unpublish) a screen-share track — separate,
   *  content-optimized (T2.06). */
  publishScreen?(track: RnVideoTrack | null, encodings: ScreenEncoding[]): void;
  /** The live LiveKit room, for the UI to read local/remote video tracks. */
  getRoom?(): Room | null;
}

export class RnCallMedia implements MediaTransport {
  constructor(private readonly rtc: RnRtcSession) {}

  async connect({ roomId, joinToken, crypto }: MediaConnectOptions): Promise<void> {
    await this.rtc.join(roomId, joinToken);
    // No-op until RN supports encoded transforms; kept so the seam is one call.
    this.rtc.installFrameE2EE?.(crypto);
  }

  disconnect(): Promise<void> {
    return this.rtc.leave();
  }
}

// createLiveKitRnRtc is the real @livekit/react-native transport (replaces the
// stub). It starts the native AudioSession, joins the SFU room with the minted
// token, and publishes the mic — remote audio plays through the audio session
// automatically, so a voice call is audible. Camera/screen tracks the RN
// controllers capture are (re)published via publishVideo/publishScreen (mirrors
// web's LiveKitRtc); the call UI reads getRoom() to render local + remote video.
// registerGlobals() must have run at app startup (app/_layout.tsx) first.
export function createLiveKitRnRtc(serverUrl: string): RnRtcSession {
  let room: Room | null = null;

  // Replace the current publication for a source: unpublish the old track, then
  // publish the new one (null just unpublishes). The RN webrtc track is a real
  // MediaStreamTrack at runtime — the structural RnVideoTrack is the seam type —
  // so it's cast for LiveKit's publish API.
  async function republish(track: RnVideoTrack | null, source: Track.Source, simulcast: boolean): Promise<void> {
    if (!room) return;
    const existing = room.localParticipant.getTrackPublication(source);
    if (existing?.track) await room.localParticipant.unpublishTrack(existing.track);
    if (track) {
      await room.localParticipant.publishTrack(track as unknown as MediaStreamTrack, { source, simulcast });
    }
  }

  return {
    async join(_roomId, joinToken) {
      await AudioSession.startAudioSession();
      const r = new Room({ adaptiveStream: true, dynacast: true });
      room = r;
      await r.connect(serverUrl, joinToken);
      await r.localParticipant.setMicrophoneEnabled(true);
    },
    async leave() {
      await room?.disconnect();
      room = null;
      await AudioSession.stopAudioSession();
    },
    publishVideo(track) {
      void republish(track, Track.Source.Camera, true);
    },
    publishScreen(track) {
      void republish(track, Track.Source.ScreenShare, false);
    },
    getRoom() {
      return room;
    },
  };
}

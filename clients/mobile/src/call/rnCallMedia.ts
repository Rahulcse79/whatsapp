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

import type { CallCrypto, MediaConnectOptions, MediaTransport, RtpEncoding } from "@wa/call-engine";
import type { RnVideoTrack } from "./rnCamera";

export interface RnRtcSession {
  join(roomId: string, joinToken: string): Promise<void>;
  leave(): Promise<void>;
  /** Present once react-native-webrtc gains encoded-frame transforms. */
  installFrameE2EE?(crypto: CallCrypto): void;
  /** Publish (or, with null, unpublish) the local camera track with the given
   *  simulcast encodings — the video seam (T2.05). */
  publishVideo?(track: RnVideoTrack | null, encodings: RtpEncoding[]): void;
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

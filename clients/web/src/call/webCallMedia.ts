// WebCallMedia joins the LiveKit room and installs the SFrame E2EE transforms on
// every track. LiveKit (the SFU) is reached through livekit-client, typed here as
// a minimal structural RtcSession so this file compiles without pulling the SDK
// into the build — the app injects a real LiveKit-backed session. Full media
// (device capture, simulcast) lands in T2.05; the E2EE wiring is real now.

import type { CallCrypto, MediaConnectOptions, MediaTransport } from "@wa/call-engine";
import { installReceiverE2EE, installSenderE2EE } from "./frameTransform";

export interface RtcSession {
  join(roomId: string, joinToken: string): Promise<void>;
  senders(): RTCRtpSender[];
  receivers(): RTCRtpReceiver[];
  /** Fires for tracks subscribed after join (the peer's audio). */
  onTrackSubscribed(cb: (receiver: RTCRtpReceiver) => void): void;
  leave(): Promise<void>;
}

export class WebCallMedia implements MediaTransport {
  constructor(private readonly rtc: RtcSession) {}

  async connect({ roomId, joinToken, crypto }: MediaConnectOptions): Promise<void> {
    await this.rtc.join(roomId, joinToken);
    this.installAll(crypto);
    this.rtc.onTrackSubscribed((r) => installReceiverE2EE(r, crypto));
  }

  disconnect(): Promise<void> {
    return this.rtc.leave();
  }

  private installAll(crypto: CallCrypto): void {
    for (const s of this.rtc.senders()) installSenderE2EE(s, crypto);
    for (const r of this.rtc.receivers()) installReceiverE2EE(r, crypto);
  }
}

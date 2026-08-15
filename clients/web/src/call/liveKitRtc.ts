// Real LiveKit-backed RtcSession (replaces the dev stub in CallContext). It
// joins the SFU room with the server-minted join token, publishes the mic on
// connect, plays remote tracks, and republishes camera/screen tracks the
// controllers capture. senders()/receivers() feed the SFrame E2EE transforms
// (frameTransform.ts) — a no-op unless the browser exposes insertable streams,
// so plain media still flows in dev.

import type { RtpEncoding, ScreenEncoding } from "@wa/call-engine";
import { Room, RoomEvent, Track, type LocalTrack, type RemoteTrack } from "livekit-client";
import type { RtcSession } from "./webCallMedia";

export function createLiveKitRtc(serverUrl: string): RtcSession {
  return new LiveKitRtc(serverUrl);
}

class LiveKitRtc implements RtcSession {
  private room: Room | null = null;
  private trackCb: ((r: RTCRtpReceiver) => void) | null = null;
  private readonly pendingReceivers: RTCRtpReceiver[] = [];
  private sink: HTMLDivElement | null = null;

  constructor(private readonly serverUrl: string) {}

  async join(_roomId: string, joinToken: string): Promise<void> {
    const room = new Room({ adaptiveStream: true, dynacast: true });
    this.room = room;
    room.on(RoomEvent.TrackSubscribed, (track) => this.onSubscribed(track));
    room.on(RoomEvent.Disconnected, () => this.cleanup());
    await room.connect(this.serverUrl, joinToken);
    // Voice is the baseline: publishing the mic makes the call audible even
    // before the camera is turned on. Camera/screen ride publishVideo/Screen.
    await room.localParticipant.setMicrophoneEnabled(true);
  }

  private onSubscribed(track: RemoteTrack): void {
    // Attach for local playback: audio autoplays (hidden), remote video shows as
    // a floating tile so a video call is visible without extra UI wiring.
    const el = track.attach();
    if (track.kind === Track.Kind.Video) {
      el.className = "call-remote-video";
    } else {
      el.style.display = "none";
    }
    this.mediaSink().appendChild(el);
    const receiver = track.receiver;
    if (!receiver) return;
    if (this.trackCb) this.trackCb(receiver);
    else this.pendingReceivers.push(receiver);
  }

  senders(): RTCRtpSender[] {
    const out: RTCRtpSender[] = [];
    this.room?.localParticipant.trackPublications.forEach((pub) => {
      const s = (pub.track as LocalTrack | undefined)?.sender;
      if (s) out.push(s);
    });
    return out;
  }

  receivers(): RTCRtpReceiver[] {
    const out: RTCRtpReceiver[] = [];
    this.room?.remoteParticipants.forEach((p) => {
      p.trackPublications.forEach((pub) => {
        const r = (pub.track as RemoteTrack | undefined)?.receiver;
        if (r) out.push(r);
      });
    });
    return out;
  }

  onTrackSubscribed(cb: (receiver: RTCRtpReceiver) => void): void {
    this.trackCb = cb;
    for (const r of this.pendingReceivers.splice(0)) cb(r);
  }

  publishVideo(track: MediaStreamTrack | null, _encodings: RtpEncoding[]): void {
    void this.republish(track, Track.Source.Camera);
  }

  publishScreen(track: MediaStreamTrack | null, _encodings: ScreenEncoding[]): void {
    void this.republish(track, Track.Source.ScreenShare);
  }

  private async republish(track: MediaStreamTrack | null, source: Track.Source): Promise<void> {
    const room = this.room;
    if (!room) return;
    const existing = room.localParticipant.getTrackPublication(source);
    if (existing?.track) await room.localParticipant.unpublishTrack(existing.track);
    if (track) await room.localParticipant.publishTrack(track, { source, simulcast: source === Track.Source.Camera });
  }

  async leave(): Promise<void> {
    await this.room?.disconnect();
    this.cleanup();
  }

  private mediaSink(): HTMLDivElement {
    if (!this.sink) {
      this.sink = document.createElement("div");
      this.sink.className = "call-media-sink";
      document.body.appendChild(this.sink);
    }
    return this.sink;
  }

  private cleanup(): void {
    this.sink?.remove();
    this.sink = null;
    this.pendingReceivers.length = 0;
    this.trackCb = null;
    this.room = null;
  }
}

// Insertable-streams E2EE (rtc-lld §2). Each media frame is piped through the
// call's SFrame cipher: outbound frames are sealed after the encoder, inbound
// frames opened before the decoder — so LiveKit's SFU only ever forwards
// ciphertext. Chromium exposes the encoded stream via
// RTCRtpSender/Receiver.createEncodedStreams(); the worker-based
// RTCRtpScriptTransform is the eventual target. Browsers without the API get
// unencrypted media (feature-detected by the caller) rather than a broken call.

import type { CallCrypto } from "@wa/call-engine";

// Minimal shape of an encoded media frame + the non-standard createEncodedStreams
// hook (not yet in lib.dom).
interface EncodedFrame {
  data: ArrayBuffer;
}
interface EncodedStreams {
  readable: ReadableStream<EncodedFrame>;
  writable: WritableStream<EncodedFrame>;
}
type WithEncodedStreams = { createEncodedStreams?: () => EncodedStreams };

/** Per-direction frame counters, so a call that carries no media can be told
 *  apart from a silent peer. Dropping every inbound frame is exactly what a key
 *  mismatch looks like, and it used to be completely invisible: the call showed
 *  as connected and simply had no sound. */
export interface FrameStats {
  ok: number;
  dropped: number;
}
const stats: Record<"send" | "recv", FrameStats> = { send: { ok: 0, dropped: 0 }, recv: { ok: 0, dropped: 0 } };

/** frameStats reports the running seal/open tallies (diagnostics + tests). */
export function frameStats(): { send: FrameStats; recv: FrameStats } {
  return { send: { ...stats.send }, recv: { ...stats.recv } };
}

/** resetFrameStats clears the tallies at the start of a call. */
export function resetFrameStats(): void {
  stats.send = { ok: 0, dropped: 0 };
  stats.recv = { ok: 0, dropped: 0 };
}

function pipe(
  streams: EncodedStreams,
  transform: (data: Uint8Array) => Promise<Uint8Array>,
  dir: "send" | "recv",
): void {
  let warned = false;
  void streams.readable
    .pipeThrough(
      new TransformStream<EncodedFrame, EncodedFrame>({
        async transform(frame, controller) {
          try {
            const out = await transform(new Uint8Array(frame.data));
            frame.data = out.buffer.slice(out.byteOffset, out.byteOffset + out.byteLength);
            stats[dir].ok++;
            controller.enqueue(frame);
          } catch (err) {
            // A frame that won't seal/open (e.g. a key not yet rotated in) is
            // dropped, never forwarded in the clear. That is the right call
            // security-wise, but it must not be silent: a sustained drop streak
            // means the peers disagree on a key, not that nobody is talking.
            stats[dir].dropped++;
            if (!warned && stats[dir].dropped >= 30 && stats[dir].ok === 0) {
              warned = true;
              console.error(
                `[call] every ${dir} frame failed to ${dir === "send" ? "seal" : "open"} ` +
                  `(${stats[dir].dropped} dropped, 0 succeeded). The peers have derived ` +
                  `different keys — check that both ends use the same identity space.`,
                err,
              );
            }
          }
        },
      }),
    )
    .pipeTo(streams.writable);
}

/** installSenderE2EE seals every outbound frame on a sender. No-op (returns
 *  false) when the browser lacks insertable streams. */
export function installSenderE2EE(sender: RTCRtpSender, crypto: CallCrypto): boolean {
  const streams = (sender as RTCRtpSender & WithEncodedStreams).createEncodedStreams?.();
  if (!streams) return false;
  pipe(streams, (data) => crypto.seal(data), "send");
  return true;
}

/** installReceiverE2EE opens every inbound frame on a receiver. */
export function installReceiverE2EE(receiver: RTCRtpReceiver, crypto: CallCrypto): boolean {
  const streams = (receiver as RTCRtpReceiver & WithEncodedStreams).createEncodedStreams?.();
  if (!streams) return false;
  pipe(streams, (data) => crypto.open(data), "recv");
  return true;
}

/** e2eeSupported feature-detects insertable streams up front. */
export function e2eeSupported(): boolean {
  return typeof RTCRtpSender !== "undefined" && "createEncodedStreams" in RTCRtpSender.prototype;
}

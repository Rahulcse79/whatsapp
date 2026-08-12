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

function pipe(streams: EncodedStreams, transform: (data: Uint8Array) => Promise<Uint8Array>): void {
  void streams.readable
    .pipeThrough(
      new TransformStream<EncodedFrame, EncodedFrame>({
        async transform(frame, controller) {
          try {
            const out = await transform(new Uint8Array(frame.data));
            frame.data = out.buffer.slice(out.byteOffset, out.byteOffset + out.byteLength);
            controller.enqueue(frame);
          } catch {
            // A frame that won't seal/open (e.g. key not yet rotated in) is
            // dropped, not forwarded in the clear.
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
  pipe(streams, (data) => crypto.seal(data));
  return true;
}

/** installReceiverE2EE opens every inbound frame on a receiver. */
export function installReceiverE2EE(receiver: RTCRtpReceiver, crypto: CallCrypto): boolean {
  const streams = (receiver as RTCRtpReceiver & WithEncodedStreams).createEncodedStreams?.();
  if (!streams) return false;
  pipe(streams, (data) => crypto.open(data));
  return true;
}

/** e2eeSupported feature-detects insertable streams up front. */
export function e2eeSupported(): boolean {
  return typeof RTCRtpSender !== "undefined" && "createEncodedStreams" in RTCRtpSender.prototype;
}

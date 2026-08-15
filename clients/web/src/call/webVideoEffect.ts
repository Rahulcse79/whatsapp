// Web background blur — a VideoProcessor that swaps the published camera track
// for an on-device blurred one. The blur runs ENTIRELY on the sender's device
// (E2EE: the server only ever receives encrypted frames, so there is nothing for
// it to blur). The heavy person-segmentation model is an injected seam.

import type { BackgroundEffect, VideoProcessor } from "@wa/call-engine";

/** SegmentationBlur wraps a raw camera track and returns a NEW track whose
 *  background is blurred, running selfie segmentation on-device (MediaPipe over
 *  WebGL / WASM + MediaStreamTrackProcessor). Injected so the model bundle is
 *  loaded only when blur is enabled, and so no frame leaves the device. */
export interface SegmentationBlur {
  /** Begin processing `source`; returns the effected output track. With
   *  opts.backgroundImage the background is REPLACED by that image (virtual
   *  background, T9.01); otherwise it is blurred. */
  start(source: MediaStreamTrack, opts?: { backgroundImage?: string }): MediaStreamTrack;
  /** Tear the pipeline down and free the model. */
  stop(): void;
}

/**
 * createWebVideoProcessor bridges the EffectController to a SegmentationBlur:
 * enabling blur swaps the published camera track for the processed one; clearing
 * restores the raw track. `sourceTrack` reads the current camera track and `swap`
 * republishes a track to the SFU (resolution is unchanged, so the camera's
 * simulcast encodings still apply and no re-negotiation of layers is needed).
 */
export function createWebVideoProcessor(
  sourceTrack: () => MediaStreamTrack | null,
  swap: (track: MediaStreamTrack) => void,
  blur: SegmentationBlur,
): VideoProcessor {
  let processed: MediaStreamTrack | null = null;

  return {
    apply(effect: BackgroundEffect, opts?: { backgroundImage?: string }) {
      const src = sourceTrack();
      if (!src || effect === "none") return Promise.resolve();
      // blur + background share the segmentation pipeline; background passes the
      // replacement image through to the processor.
      processed = blur.start(src, effect === "background" ? { backgroundImage: opts?.backgroundImage } : undefined);
      swap(processed);
      return Promise.resolve();
    },
    clear() {
      if (processed) {
        blur.stop();
        processed = null;
        const src = sourceTrack();
        if (src) swap(src);
      }
      return Promise.resolve();
    },
  };
}

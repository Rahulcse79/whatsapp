// React Native background blur — a VideoProcessor over a native frame processor
// (react-native-webrtc frame transform / Vision-Camera segmentation plugin). The
// blur is applied in-place on the local capture pipeline, entirely on-device:
// under E2EE the server only ever receives encrypted frames, so there is nothing
// for it to blur. The native processor is injected structurally (mirrors the
// other RN media seams).

import type { BackgroundEffect, VideoProcessor } from "@wa/call-engine";

/** RnBackgroundBlur toggles on-device background blur on the local capture. */
export interface RnBackgroundBlur {
  enable(): void;
  disable(): void;
}

export function createRnVideoProcessor(blur: RnBackgroundBlur): VideoProcessor {
  return {
    apply(effect: BackgroundEffect) {
      if (effect === "blur") blur.enable();
      return Promise.resolve();
    },
    clear() {
      blur.disable();
      return Promise.resolve();
    },
  };
}

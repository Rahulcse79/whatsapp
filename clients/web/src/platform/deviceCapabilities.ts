import type { DeviceCapabilities } from "@wa/client-core";

// Web DeviceCapabilities: vibration, a short notification tone, and the
// clipboard. All three are best-effort — a browser may not implement them, or
// may block them until the user has interacted with the page — so each degrades
// to a no-op rather than throwing. A missing buzz must never fail a send.

export const webDeviceCapabilities: DeviceCapabilities = {
  vibrate(ms: number): void {
    try {
      if (typeof navigator !== "undefined" && "vibrate" in navigator) navigator.vibrate(ms);
    } catch {
      /* unsupported or blocked */
    }
  },

  playNotificationSound(): void {
    try {
      // A short synthesised blip rather than a bundled asset: no network, no
      // decoding, and nothing to ship. Browsers block audio until the user has
      // interacted with the page, which the catch covers.
      const Ctor =
        (window as unknown as { AudioContext?: typeof AudioContext }).AudioContext ??
        (window as unknown as { webkitAudioContext?: typeof AudioContext }).webkitAudioContext;
      if (!Ctor) return;
      const ctx = new Ctor();
      const osc = ctx.createOscillator();
      const gain = ctx.createGain();
      osc.frequency.value = 880;
      gain.gain.value = 0.05;
      osc.connect(gain).connect(ctx.destination);
      osc.start();
      osc.stop(ctx.currentTime + 0.12);
      osc.onended = () => void ctx.close();
    } catch {
      /* autoplay blocked or no audio device */
    }
  },

  async copyToClipboard(text: string): Promise<boolean> {
    try {
      await navigator.clipboard.writeText(text);
      return true;
    } catch {
      return false; // insecure context, no permission, or unsupported
    }
  },
};

// A DEV-DOUBLE RootSecretProvider (shared so web + mobile derive identical keys).
//
// In production the call root secret is the peers' Signal-session shared secret,
// distributed via WS E2EE signaling (rtc-lld §2) so the SFU never sees it — the
// same seam @wa/crypto-wrapper's dev ciphers stand in for. Until the keys-
// directory + ratchet wiring lands, this derives a deterministic per-(pair, room)
// secret from a local seed. It is INSECURE — a real deployment gets a fresh secret
// from the ratchet, not a static shared seed — and never ships. It exists so the
// frame-crypto path is exercised end to end and both peers agree on a key.
//
// Requires WebCrypto (crypto.subtle). Browsers have it natively; React Native
// needs a polyfill (react-native-quick-crypto / expo-crypto), like the rest of
// @wa/call-engine.

import type { RootSecretProvider } from "./callSession";

export function createDevRootSecretProvider(selfId: string, seed: string): RootSecretProvider {
  return {
    async callSecret(peerId, roomId) {
      // Order-independent pairing so both ends derive the same secret.
      const [lo, hi] = selfId < peerId ? [selfId, peerId] : [peerId, selfId];
      const material = new TextEncoder().encode(`wa-call-dev|${seed}|${lo}|${hi}|${roomId}`);
      const digest = await crypto.subtle.digest("SHA-256", material);
      return new Uint8Array(digest);
    },
  };
}

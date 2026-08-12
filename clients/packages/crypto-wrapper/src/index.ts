// @wa/crypto-wrapper — the ONLY package that touches libsignal. It exposes the
// E2EE session contract (establish / encrypt / decrypt) behind interfaces so
// UI and sync code never handle key material (coding-standards.md). The
// production cipher wraps libsignal (e2ee-design §2); MockSessionCipher /
// DevSessionCipher are INSECURE test doubles. E2EEEngine is the integration
// layer (per-device fan-out, self-sync copies, device-list trust).
export * from "./cipher";
export * from "./devSession";
export * from "./engine";
export * from "./senderKey";
export * from "./deviceList";
export * from "./qrLink";

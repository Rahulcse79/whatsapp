// @wa/crypto-wrapper — the ONLY package that touches libsignal. It exposes the
// E2EE session contract (establish / encrypt / decrypt) behind interfaces so
// UI and sync code never handle key material (coding-standards.md). The
// production cipher wraps libsignal (e2ee-design §2); MockSessionCipher is a
// test double.
export * from "./cipher";

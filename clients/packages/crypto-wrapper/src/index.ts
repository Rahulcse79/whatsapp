// Crypto boundary: only this package may import libsignal. UI code never
// sees key-material types (Docs/13-standards/coding-standards.md).
//
// Will expose: session establishment (X3DH), Double Ratchet encrypt/decrypt,
// Sender Keys for groups, device-list signing/verification, safety numbers.
// Design: Docs/06-security/e2ee-design.md. Implementation: task T0.17/T0.20.
export {};

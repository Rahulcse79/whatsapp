// Package auth owns identity: OTP flows (SMS/email/TOTP drivers), JWT
// issuance and refresh rotation, the 2FA registration PIN, and sessions.
//
// Boundary (CI-enforced from T0.06): other contexts call in only through
// port.go; domain/ stays free of I/O imports.
// Design: Docs/05-services/core-api-lld.md §4, Docs/06-security/security-architecture.md §2.
package auth

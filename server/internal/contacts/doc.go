// Package contacts owns hashed address-book sync (peppered HMAC discovery),
// username search, favorites, and blocks. Plaintext address books never
// reach the server; enumeration defenses per threat-model T11.
//
// Design: Docs/04-api/auth-users-api.md, Docs/06-security/threat-model-abuse.md.
package contacts

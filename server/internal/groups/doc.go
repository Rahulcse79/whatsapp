// Package groups owns membership, roles, permissions, invite links/QR, and
// group metadata. Every membership/settings mutation bumps groups.version
// and emits an ordered group event — clients rotate Sender Keys on those
// events; this package never touches key material.
//
// Design: Docs/04-api/messaging-groups-api.md, Docs/06-security/e2ee-design.md §3.
package groups

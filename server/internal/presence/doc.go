// Package presence owns online/last-seen, typing relays, and the
// subscription model (clients subscribe only to on-screen chats — never
// O(contacts) broadcast). State lives exclusively in Valkey; this package
// never touches PostgreSQL. Privacy settings are enforced at subscribe time.
//
// Design: Docs/02-architecture/data-structures-algorithms.md §10–11, HLD §8.5.
package presence

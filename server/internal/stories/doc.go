// Package stories owns status posts: audience snapshots at post time, 24 h
// hard expiry, view receipts. Content is E2E-encrypted with per-story keys
// distributed client-side; this package handles ciphertext refs and metadata only.
//
// Design: Docs/04-api/media-stories-api.md, HLD §12.
package stories

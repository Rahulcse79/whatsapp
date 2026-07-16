// Package media owns upload sessions, presigned URL orchestration, quotas,
// completion verification, and garbage collection. Blobs are always
// ciphertext and never transit our services (clients ↔ MinIO directly).
// Used by cmd/media-svc.
//
// Design: Docs/05-services/media-svc-lld.md, Docs/04-api/media-stories-api.md.
package media

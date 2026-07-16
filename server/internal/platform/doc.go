// Package platform holds cross-cutting infrastructure shared by all
// deployables: config loading, PostgreSQL pool (PgBouncer-aware), Valkey
// client, NATS connection, OpenTelemetry wiring, UUIDv7 generation, the
// GCRA rate limiter, and feature-flag evaluation.
//
// Boundary: platform imports no business contexts — ever.
// Design: Docs/05-services/core-api-lld.md §1.
package platform

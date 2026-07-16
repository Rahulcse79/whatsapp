// Package admin owns trust & safety and operations tooling: the report
// queue, account actions (warn/suspend/ban), feature flags, and metadata-
// only analytics rollups. Every mutating action writes an append-only
// audit_log row in the same transaction. By construction admins can never
// read content — the data does not exist server-side.
//
// Design: Docs/06-security/security-architecture.md §4, HLD §15.6.
package admin

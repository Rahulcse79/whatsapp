// Package ptt implements server-authoritative push-to-talk floor control:
// atomic Valkey Lua acquire/release/heartbeat with fencing sequences, FIFO
// queueing, and SFU publish-permission flips. Budget: p95 grant ≤ 200 ms.
//
// Design: Docs/03-database/valkey-keyspace.md §2,
// Docs/02-architecture/data-structures-algorithms.md §8, Docs/05-services/rtc-lld.md §4.
package ptt

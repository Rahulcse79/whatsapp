// Sync engine: outbox flush loop, cumulative cursors, replay merge, and the
// conflict rules table. UI derives ONLY from the local DB; the network layer
// mutates the DB, never components (binding invariant).
// Design: Docs/11-clients/offline-sync-local-store.md. Implementation: T0.17.
export {};

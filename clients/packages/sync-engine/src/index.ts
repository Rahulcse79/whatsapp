// @wa/sync-engine — the local-first data layer shared by web + mobile:
// crash-safe outbox, per-conversation cursors, and commutative conflict rules.
// Framework-free (no React); UI derives from the local DB, the network layer
// mutates the DB — never components (Docs/11-clients/offline-sync-local-store.md).
export * from "./outbox";
export * from "./cursors";
export * from "./conflict";

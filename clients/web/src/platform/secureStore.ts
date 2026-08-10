import type { SecureStore } from "@wa/client-core";

// Session tokens live in IndexedDB, never localStorage (web-app-architecture.md
// §3 — no secrets in localStorage). The full build wraps values with a WebCrypto
// non-extractable key; the shell stores them directly and leaves the wrapping to
// the security epic.
const DB_NAME = "wav2-secure";
const STORE = "kv";

function openDb(): Promise<IDBDatabase> {
  return new Promise((resolve, reject) => {
    const req = indexedDB.open(DB_NAME, 1);
    req.onupgradeneeded = () => req.result.createObjectStore(STORE);
    req.onsuccess = () => resolve(req.result);
    req.onerror = () => reject(req.error);
  });
}

function run<T>(mode: IDBTransactionMode, op: (store: IDBObjectStore) => IDBRequest<T>): Promise<T> {
  return openDb().then(
    (db) =>
      new Promise<T>((resolve, reject) => {
        const req = op(db.transaction(STORE, mode).objectStore(STORE));
        req.onsuccess = () => resolve(req.result);
        req.onerror = () => reject(req.error);
      }),
  );
}

export const indexedDbSecureStore: SecureStore = {
  async get(key: string): Promise<string | null> {
    const v = await run<unknown>("readonly", (s) => s.get(key));
    return typeof v === "string" ? v : null;
  },
  async set(key: string, value: string): Promise<void> {
    await run("readwrite", (s) => s.put(value, key));
  },
  async delete(key: string): Promise<void> {
    await run("readwrite", (s) => s.delete(key));
  },
};

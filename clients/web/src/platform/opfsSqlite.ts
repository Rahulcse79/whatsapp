import sqlite3InitModule, { type BindingSpec, type Database, type SAHPoolUtil, type Sqlite3Static } from "@sqlite.org/sqlite-wasm";
import type { SqliteDB, SqlRow, SqlValue } from "@wa/client-core";

// The SqliteDB port for the browser — the piece the web client was missing.
//
// The worker previously used MemoryMessageRepo, so every reload discarded every
// conversation and message. The persistent MessageStore already exists and ships
// on mobile; it only needs a SqliteDB, which on native is expo-sqlite and here is
// SQLite compiled to WebAssembly.
//
// Storage is the OPFS SAHPool VFS specifically. The other OPFS backend needs
// SharedArrayBuffer and therefore COOP/COEP headers on the dev server and in
// production; the SAHPool VFS does not, so this works with no server changes and
// no cross-origin isolation. Where OPFS is unavailable at all — a private
// window, an older browser, a worker without storage access — we fall back to an
// in-memory SQLite database, which degrades to exactly today's behaviour rather
// than failing to start.

export interface OpenedDb extends SqliteDB {
  /** True when writes actually survive a reload. False on the memory fallback,
   *  so the caller can tell the user instead of silently losing their history. */
  readonly durable: boolean;
}

/** OPFS directory backing the pool is derived from this name (".wav2-pool"). */
const POOL_NAME = "wav2-pool";

let sqlite3: Sqlite3Static | null = null;

async function loadSqlite(): Promise<Sqlite3Static> {
  if (!sqlite3) sqlite3 = await sqlite3InitModule();
  return sqlite3;
}

// The SAHPool VFS holds an exclusive OPFS sync access handle per pool file, so
// only one client at a time may own the pool. On a reload the outgoing page's
// worker is often still winding down and still holding them, which surfaces as
// NoModificationAllowedError — a race, not a real failure. Retry briefly before
// giving up, otherwise a simple refresh would drop the user into the
// non-persistent fallback and lose their history for that session.
const POOL_RETRIES = 8;
const POOL_RETRY_MS = 250;

async function openPool(s: Sqlite3Static): Promise<SAHPoolUtil> {
  if (typeof s.installOpfsSAHPoolVfs !== "function") {
    throw new Error("OPFS SAHPool VFS unavailable in this build");
  }
  let last: unknown;
  for (let attempt = 0; attempt < POOL_RETRIES; attempt++) {
    try {
      return await s.installOpfsSAHPoolVfs({ name: POOL_NAME });
    } catch (err) {
      last = err;
      // Only a contended pool is worth retrying; anything else (no OPFS at all,
      // storage denied) will fail identically however long we wait.
      if ((err as { name?: string }).name !== "NoModificationAllowedError") throw err;
      await new Promise((r) => setTimeout(r, POOL_RETRY_MS));
    }
  }
  throw last;
}

/** openWebDatabase opens the local message database, persisting to OPFS when the
 *  browser allows it. `name` is the database file name inside the VFS. */
export async function openWebDatabase(name = "wav2.sqlite3"): Promise<OpenedDb> {
  const s = await loadSqlite();

  let db: Database;
  let durable = false;
  try {
    db = new (await openPool(s)).OpfsSAHPoolDb("/" + name);
    durable = true;
  } catch (err) {
    // Not fatal: an in-memory database keeps the app usable, and `durable`
    // tells the caller history will not survive this session.
    const e = err as { name?: string; message?: string };
    console.warn("[db] OPFS unavailable — falling back to a non-persistent database:", e?.name ?? err, e?.message ?? "");
    db = new s.oo1.DB(":memory:");
  }

  // WAL is meaningless for the SAHPool VFS and unsupported in memory; the
  // pragmas that do matter are the ones making writes durable and fast enough.
  try {
    db.exec("PRAGMA synchronous=NORMAL; PRAGMA foreign_keys=ON;");
  } catch {
    /* pragmas are best-effort; a VFS that rejects one must not stop startup */
  }

  return {
    durable,

    async exec(sql: string): Promise<void> {
      db.exec(sql);
    },

    async run(sql: string, params: SqlValue[] = []): Promise<void> {
      db.exec({ sql, bind: params as BindingSpec });
    },

    async all<T extends SqlRow = SqlRow>(sql: string, params: SqlValue[] = []): Promise<T[]> {
      const rows: T[] = [];
      db.exec({
        sql,
        bind: params as BindingSpec,
        // Object rows so callers read columns by name, matching expo-sqlite's
        // getAllAsync and therefore MessageStore's expectations.
        rowMode: "object",
        callback: (row) => {
          rows.push(row as T);
        },
      });
      return rows;
    },
  };
}

import * as SQLite from "expo-sqlite";
import type { SqliteDB, SqlRow, SqlValue } from "../core/ports";

// SqliteDB port backed by expo-sqlite (SQLCipher-capable). The DB key comes from
// the hardware keystore in the full build (mobile-app-architecture.md §2); the
// shell opens an unencrypted DB and leaves that wiring to the security epic.
export async function openDatabase(name = "wav2.db"): Promise<SqliteDB> {
  const db = await SQLite.openDatabaseAsync(name);
  return {
    async exec(sql: string): Promise<void> {
      await db.execAsync(sql);
    },
    async run(sql: string, params: SqlValue[] = []): Promise<void> {
      await db.runAsync(sql, ...params);
    },
    async all<T extends SqlRow = SqlRow>(sql: string, params: SqlValue[] = []): Promise<T[]> {
      return db.getAllAsync<T>(sql, ...params);
    },
  };
}

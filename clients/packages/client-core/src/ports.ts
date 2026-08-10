// Ports: the platform-agnostic seams the framework-free core depends on. Domain
// services import NO react-native modules (mobile-app-architecture.md §1) —
// every side effect (time, sockets, HTTP, local DB, secrets) arrives through
// one of these interfaces, so the core runs and is unit-tested in plain Node
// and the RN/Expo adapters simply implement them.

import type { ClientFrame, ServerFrame } from "./frames";

/** Epoch milliseconds. */
export type Millis = number;

/** Cancels a pending scheduled callback. Safe to call more than once. */
export type Cancel = () => void;

/** Scheduler abstracts timers + clock so tests drive time deterministically. */
export interface Scheduler {
  setTimeout(fn: () => void, ms: Millis): Cancel;
  now(): Millis;
}

/**
 * WsTransport is one WebSocket connection speaking decoded frames. The
 * implementation owns the socket and the codec; the core never sees bytes.
 * Callbacks are assigned by WsClient before the socket opens.
 */
export interface WsTransport {
  send(frame: ClientFrame): void;
  close(code?: number): void;
  onOpen: (() => void) | null;
  onFrame: ((frame: ServerFrame) => void) | null;
  onClose: ((code: number) => void) | null;
  onError: ((err: unknown) => void) | null;
}

/** Builds a fresh transport per connection attempt. */
export type TransportFactory = () => WsTransport;

/** The subset of an HTTP response the core needs from the REST client. */
export interface HttpResponse {
  status: number;
  json(): Promise<unknown>;
}

/** HttpClient is the REST seam (OTP auth, token refresh). */
export interface HttpClient {
  post(path: string, body: unknown, headers?: Record<string, string>): Promise<HttpResponse>;
}

/** A value storable in a SQLite cell. */
export type SqlValue = string | number | null | Uint8Array;
/** One returned row. */
export type SqlRow = Record<string, SqlValue>;

/** SqliteDB is the local-store seam (SQLCipher via expo-sqlite at runtime). */
export interface SqliteDB {
  exec(sql: string): Promise<void>;
  run(sql: string, params?: SqlValue[]): Promise<void>;
  all<T extends SqlRow = SqlRow>(sql: string, params?: SqlValue[]): Promise<T[]>;
}

/** SecureStore holds secrets at rest (Keychain / Android Keystore). */
export interface SecureStore {
  get(key: string): Promise<string | null>;
  set(key: string, value: string): Promise<void>;
  delete(key: string): Promise<void>;
}

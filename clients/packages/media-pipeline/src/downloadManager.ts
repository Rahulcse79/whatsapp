// DownloadManager — the engine behind the "download manager" UX. It runs a
// bounded number of attachment downloads at once, tracks each one's state,
// caches decrypted bytes so a re-open is instant, and lets the UI retry a
// failure. Framework-free: it drives {@link fetchAndDecrypt} and notifies
// subscribers; web/mobile turn the decrypted bytes into object URLs / data URIs.
//
// Deliberately coarse-grained progress (queued → downloading → ready/error):
// the DownloadTransport hands back a whole blob, so there is no byte-level
// progress to report. That's the right fidelity for a chat attachment list —
// a spinner and a retry affordance, not a percent bar.

import { decryptThumbnail, fetchAndDecrypt, type DownloadTransport } from "./download";
import type { MediaEnvelope } from "./envelope";

export type DownloadState = "queued" | "downloading" | "ready" | "error";

/** A snapshot of one attachment's download. Emitted to subscribers on change. */
export interface DownloadItem {
  objectKey: string;
  state: DownloadState;
  /** Decrypted bytes, present once `state === "ready"`. */
  bytes?: Uint8Array;
  /** Failure message, present once `state === "error"`. */
  error?: string;
  /** How many times a download has been attempted (for the retry UI/backoff). */
  attempts: number;
}

export interface DownloadManagerOptions {
  transport: DownloadTransport;
  /** Max concurrent downloads (default 3). */
  concurrency?: number;
}

type Entry = DownloadItem & { env: MediaEnvelope };

/** Manages the lifecycle of many attachment downloads with a concurrency cap. */
export class DownloadManager {
  private readonly transport: DownloadTransport;
  private readonly concurrency: number;
  private readonly entries = new Map<string, Entry>();
  private readonly queue: string[] = [];
  private readonly listeners = new Set<(item: DownloadItem) => void>();
  private active = 0;

  constructor(opts: DownloadManagerOptions) {
    this.transport = opts.transport;
    this.concurrency = Math.max(1, opts.concurrency ?? 3);
  }

  /** request ensures `env` is downloading (or already cached) and returns its
   *  current snapshot. Idempotent: a second request for a ready/in-flight item
   *  neither re-downloads nor re-queues it. */
  request(env: MediaEnvelope): DownloadItem {
    const existing = this.entries.get(env.objectKey);
    if (existing) {
      if (existing.state === "error") this.enqueue(existing); // recoverable → retry
      return snapshot(existing);
    }
    const entry: Entry = { objectKey: env.objectKey, env, state: "queued", attempts: 0 };
    this.entries.set(env.objectKey, entry);
    this.enqueue(entry);
    return snapshot(entry);
  }

  /** retry re-queues a failed item; a no-op for any other state. */
  retry(objectKey: string): void {
    const entry = this.entries.get(objectKey);
    if (entry && entry.state === "error") this.enqueue(entry);
  }

  /** get returns the current snapshot for an object key, if tracked. */
  get(objectKey: string): DownloadItem | undefined {
    const entry = this.entries.get(objectKey);
    return entry ? snapshot(entry) : undefined;
  }

  /** items lists every tracked download, newest activity first is the caller's
   *  concern — insertion order is preserved here. */
  items(): DownloadItem[] {
    return [...this.entries.values()].map(snapshot);
  }

  /** thumbnail decrypts the inline preview (no queue, no network); null when the
   *  envelope carries none. Used to paint a bubble before the full download. */
  thumbnail(env: MediaEnvelope): Promise<Uint8Array | null> {
    return decryptThumbnail(env);
  }

  /** subscribe registers a listener fired on every item state change. Returns an
   *  unsubscribe function. */
  subscribe(fn: (item: DownloadItem) => void): () => void {
    this.listeners.add(fn);
    return () => this.listeners.delete(fn);
  }

  /** forget drops a tracked item, releasing its cached bytes. */
  forget(objectKey: string): void {
    this.entries.delete(objectKey);
  }

  private enqueue(entry: Entry): void {
    if (entry.state === "downloading") return;
    if (this.queue.includes(entry.objectKey)) return;
    entry.state = "queued";
    entry.error = undefined;
    this.queue.push(entry.objectKey);
    this.emit(entry);
    this.pump();
  }

  private pump(): void {
    while (this.active < this.concurrency && this.queue.length > 0) {
      const key = this.queue.shift()!;
      const entry = this.entries.get(key);
      if (!entry || entry.state !== "queued") continue;
      void this.run(entry);
    }
  }

  private async run(entry: Entry): Promise<void> {
    this.active++;
    entry.state = "downloading";
    entry.attempts++;
    this.emit(entry);
    try {
      const bytes = await fetchAndDecrypt(entry.env, this.transport);
      entry.bytes = bytes;
      entry.state = "ready";
      entry.error = undefined;
    } catch (err) {
      entry.state = "error";
      entry.error = err instanceof Error ? err.message : String(err);
    } finally {
      this.active--;
      this.emit(entry);
      this.pump();
    }
  }

  private emit(entry: Entry): void {
    const snap = snapshot(entry);
    for (const fn of this.listeners) fn(snap);
  }
}

function snapshot(entry: Entry): DownloadItem {
  const item: DownloadItem = { objectKey: entry.objectKey, state: entry.state, attempts: entry.attempts };
  if (entry.bytes) item.bytes = entry.bytes;
  if (entry.error) item.error = entry.error;
  return item;
}

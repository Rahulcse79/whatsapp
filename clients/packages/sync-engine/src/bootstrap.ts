// History bootstrap transfer (offline-sync-local-store §6, sequence-diagrams §8).
// When a device is linked, the server holds only the ≤30-day ciphertext window —
// the full history lives on the primary. So the primary streams its history to
// the new device as E2E-encrypted, chunked, resumable ciphertext (relayed by the
// server, which can't read it); the new device imports each chunk, then normal
// delta sync takes over.
//
// Framework-free: records are opaque bytes (the caller serializes a message to a
// record and deserializes it in `importRecords`), the E2EE cipher is a port
// (crypto-wrapper's session cipher in production), and the relay transport is the
// app's. The engine owns the framing, chunking, progress, and resume logic.

/** ChunkCipher seals a plaintext chunk to the new device's session and opens it
 *  on receipt. The server relays only the sealed bytes. */
export interface ChunkCipher {
  seal(plaintext: Uint8Array): Promise<Uint8Array>;
  open(sealed: Uint8Array): Promise<Uint8Array>;
}

/** The sender announces the transfer up front so the receiver knows how many
 *  chunks to expect (progress + resume). */
export interface BootstrapManifest {
  transferId: string;
  totalChunks: number;
  totalRecords: number;
}

/** One relayed unit: the sealed, framed batch of records at index `seq`. */
export interface HistoryChunk {
  transferId: string;
  seq: number;
  sealed: Uint8Array;
}

/** Records per chunk — a batch amortizes the per-message seal + relay overhead. */
export const DEFAULT_CHUNK_RECORDS = 200;

// ── record framing ─────────────────────────────────────────────────────────

/** frameRecords packs a batch into one buffer: u32 count, then (u32 len, bytes)
 *  per record. Reversed by {@link unframeRecords} after decryption. */
export function frameRecords(records: Uint8Array[]): Uint8Array {
  let size = 4;
  for (const r of records) size += 4 + r.length;
  const out = new Uint8Array(size);
  let off = writeU32(out, 0, records.length);
  for (const r of records) {
    off = writeU32(out, off, r.length);
    out.set(r, off);
    off += r.length;
  }
  return out;
}

/** unframeRecords splits a framed buffer back into its records. */
export function unframeRecords(buf: Uint8Array): Uint8Array[] {
  const count = readU32(buf, 0);
  const out: Uint8Array[] = [];
  let off = 4;
  for (let i = 0; i < count; i++) {
    if (off + 4 > buf.length) throw new Error("bootstrap: truncated record header");
    const len = readU32(buf, off);
    off += 4;
    if (off + len > buf.length) throw new Error("bootstrap: truncated record body");
    out.push(buf.subarray(off, off + len));
    off += len;
  }
  return out;
}

// ── sender (primary device) ──────────────────────────────────────────────

/** BootstrapSender turns the primary's history into sealed chunks on demand.
 *  chunk(seq) is idempotent + repeatable, which is what makes the transfer
 *  resumable — the receiver asks for exactly the seqs it is missing. */
export class BootstrapSender {
  private readonly chunkRecords: number;

  constructor(
    private readonly transferId: string,
    private readonly records: Uint8Array[],
    private readonly cipher: ChunkCipher,
    chunkRecords: number = DEFAULT_CHUNK_RECORDS,
  ) {
    if (chunkRecords <= 0) throw new Error("chunkRecords must be positive");
    this.chunkRecords = chunkRecords;
  }

  totalChunks(): number {
    return Math.ceil(this.records.length / this.chunkRecords);
  }

  manifest(): BootstrapManifest {
    return { transferId: this.transferId, totalChunks: this.totalChunks(), totalRecords: this.records.length };
  }

  /** chunk seals the batch at `seq`. */
  async chunk(seq: number): Promise<HistoryChunk> {
    if (seq < 0 || seq >= this.totalChunks()) throw new Error(`bootstrap: chunk ${seq} out of range`);
    const start = seq * this.chunkRecords;
    const batch = this.records.slice(start, start + this.chunkRecords);
    const sealed = await this.cipher.seal(frameRecords(batch));
    return { transferId: this.transferId, seq, sealed };
  }
}

// ── receiver (new device) ──────────────────────────────────────────────────

export interface BootstrapProgress {
  received: number;
  total: number;
}

/** BootstrapReceiver decrypts + imports chunks, dedups re-sends, and tracks
 *  progress so the UI can show it and a restart can resume. */
export class BootstrapReceiver {
  private manifest: BootstrapManifest | null = null;
  private readonly received = new Set<number>();

  constructor(
    private readonly cipher: ChunkCipher,
    private readonly importRecords: (records: Uint8Array[]) => void | Promise<void>,
  ) {}

  setManifest(m: BootstrapManifest): void {
    this.manifest = m;
  }

  /** restore seeds the already-received seqs after a restart (resume). */
  restore(receivedSeqs: number[]): void {
    for (const s of receivedSeqs) this.received.add(s);
  }

  /** accept decrypts, imports, and records a chunk. A duplicate (already-received
   *  seq) is a no-op, so re-sends never double-import. */
  async accept(chunk: HistoryChunk): Promise<void> {
    if (this.received.has(chunk.seq)) return;
    const records = unframeRecords(await this.cipher.open(chunk.sealed));
    await this.importRecords(records);
    this.received.add(chunk.seq);
  }

  /** missing lists the chunk seqs not yet received (empty until the manifest
   *  arrives, or once complete). */
  missing(): number[] {
    if (!this.manifest) return [];
    const out: number[] = [];
    for (let s = 0; s < this.manifest.totalChunks; s++) {
      if (!this.received.has(s)) out.push(s);
    }
    return out;
  }

  receivedSeqs(): number[] {
    return [...this.received].sort((a, b) => a - b);
  }

  progress(): BootstrapProgress {
    return { received: this.received.size, total: this.manifest?.totalChunks ?? 0 };
  }

  complete(): boolean {
    return this.manifest !== null && this.received.size >= this.manifest.totalChunks;
  }
}

// ── u32 helpers (big-endian) ────────────────────────────────────────────────

function writeU32(buf: Uint8Array, off: number, n: number): number {
  buf[off] = (n >>> 24) & 0xff;
  buf[off + 1] = (n >>> 16) & 0xff;
  buf[off + 2] = (n >>> 8) & 0xff;
  buf[off + 3] = n & 0xff;
  return off + 4;
}

function readU32(buf: Uint8Array, off: number): number {
  return ((buf[off]! << 24) | (buf[off + 1]! << 16) | (buf[off + 2]! << 8) | buf[off + 3]!) >>> 0;
}

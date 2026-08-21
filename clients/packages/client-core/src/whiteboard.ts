// Collaborative whiteboard CRDT (T12.02). The board is an append-only op-log:
// stroke adds form a grow-only set, erases are tombstones, and a clear is a
// barrier (seq). Because ops are keyed by a unique id and merged by union, two
// clients that draw concurrently converge without coordination — a CRDT. A
// Lamport-style `seq` gives a deterministic render order. Pure + framework-free;
// transport (REST poll now, WS push later) is the platform's job.

export interface StrokeOp {
  t: "stroke";
  id: string;
  author: string;
  seq: number;
  color: string;
  width: number;
  /** flat, viewport-normalised points [x0,y0,x1,y1,…] in 0..1 */
  points: number[];
}
export interface EraseOp {
  t: "erase";
  id: string;
  author: string;
  seq: number;
  /** the stroke id this tombstones */
  target: string;
}
export interface ClearOp {
  t: "clear";
  id: string;
  author: string;
  seq: number;
}
export type BoardOp = StrokeOp | EraseOp | ClearOp;

/** mergeOps unions two op-logs, de-duping by op id (idempotent + commutative —
 *  the CRDT merge). */
export function mergeOps(a: BoardOp[], b: BoardOp[]): BoardOp[] {
  const byId = new Map<string, BoardOp>();
  for (const o of a) byId.set(o.id, o);
  for (const o of b) byId.set(o.id, o);
  return [...byId.values()];
}

/** nextSeq is a Lamport clock tick: one past the max seq seen, so a new local op
 *  always sorts after everything it has observed. */
export function nextSeq(ops: BoardOp[]): number {
  let max = 0;
  for (const o of ops) if (o.seq > max) max = o.seq;
  return max + 1;
}

/** maxSeq returns the highest seq in the log (a sync cursor). */
export function maxSeq(ops: BoardOp[]): number {
  let max = 0;
  for (const o of ops) if (o.seq > max) max = o.seq;
  return max;
}

/** renderStrokes reduces the op-log to the visible strokes, in draw order:
 *  ordered by (seq, id); strokes after the last clear that aren't tombstoned. */
export function renderStrokes(ops: BoardOp[]): StrokeOp[] {
  const sorted = [...ops].sort((x, y) => x.seq - y.seq || (x.id < y.id ? -1 : x.id > y.id ? 1 : 0));
  let clearSeq = 0;
  const erased = new Set<string>();
  for (const o of sorted) {
    if (o.t === "clear" && o.seq > clearSeq) clearSeq = o.seq;
    else if (o.t === "erase") erased.add(o.target);
  }
  return sorted.filter((o): o is StrokeOp => o.t === "stroke" && o.seq > clearSeq && !erased.has(o.id));
}

/** makeStroke builds a local stroke op with a fresh Lamport seq. */
export function makeStroke(ops: BoardOp[], author: string, id: string, color: string, width: number, points: number[]): StrokeOp {
  return { t: "stroke", id, author, seq: nextSeq(ops), color, width, points };
}
export function makeErase(ops: BoardOp[], author: string, id: string, target: string): EraseOp {
  return { t: "erase", id, author, seq: nextSeq(ops), target };
}
export function makeClear(ops: BoardOp[], author: string, id: string): ClearOp {
  return { t: "clear", id, author, seq: nextSeq(ops) };
}

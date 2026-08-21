import { describe, expect, it } from "vitest";
import { makeClear, makeErase, makeStroke, maxSeq, mergeOps, nextSeq, renderStrokes, type BoardOp } from "./whiteboard";

const stroke = (id: string, seq: number, author = "a"): BoardOp => ({ t: "stroke", id, author, seq, color: "#000", width: 2, points: [0, 0, 1, 1] });

describe("mergeOps (CRDT union)", () => {
  it("is commutative + idempotent, de-duping by id", () => {
    const a = [stroke("s1", 1), stroke("s2", 2)];
    const b = [stroke("s2", 2), stroke("s3", 3)];
    expect(mergeOps(a, b).length).toBe(3);
    expect(mergeOps(b, a).length).toBe(3);
    expect(mergeOps(a, a).length).toBe(2);
  });
});

describe("seq clocks", () => {
  it("nextSeq is one past the max; maxSeq reads the cursor", () => {
    const ops = [stroke("s1", 1), stroke("s2", 5)];
    expect(maxSeq(ops)).toBe(5);
    expect(nextSeq(ops)).toBe(6);
    expect(nextSeq([])).toBe(1);
  });
});

describe("renderStrokes", () => {
  it("orders by seq then id and drops tombstoned strokes", () => {
    const ops: BoardOp[] = [stroke("s2", 2), stroke("s1", 1), { t: "erase", id: "e1", author: "b", seq: 3, target: "s1" }];
    const out = renderStrokes(ops);
    expect(out.map((s) => s.id)).toEqual(["s2"]); // s1 erased, s2 remains
  });
  it("a clear hides everything drawn before it, keeps later strokes", () => {
    const ops: BoardOp[] = [stroke("s1", 1), { t: "clear", id: "c1", author: "a", seq: 2 }, stroke("s3", 3)];
    expect(renderStrokes(ops).map((s) => s.id)).toEqual(["s3"]);
  });
  it("converges regardless of merge order (concurrent draws)", () => {
    const p1: BoardOp[] = [stroke("s1", 1, "alice")];
    const p2: BoardOp[] = [stroke("s2", 1, "bob")]; // same seq, concurrent
    const left = renderStrokes(mergeOps(p1, p2)).map((s) => s.id);
    const right = renderStrokes(mergeOps(p2, p1)).map((s) => s.id);
    expect(left).toEqual(right); // deterministic tie-break by id
    expect(left).toEqual(["s1", "s2"]);
  });
});

describe("op builders", () => {
  it("stamp a fresh Lamport seq", () => {
    const ops = [stroke("s1", 4)];
    expect(makeStroke(ops, "a", "s2", "#f00", 3, [0, 0]).seq).toBe(5);
    expect(makeErase(ops, "a", "e1", "s1").seq).toBe(5);
    expect(makeClear(ops, "a", "c1").seq).toBe(5);
  });
});

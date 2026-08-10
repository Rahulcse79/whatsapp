// Conflict resolution for the local store. The server's per-conversation seq
// gives total order, so only commutative metadata merges remain — no CRDTs
// needed (Docs/11-clients/offline-sync-local-store.md §4).

/** A message as materialised locally (the fields conflict rules touch). */
export interface LocalMessage {
  msgUuid: string;
  seq: number;
  deleted: boolean;
  editedBody?: string; // last applied edit, if any
  reactions: Record<string, Set<string>>; // emoji → set of reactor userIds
  pinned?: boolean; // conversation pin — syncs to the user's own devices (KindPin)
  starred?: boolean; // purely local, per-device — never leaves the device
}

/** OverlayKind is the type of an overlay event referencing a message. */
export type OverlayKind =
  | "edit"
  | "delete"
  | "reaction-add"
  | "reaction-remove"
  | "pin"
  | "unpin"
  | "star"
  | "unstar";

export interface Overlay {
  kind: OverlayKind;
  editBody?: string;
  emoji?: string;
  reactorUserId?: string;
}

/**
 * applyOverlay folds an overlay into a message idempotently. Rules:
 *  - delete is terminal and wins over any edit (a deleted message stays deleted).
 *  - edits apply only to non-deleted messages; the latest edit wins (callers
 *    apply in seq order, so "latest" = last applied).
 *  - reactions are a set-union keyed by (emoji, reactor); removal tombstones
 *    the pair. Applying the same reaction twice is a no-op.
 */
export function applyOverlay(msg: LocalMessage, overlay: Overlay): LocalMessage {
  switch (overlay.kind) {
    case "delete":
      return { ...msg, deleted: true, editedBody: undefined };
    case "edit":
      if (msg.deleted) return msg; // delete wins
      return { ...msg, editedBody: overlay.editBody };
    case "reaction-add": {
      if (!overlay.emoji || !overlay.reactorUserId) return msg;
      const reactions = cloneReactions(msg.reactions);
      (reactions[overlay.emoji] ??= new Set()).add(overlay.reactorUserId);
      return { ...msg, reactions };
    }
    case "reaction-remove": {
      if (!overlay.emoji || !overlay.reactorUserId) return msg;
      const reactions = cloneReactions(msg.reactions);
      const set = reactions[overlay.emoji];
      if (set) {
        set.delete(overlay.reactorUserId);
        if (set.size === 0) delete reactions[overlay.emoji];
      }
      return { ...msg, reactions };
    }
    // Pin/star are boolean flags (last-writer-wins per device). Pin syncs to the
    // user's own devices via a KindPin overlay; star is purely local.
    case "pin":
      return { ...msg, pinned: true };
    case "unpin":
      return { ...msg, pinned: false };
    case "star":
      return { ...msg, starred: true };
    case "unstar":
      return { ...msg, starred: false };
  }
}

function cloneReactions(r: Record<string, Set<string>>): Record<string, Set<string>> {
  const out: Record<string, Set<string>> = {};
  for (const [emoji, set] of Object.entries(r)) out[emoji] = new Set(set);
  return out;
}

/**
 * mergeReadSeq merges a read-watermark across a user's own devices: read state
 * only ever advances (monotonic max), so self-sync from multiple devices
 * converges without conflict.
 */
export function mergeReadSeq(a: number, b: number): number {
  return Math.max(a, b);
}

// Dedicated worker: the local DB + crypto, kept off the main thread so the UI
// stays at 60 fps (web-app-architecture.md §1). Storage is SQLite-wasm on OPFS —
// the same MessageStore mobile runs, over a browser SqliteDB — plus the T0.20
// DevSessionCipher (INSECURE dev double; real libsignal is the seam). It speaks
// the RPC protocol in ./rpc.
//
// This used to be MemoryMessageRepo, which meant every reload discarded every
// conversation and message. The server cannot restore them: message_inbox is a
// relay buffer that DELETEs on ack (ADR-001, no server-side archive), so the
// local database is the only durable copy of a chat's history.

import { Cursors } from "@wa/sync-engine";
import { MessageStore, MsgKind, type InboxBatch, type MessageRepo } from "@wa/client-core";
import { openWebDatabase } from "../platform/opfsSqlite";
import type { EnqueueOverlayInput, EnqueueTextInput, MarkReceiptInput, MarkSentInput, RpcRequest, SearchInput } from "./rpc";

// Opening SQLite-wasm is async, so the store is a promise every RPC awaits.
// Memoising the promise (rather than the resolved store) means concurrent
// requests arriving during startup queue on the same open instead of racing it
// and creating two databases over one file.
let opening: Promise<MessageRepo> | null = null;
let durable = true;
function repo(): Promise<MessageRepo> {
  if (!opening) {
    opening = openWebDatabase().then((db) => {
      durable = db.durable;
      if (!durable) {
        console.warn("[db] no persistent storage available — history will not survive a reload");
      }
      return new MessageStore(db, new Cursors());
    });
  }
  return opening;
}
const encoder = new TextEncoder();
const decoder = new TextDecoder();

// Conversation-shared dev cipher (Milestone 1): both peers derive the SAME
// AES-GCM key from the shared conversation_id, so A's ciphertext opens on B.
// (The old self-addressed DevSessionCipher keyed on a random per-device secret,
// so a device could only round-trip its OWN messages.) This is an INSECURE dev
// double — anyone who learns the conversation_id can derive the key; real
// per-device libsignal E2EE (keys directory + E2EEEngine) is the seam that
// replaces it. WebCrypto is available (worker on a secure context).
const keyCache = new Map<string, Promise<CryptoKey>>();

function convKey(conversationId: string): Promise<CryptoKey> {
  let k = keyCache.get(conversationId);
  if (!k) {
    k = crypto.subtle
      .digest("SHA-256", encoder.encode("wa-dev-conv-v1:" + conversationId))
      .then((raw) => crypto.subtle.importKey("raw", raw, "AES-GCM", false, ["encrypt", "decrypt"]));
    keyCache.set(conversationId, k);
  }
  return k;
}

async function seal(conversationId: string, text: string): Promise<Uint8Array> {
  const key = await convKey(conversationId);
  const iv = crypto.getRandomValues(new Uint8Array(12));
  const ct = new Uint8Array(await crypto.subtle.encrypt({ name: "AES-GCM", iv }, key, encoder.encode(text)));
  const out = new Uint8Array(12 + ct.length);
  out.set(iv, 0);
  out.set(ct, 12);
  return out;
}

async function openSealed(conversationId: string, envelope: Uint8Array): Promise<string> {
  const key = await convKey(conversationId);
  const iv = envelope.subarray(0, 12);
  const ct = envelope.subarray(12);
  const pt = await crypto.subtle.decrypt({ name: "AES-GCM", iv }, key, ct);
  return decoder.decode(pt);
}

const handlers: Record<string, (arg: unknown) => Promise<unknown>> = {
  init: async () => (await repo()).init(),
  storageDurable: async () => {
    await repo();
    return durable;
  },
  persistInboxBatch: async (arg) => {
    const batch = arg as InboxBatch;
    // Open each inbound ciphertext with the conversation-shared key so the
    // recipient stores real text, not the "[encrypted]" placeholder. A failure
    // (foreign/rotated key) just leaves that item as the placeholder.
    const bodies = new Map<string, string>();
    for (const it of batch.items) {
      try {
        bodies.set(it.msgUuid, await openSealed(it.conversationId, it.ciphertext));
      } catch {
        /* undefined → placeholder */
      }
    }
    return (await repo()).persistInboxBatch(batch, bodies);
  },
  enqueueText: async (arg) => {
    const input = arg as EnqueueTextInput;
    // input.text is the already-encoded body (text, optionally + link preview);
    // seal the whole thing so the recipient decrypts to the same body.
    const payload = await seal(input.conversationId, input.text);
    await (await repo()).enqueueOutgoing({
      clientRef: input.clientRef,
      conversationId: input.conversationId,
      plaintext: input.text,
      payload,
      now: input.now,
      listText: input.listText,
    });
  },
  enqueueOverlay: async (arg) => {
    const input = arg as EnqueueOverlayInput;
    // Delete needs no content; edit seals the new body so the recipient decrypts
    // it. The overlay's clientRef becomes its own msgUuid on the wire; the
    // recipient keys the decrypted edit text by it (planInboxBatch → OverlayApply).
    const payload = await seal(input.conversationId, input.kind === "delete" ? "" : input.text);
    const overlayKind =
      input.kind === "delete" ? MsgKind.OVERLAY_DELETE : input.kind === "react" ? MsgKind.REACTION : MsgKind.OVERLAY_EDIT;
    await (await repo()).enqueueOutgoing({
      clientRef: input.clientRef,
      conversationId: input.conversationId,
      plaintext: input.text,
      payload,
      now: input.now,
      kind: overlayKind,
      overlayTarget: input.targetMsgUuid,
    });
  },
  setPinned: async (arg) => {
    const i = arg as { msgUuid: string; pinned: boolean };
    await (await repo()).setPinned(i.msgUuid, i.pinned);
  },
  setStarred: async (arg) => {
    const i = arg as { msgUuid: string; starred: boolean };
    await (await repo()).setStarred(i.msgUuid, i.starred);
  },
  deleteForMe: async (arg) => {
    const i = arg as { msgUuid: string };
    await (await repo()).deleteForMe(i.msgUuid);
  },
  markSent: async (arg) => {
    const input = arg as MarkSentInput;
    await (await repo()).markSent(input.clientRef, input.seq);
  },
  markReceipt: async (arg) => {
    const input = arg as MarkReceiptInput;
    await (await repo()).markReceipt(input.conversationId, input.kind, input.upToSeq);
  },
  pendingSends: async () => (await repo()).pendingSends(),
  conversations: async () => (await repo()).conversations(),
  thread: async (arg) => (await repo()).thread(arg as string),
  search: async (arg) => {
    const input = arg as SearchInput;
    return (await repo()).search(input.query, {
      conversationId: input.conversationId,
      limit: input.limit,
      fromMe: input.fromMe,
      after: input.after,
      before: input.before,
      mediaOnly: input.mediaOnly,
      hashtag: input.hashtag,
    });
  },
};

// Cast self to just the worker surface we use — avoids the DOM-vs-WebWorker
// global `self` type clash without pulling in the webworker lib.
const ctx = self as unknown as {
  postMessage(message: unknown): void;
  addEventListener(type: "message", listener: (e: MessageEvent) => void): void;
};

ctx.addEventListener("message", (e: MessageEvent) => {
  const { id, method, arg } = e.data as RpcRequest;
  const fn = handlers[method];
  if (!fn) {
    ctx.postMessage({ id, error: `unknown method: ${method}` });
    return;
  }
  fn(arg)
    .then((result) => ctx.postMessage({ id, result }))
    .catch((err: unknown) => ctx.postMessage({ id, error: String(err) }));
});

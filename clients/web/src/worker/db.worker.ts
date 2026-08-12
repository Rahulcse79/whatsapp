// Dedicated worker: the local DB + crypto, kept off the main thread so the UI
// stays at 60 fps (web-app-architecture.md §1). The shell uses the in-memory
// MemoryMessageRepo (OPFS SQLite-wasm lands with the persistence epic) and the
// T0.20 DevSessionCipher (INSECURE dev double; real libsignal is the seam). It
// speaks the RPC protocol in ./rpc.

import { MemoryMessageRepo, type InboxBatch } from "@wa/client-core";
import { DevSessionCipher, generateDevIdentity } from "@wa/crypto-wrapper";
import type { EnqueueTextInput, MarkSentInput, RpcRequest, SearchInput } from "./rpc";

const repo = new MemoryMessageRepo();
// E2EE lives in the worker so key material never reaches the UI thread. The
// T0.20 DevSessionCipher is an INSECURE dev double; the live per-device fan-out
// (E2EEEngine + keys directory + real libsignal) activates once the keys API is
// wired. Self-addressed placeholder peer until then.
const cipher = new DevSessionCipher(generateDevIdentity({ userId: "self", deviceId: "web" }));
const encoder = new TextEncoder();
async function seal(conversationId: string, text: string): Promise<Uint8Array> {
  const address = { userId: conversationId, deviceId: "peer" };
  if (!cipher.hasSession(address)) {
    await cipher.establish({ address, identityKey: encoder.encode(conversationId), signedPrekey: encoder.encode("spk") });
  }
  return cipher.encrypt(address, encoder.encode(text));
}

const handlers: Record<string, (arg: unknown) => Promise<unknown>> = {
  init: () => repo.init(),
  persistInboxBatch: (arg) => repo.persistInboxBatch(arg as InboxBatch),
  enqueueText: async (arg) => {
    const input = arg as EnqueueTextInput;
    // input.text is the already-encoded body (text, optionally + link preview);
    // seal the whole thing so the recipient decrypts to the same body.
    const payload = await seal(input.conversationId, input.text);
    await repo.enqueueOutgoing({
      clientRef: input.clientRef,
      conversationId: input.conversationId,
      plaintext: input.text,
      payload,
      now: input.now,
      listText: input.listText,
    });
  },
  markSent: async (arg) => {
    const input = arg as MarkSentInput;
    await repo.markSent(input.clientRef, input.seq);
  },
  pendingSends: () => repo.pendingSends(),
  conversations: () => repo.conversations(),
  thread: (arg) => repo.thread(arg as string),
  search: (arg) => {
    const input = arg as SearchInput;
    return repo.search(input.query, { conversationId: input.conversationId, limit: input.limit });
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

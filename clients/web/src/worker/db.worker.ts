// Dedicated worker: the local DB + crypto, kept off the main thread so the UI
// stays at 60 fps (web-app-architecture.md §1). The shell uses the in-memory
// MemoryMessageRepo (OPFS SQLite-wasm lands with the persistence epic) and
// MockSessionCipher (real X3DH/Double-Ratchet at T0.20). It speaks the RPC
// protocol in ./rpc.

import { MemoryMessageRepo, type InboxBatch } from "@wa/client-core";
import { MockSessionCipher } from "@wa/crypto-wrapper";
import type { EnqueueTextInput, MarkSentInput, RpcRequest } from "./rpc";

const repo = new MemoryMessageRepo();
const cipher = new MockSessionCipher();
const encoder = new TextEncoder();

// Placeholder E2EE seam: sealing runs in the worker so key material never
// reaches the UI thread. Self-addressed + INSECURE until T0.20.
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
    const payload = await seal(input.conversationId, input.text);
    await repo.enqueueOutgoing({
      clientRef: input.clientRef,
      conversationId: input.conversationId,
      plaintext: input.text,
      payload,
      now: input.now,
    });
  },
  markSent: async (arg) => {
    const input = arg as MarkSentInput;
    await repo.markSent(input.clientRef, input.seq);
  },
  pendingSends: () => repo.pendingSends(),
  conversations: () => repo.conversations(),
  thread: (arg) => repo.thread(arg as string),
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

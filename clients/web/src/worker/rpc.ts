// Typed RPC across the dedicated DB/crypto worker. The main thread holds a DbApi
// proxy; the worker implements the same method names over postMessage. Uint8Array
// payloads survive structured clone, so MsgSend frames cross intact.

import type {
  ChatSummary,
  ConversationCursor,
  InboxBatch,
  MsgSend,
  ReceiptKind,
  SearchHit,
  ThreadMessage,
} from "@wa/client-core";

export interface EnqueueTextInput {
  conversationId: string;
  /** The encoded plaintext body to seal + store (may carry a link preview). */
  text: string;
  clientRef: string;
  now: number;
  /** Human text for the chat-list preview when `text` is an encoded body. */
  listText?: string;
}

export interface MarkSentInput {
  clientRef: string;
  seq: number;
}

export interface MarkReceiptInput {
  conversationId: string;
  kind: ReceiptKind;
  upToSeq: number;
}

export interface SearchInput {
  query: string;
  conversationId?: string;
  limit?: number;
}

/** The data + crypto surface the worker exposes to the main thread. */
export interface DbApi {
  init(): Promise<ConversationCursor[]>;
  persistInboxBatch(batch: InboxBatch): Promise<ConversationCursor[]>;
  enqueueText(input: EnqueueTextInput): Promise<void>;
  markSent(input: MarkSentInput): Promise<void>;
  markReceipt(input: MarkReceiptInput): Promise<void>;
  pendingSends(): Promise<MsgSend[]>;
  conversations(): Promise<ChatSummary[]>;
  thread(conversationId: string): Promise<ThreadMessage[]>;
  search(input: SearchInput): Promise<SearchHit[]>;
}

export interface RpcRequest {
  id: number;
  method: string;
  arg: unknown;
}

export interface RpcResponse {
  id: number;
  result?: unknown;
  error?: string;
}

/** createDbClient wraps a Worker in a promise-returning DbApi proxy. */
export function createDbClient(worker: Worker): DbApi {
  let seq = 0;
  const pending = new Map<number, { resolve: (v: unknown) => void; reject: (e: unknown) => void }>();

  worker.addEventListener("message", (e: MessageEvent<RpcResponse>) => {
    const msg = e.data;
    const waiter = pending.get(msg.id);
    if (!waiter) return;
    pending.delete(msg.id);
    if (msg.error !== undefined) waiter.reject(new Error(msg.error));
    else waiter.resolve(msg.result);
  });

  function call<T>(method: string, arg?: unknown): Promise<T> {
    const id = seq++;
    return new Promise<T>((resolve, reject) => {
      pending.set(id, { resolve: (v) => resolve(v as T), reject });
      const req: RpcRequest = { id, method, arg };
      worker.postMessage(req);
    });
  }

  return {
    init: () => call<ConversationCursor[]>("init"),
    persistInboxBatch: (batch) => call<ConversationCursor[]>("persistInboxBatch", batch),
    enqueueText: (input) => call<void>("enqueueText", input),
    markSent: (input) => call<void>("markSent", input),
    markReceipt: (input) => call<void>("markReceipt", input),
    pendingSends: () => call<MsgSend[]>("pendingSends"),
    conversations: () => call<ChatSummary[]>("conversations"),
    thread: (conversationId) => call<ThreadMessage[]>("thread", conversationId),
    search: (input) => call<SearchHit[]>("search", input),
  };
}

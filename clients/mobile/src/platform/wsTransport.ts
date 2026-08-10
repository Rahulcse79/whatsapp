import type { ClientFrame, ServerFrame } from "../core/frames";
import type { TransportFactory, WsTransport } from "../core/ports";

// DEV/OFFLINE codec: frames as JSON (Uint8Array fields carried as tagged number
// arrays). The wire contract is binary protobuf (websocket-protocol.md §Wire);
// the production transport swaps this codec for @wa/proto-types once that
// package is generated for the clients build (task T0.20). The core state
// machine is codec-agnostic — only this file changes.
export function createWsTransportFactory(url: string): TransportFactory {
  return () => new JsonWsTransport(url);
}

class JsonWsTransport implements WsTransport {
  onOpen: (() => void) | null = null;
  onFrame: ((f: ServerFrame) => void) | null = null;
  onClose: ((code: number) => void) | null = null;
  onError: ((err: unknown) => void) | null = null;

  private readonly ws: WebSocket;

  constructor(url: string) {
    this.ws = new WebSocket(url);
    this.ws.onopen = () => this.onOpen?.();
    this.ws.onmessage = (e: MessageEvent) => {
      try {
        this.onFrame?.(decode(e.data));
      } catch (err) {
        this.onError?.(err);
      }
    };
    this.ws.onclose = (e: CloseEvent) => this.onClose?.(e.code);
    this.ws.onerror = (e) => this.onError?.(e);
  }

  send(frame: ClientFrame): void {
    this.ws.send(encode(frame));
  }
  close(code?: number): void {
    this.ws.close(code);
  }
}

function encode(frame: ClientFrame): string {
  return JSON.stringify(frame, (_k, v: unknown) =>
    v instanceof Uint8Array ? { __u8: Array.from(v) } : v,
  );
}

function decode(data: unknown): ServerFrame {
  const text = typeof data === "string" ? data : "";
  return JSON.parse(text, (_k, v: unknown) => {
    if (v && typeof v === "object" && "__u8" in v && Array.isArray((v as { __u8: unknown }).__u8)) {
      return new Uint8Array((v as { __u8: number[] }).__u8);
    }
    return v;
  }) as ServerFrame;
}

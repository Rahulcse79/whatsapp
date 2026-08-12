// @wa/client-core — the framework-free client engine shared by web + mobile.
// Wire frames + ports, the WS reconnect/resume state machine, full-jitter
// backoff, the OTP/refresh REST client, session storage, and the local-store
// logic. No DOM or React-Native imports; platform adapters (sockets, HTTP,
// SQLite, secure storage) are injected via the port interfaces here.
export * from "./frames";
export * from "./ports";
export * from "./backoff";
export * from "./ids";
export * from "./session";
export * from "./otpClient";
export * from "./wsClient";
export * from "./search";
export * from "./db/schema";
export * from "./db/outboxStore";
export * from "./db/messageStore";
export * from "./memoryRepo";

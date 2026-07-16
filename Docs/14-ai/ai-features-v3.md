# AI Features — V3 (Deferred)

| Doc | AI under E2EE: what's possible, what's forbidden, V3 designs |
|---|---|
| Status | Backlog design note (no V2 work) · Upstream: HLD §2.3, Appendix D E28 |
| Iron rule | The server cannot run AI on content it cannot read. Every AI feature is **on-device** or operates on **explicitly user-disclosed** data. "Send your chats to our AI cloud" is a privacy-posture break and is rejected regardless of demand. |

## Feasibility matrix

| Feature | Where it can run | Verdict |
|---|---|---|
| Smart replies | On-device small LM (Gemma/Phi-class, quantized) over local history | ✅ V3 |
| Voice-note transcription | On-device Whisper-class (RN JSI / WASM); language packs downloadable | ✅ V3 — highest user value |
| Message translation | On-device NMT models per language pair | ✅ V3 |
| Semantic search | On-device embeddings + local vector index (SQLite-vec) alongside FTS5 | ✅ V3 |
| Spam detection | Server-side **metadata-only** (fan-out patterns — already in threat-model T10); content classifiers only client-side with local-only verdicts | ✅ limited |
| "AI assistant you chat with" | Opt-in bot conversation = user knowingly sends content to a disclosed endpoint (clearly not E2EE with a human; labeled in UI) | ✅ possible, explicit consent surface |
| Server-side summarization/moderation of chats | — | ❌ impossible by construction (e2ee-design §8) |

## Design constraints for the V3 implementation

- Model runtime: llama.cpp-class via JSI (mobile) / WASM+WebGPU (web); models fetched as signed packs from MinIO (self-hostable — consistent with offline profile); nothing phones home.
- Battery/storage budgets: transcription on-demand only; embeddings built incrementally on charge+wifi; every model pack opt-in per feature.
- Evaluation before ship: on-device latency budgets (transcription ≤ 1× realtime on mid-tier), quality bars per language, kill-switch flags.
- Anthropic/OpenAI-style cloud APIs: only ever for the explicit opt-in assistant bot (disclosed endpoint), never for user↔user content processing.

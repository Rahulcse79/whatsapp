# server/

Go backend — no implementation code yet (blueprint phase, see [/Docs/README.md](../Docs/README.md)).

| Dir | Will contain | Blueprint |
|---|---|---|
| `cmd/` | One main per deployable: `core-api`, `ws-gateway`, `media-svc`, `notification-svc` | [Docs/05-services/](../Docs/05-services/core-api-lld.md) LLDs |
| `internal/` | Bounded contexts: auth, keys, chat, groups, calls, ptt, stories, presence, contacts, admin + `platform/` | [core-api-lld.md](../Docs/05-services/core-api-lld.md) §1 layout + import rules |
| `proto/` | Source of truth: WS frames, gRPC, NATS events (buf; codegen Go+TS) | [Docs/04-api/websocket-protocol.md](../Docs/04-api/websocket-protocol.md) |

First tasks: T0.01–T0.02, T0.05 in [task-breakdown.md](../Docs/12-planning/task-breakdown.md).

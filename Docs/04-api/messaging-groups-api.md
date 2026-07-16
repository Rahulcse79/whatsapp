# REST API — Groups (+ Messaging Semantics Reference)

| Doc | Group management REST; message semantics pointers |
|---|---|
| Status | v1.0 · Messaging itself is **WS-only** ([websocket-protocol.md](websocket-protocol.md)) — REST here is group *metadata* |

## Groups (`/v1/groups`) — FR-GRP-*

| Endpoint | Req → Resp | Errors / notes |
|---|---|---|
| `POST /` | `{name, description?, member_ids[≤1023]}` → `{group, conversation_id}` | Creator = owner; emits `group.member_added`×N |
| `GET /{id}` | → group + settings + my role | Members-only |
| `PATCH /{id}` | `{name?, description?, avatar_ref?}` → 204 | Permission `who_can_edit_info` |
| `PUT /{id}/settings` | `{who_can_post, who_can_edit_info, announcements}` → 204 | Admin+ |
| `GET /{id}/members?cursor=` | paged members+roles | |
| `POST /{id}/members` | `{user_ids[]}` → per-id results | Cap 1,024 `STATE_GROUP_FULL`; **triggers Sender-Key rotation** (client-driven — see note) |
| `DELETE /{id}/members/{uid}` | → 204 | Admin+; rotation trigger |
| `PUT /{id}/members/{uid}/role` | `{role}` → 204 | Owner promotes/demotes admins |
| `POST /{id}/leave` | → 204 | Owner must transfer first `STATE_OWNER_MUST_TRANSFER` |
| `DELETE /{id}` | → 204 | Owner only; tombstone + client-side purge event |
| `POST /{id}/invite-links` | `{expires?, max_uses?}` → `{token, url, qr}` | Admin+; revoke via `DELETE /invite-links/{token}` |
| `POST /join` | `{token}` → `{group}` | Respects group caps; join emits membership event |

## Messaging semantics quick reference (normative source: WS protocol doc)

| Concern | Rule |
|---|---|
| Send path | WS `MsgSend` only; REST never carries message content |
| Group encryption | Sender Keys: encrypt once → server fans ciphertext (HLD §8.3) |
| Key rotation | **Client-driven, server-signaled**: membership events (`group_event` frames, ordered per group) tell members to rotate; new Sender Key distributed pairwise via Signal sessions. Server never sees keys |
| Announcements mode | Non-admin `MsgSend` to the group's conversation → `GROUP_POSTING_RESTRICTED` |
| Aggregate receipts | Ticks flip when **all** member devices reach state; per-member detail = client-side message info |
| Big-group throttle | ≥ 256 members: 2 msg/s per sender ([api-standards.md](api-standards.md) §4) |
| Mentions | Inside ciphertext; client renders + elevates notification locally (FR-NOTIF-02) |

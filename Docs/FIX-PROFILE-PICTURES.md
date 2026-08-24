# Profile pictures: analysis and fix plan

Request: fix everything related to profile pictures, and put an upload control
on the avatar itself.

## Finding: the feature does not exist at any layer

There is no bug to fix in an existing profile-picture path, because there is no
path. Each layer stops short:

| Layer | State |
|---|---|
| Schema | `users.avatar_ref` exists (migration 000002, FK added in 000003) |
| Server model | `profile.Profile` has **no avatar field** |
| Server store | `Get` / `Public` / `Update` **never read or write `avatar_ref`** |
| Server API | `PUT /v1/me` accepts display name, username, about — **no avatar** |
| Client service | no upload, no fetch, no cache |
| `Avatar` component | accepts `name` and `id` props and **ignores both** — it always renders the default silhouette |
| Profile screen | shows `<Avatar size={88} />` with **no upload control** |

So the column has been sitting unused since 000002, and the component that would
display a picture cannot display one.

### The latent type bug, in two more places

`users.avatar_ref` is `uuid REFERENCES media_objects(id)`. The media pipeline
yields an **object key (text)**, not a `media_objects` id — which is exactly the
defect migration 000023 fixed for stories:

> *"the 000006 FK to media_objects made image stories fail (object key isn't a
> uuid)… Store the object key as text."*

The same mistake is unfixed in `users.avatar_ref` **and** `groups.avatar_ref`
(migration 000004, whose `UpdateInfo` still casts `$4::uuid`). Any attempt to
set either from a real upload would fail on the cast. This fix corrects `users`;
`groups` is flagged as a follow-up rather than silently changed.

### Encryption: avatars are identity metadata, not message content

The media pipeline's `prepare()` always encrypts, and distributing a per-file
key to every contact is the same unsolved problem that leaves stories
undecryptable. Avatars do not need it: `internal/profile`'s own package doc says
it holds *"only identity metadata — so no E2EE concerns"*, and username, display
name and about are already plaintext on the server. An avatar belongs in that
set.

So the avatar uploads as **plaintext** through `ResumableUploader` directly,
reusing the resumable/multipart machinery while skipping `prepare()`'s
encryption. Visibility is enforced by the existing `avatar` privacy setting
(`everyone` / `contacts` / `nobody`), which the privacy model already defines and
nothing currently honours.

## Phases

- [x] **Phase 1 — Server.** Migration: `users.avatar_ref` uuid → text, dropping
  the FK (the 000023 fix, applied where it was missed). Carry `AvatarRef`
  through `Profile`, `Get`, `Public` and `Update`; accept it on `PUT /v1/me`;
  return it from `GET /v1/me` and `GET /v1/users/{id}`, with the public view
  honouring the `avatar` privacy setting.
- [ ] **Phase 2 — Client service.** `uploadAvatar(bytes, mime)` (plaintext via
  the uploader, then `PUT /v1/me`), `removeAvatar()`, and a cached
  `avatarUrlFor(userId)` that resolves an object key to a download URL.
- [ ] **Phase 3 — Avatar component.** Render the picture when one is known,
  falling back to the existing silhouette. Every existing call site keeps
  working unchanged.
- [ ] **Phase 4 — Upload control.** A camera badge on the profile avatar
  (WhatsApp's affordance): click to pick a file, with client-side validation,
  live update, and a remove action.
- [ ] **Phase 5 — Verification.** Upload, display, persistence across reload.

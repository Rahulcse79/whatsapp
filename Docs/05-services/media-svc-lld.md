# LLD — media-svc

| Doc | Upload orchestration, quota, GC |
|---|---|
| Status | v1.0 · Go; stateless ×2 pods; owns `media_objects` + MinIO admin |

## 1. Responsibilities

Presigned multipart upload orchestration; completion verification (size, content hash); quota/rate enforcement; download URL minting; GIF search proxy (online profiles); GC of unreferenced/expired objects; encrypted-backup endpoints. **Blobs never transit this service** — clients ↔ MinIO directly via presigned URLs (bandwidth isolation, HLD §9).

## 2. Upload session state machine

```
POST /uploads → PENDING (media_objects.upload_state, expires 24 h)
  parts uploaded directly to MinIO (multipart)
POST /complete → verify ListParts sizes + content_hash (S3 ETag composite or full GET-hash ≤ 4 MB)
  → COMPLETE (refcount 0)
message referencing it accepted (chat context, gRPC) → refcount++
24 h PENDING with no complete → GC abort-multipart + row delete
```

Verification note: composite multipart ETags ≠ content hash → for ≤ 4 MB single-part we hash-verify server-side via ranged GET; for multipart we require client per-part checksums (`x-amz-checksum-sha256`) enforced by MinIO — cheap and cryptographically sufficient (content is ciphertext; the hash binds envelope↔blob).

## 3. Quota model

| Limit | Value | Enforced |
|---|---|---|
| Per file | 25 MB | at create + MinIO policy |
| Create-upload rate | 30/hr/user | GCRA |
| Per-user total live storage | 2 GB default (flag-tunable) | gRPC quota check → core-api-owned counter (single writer) |
| Backup | 1 active, size per profile | create path |

## 4. GC job (leader-elected singleton, K8s Lease)

Hourly sweep: `refcount = 0 AND (expired OR pending-stale)` → MinIO delete → row delete → `media.orphaned` event. Story blobs additionally covered by 24 h MinIO ILM rule (backstop — defense in depth for the privacy promise). Refcount decrements arrive via `media.lifecycle` (e.g., delete-for-everyone with media, account purge).

## 5. Failure modes

| Failure | Behavior |
|---|---|
| MinIO node down (EC 2+2) | Presigns keep working; write throughput degrades; alert |
| Complete-verify mismatch | Reject `VALIDATION_HASH_MISMATCH`; client re-uploads; no row committed |
| GC crash mid-sweep | Idempotent deletes; lease passes; resumes next tick |
| Quota gRPC unavailable | Fail closed on create-upload (`TRANSIENT_UNAVAILABLE`) — uploads are retryable, quota escapes are not |

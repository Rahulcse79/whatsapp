# Media edge cache (T15.04)

A reference CDN in front of MinIO, so media downloads are served from a cache
instead of hitting object storage for every fetch, and so video can start
playing before it has fully downloaded.

## Why this is safe

Media blobs are **E2EE ciphertext**. An intermediate cache — this nginx, or a
managed CDN in front of it — never sees plaintext, and cannot, because the file
key never leaves the clients. Completed objects are also immutable, which makes
them ideal cache entries: long TTLs with no invalidation problem.

## How authorisation works

`media-svc` mints a signed URL per download request:

```
<WA_CDN_BASE_URL>/<object key>?e=<unix expiry>&s=<token>
token = base64url( HMAC-SHA256(secret, "<object key>\n<expiry>") )   # unpadded
```

The signer is `server/internal/media/domain/cdn.go`; the edge verifies the same
construction in `verify.js`. Both sides are pinned to a shared test vector
(`TestKnownVector`) because a silent drift on either side would 403 every media
fetch.

The token grants no more than the MinIO presigned URL it replaces: read one
object, for a bounded time. Expiry is inside the signed string, so a client
cannot extend its own access.

## Enabling it

Set both on `media-svc` — setting only one is a fatal misconfiguration rather
than a silent fallback:

| Variable | Meaning |
|---|---|
| `WA_CDN_BASE_URL` | public base, e.g. `https://cdn.example.com/media` |
| `WA_CDN_SIGNING_KEY` | shared secret, must equal the edge's `CDN_SIGNING_KEY` |

Unset both and `media-svc` mints MinIO presigned GETs exactly as before.

## Origin access — do not skip this

nginx cannot sign S3 SigV4 requests, so the edge cannot authenticate to a
private bucket by itself. Grant the bucket read access **to the edge's network
and nothing else**:

```bash
mc anonymous set-json ./origin-policy.json local/media
```

`origin-policy.json` restricts `s3:GetObject` to RFC-1918 source ranges; narrow
those to your actual edge subnet before using it anywhere real. Confidentiality
does not rest on this policy — the bytes are ciphertext under unguessable keys —
but it is what stops the origin being an open bucket, while the edge token is
what enforces per-object, time-bounded access.

## Progressive / streaming playback

The edge passes `Range` and `If-Range` through to MinIO and caches `206`
responses keyed by range, so a player can seek and start early instead of
waiting for a whole file. `/v1/media/download-urls` now also returns
`size_bytes`, so a client can choose a ranged fetch over a plain download
without a HEAD round-trip.

## Using a managed CDN instead

Two options, both supported by the same signing scheme:

1. **CDN in front of this nginx** — simplest; the CDN caches, nginx enforces
   tokens and shields the origin.
2. **CDN straight onto MinIO** — configure the provider's own token auth with
   the same HMAC construction and secret, and skip this nginx entirely.

## Status

⚠️ **The server side is tested; this edge configuration is not yet exercised
end-to-end.** The Go signer, the CDN delivery adapter and the shared test vector
have unit tests. The nginx + njs configuration has been written against the same
verified vector but has not been run in a container, because the development
host's container runtime was unavailable at the time. Before relying on it:

```bash
docker compose up media-cdn
curl -sI "http://localhost:8088/media/<key>?e=<exp>&s=<sig>"   # expect 200
curl -sI "http://localhost:8088/media/<key>?e=<exp>&s=bogus"   # expect 403
curl -sI -H 'Range: bytes=0-1023' "http://localhost:8088/media/<key>?e=..&s=.."  # expect 206
```

Note the image must provide the njs module (`ngx_http_js_module.so`); the config
loads it in `nginx-main.conf` and nginx will refuse to start if it is missing —
a loud failure, not a silent one.

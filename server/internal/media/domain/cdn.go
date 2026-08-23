package domain

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/url"
	"strconv"
	"strings"
)

// CDN edge signing (T15.04).
//
// Media blobs are E2EE ciphertext, so a cache in front of the origin never sees
// plaintext — fronting MinIO with a CDN is safe by construction. What a CDN
// cannot do is honour a MinIO presigned URL: an S3 SigV4 signature covers the
// Host header, so rewriting the host to the edge invalidates it. Instead the
// edge validates its own token, minted here and verifiable by any edge that can
// compute an HMAC (nginx `secure_link`, Caddy, Varnish, or a CDN's own
// token-auth), keeping this deployable on self-hosted infrastructure.
//
// The token binds exactly two things: the object key and an expiry. It grants
// no more than the presigned URL it replaces — read one object, for a while.

const (
	// ExpiryParam and SigParam are the query parameters the edge reads.
	ExpiryParam = "e"
	SigParam    = "s"
)

var (
	ErrCDNExpired   = errors.New("media: cdn token expired")
	ErrCDNBadSig    = errors.New("media: cdn token signature mismatch")
	ErrCDNMalformed = errors.New("media: cdn token malformed")
)

// SignCDNToken returns the URL-safe token authorising a GET of key until
// expiresUnix. The signed string is "key\nexpiry" — newline-separated so a key
// containing the delimiter cannot be used to shift the boundary.
func SignCDNToken(secret []byte, key string, expiresUnix int64) string {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(key))
	mac.Write([]byte{'\n'})
	mac.Write([]byte(strconv.FormatInt(expiresUnix, 10)))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

// VerifyCDNToken checks a presented token for key at time nowUnix. It is the
// reference implementation of what the edge enforces — kept here so the
// contract is tested in one place, and so a Go-based edge can reuse it.
func VerifyCDNToken(secret []byte, key string, expiresUnix, nowUnix int64, token string) error {
	if token == "" {
		return ErrCDNMalformed
	}
	// Constant-time comparison, and expiry checked only after the signature so
	// an attacker learns nothing from the ordering.
	want := SignCDNToken(secret, key, expiresUnix)
	if !hmac.Equal([]byte(want), []byte(token)) {
		return ErrCDNBadSig
	}
	if nowUnix >= expiresUnix {
		return ErrCDNExpired
	}
	return nil
}

// BuildCDNURL composes the edge URL for an object: baseURL + "/" + key, with the
// expiry and signature attached. baseURL may carry a path prefix (e.g.
// https://cdn.example/media) and a trailing slash is tolerated.
func BuildCDNURL(baseURL, key string, secret []byte, expiresUnix int64) (string, error) {
	if strings.TrimSpace(baseURL) == "" {
		return "", ErrCDNMalformed
	}
	base, err := url.Parse(strings.TrimRight(baseURL, "/"))
	if err != nil || base.Host == "" {
		return "", ErrCDNMalformed
	}
	// Escape each path segment so a key with slashes keeps its structure while
	// anything exotic stays encoded.
	segs := strings.Split(strings.TrimPrefix(key, "/"), "/")
	for i, s := range segs {
		segs[i] = url.PathEscape(s)
	}
	base.Path = base.Path + "/" + strings.Join(segs, "/")

	q := base.Query()
	q.Set(ExpiryParam, strconv.FormatInt(expiresUnix, 10))
	q.Set(SigParam, SignCDNToken(secret, key, expiresUnix))
	base.RawQuery = q.Encode()
	return base.String(), nil
}

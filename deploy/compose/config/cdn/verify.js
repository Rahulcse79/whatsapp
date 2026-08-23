/*
 * Edge token verification (T15.04) — the njs counterpart of
 * server/internal/media/domain/cdn.go.
 *
 * Contract, and it must stay byte-identical to the Go signer:
 *   signed string = <object key> "\n" <unix expiry>
 *   token         = base64url(HMAC-SHA256(secret, signed string)), unpadded
 *   URL           = <base>/<key>?e=<expiry>&s=<token>
 *
 * The object key is the request path with the location prefix removed, i.e. the
 * same string media-svc signed. Getting that mapping wrong is the one way this
 * silently breaks, so it is derived here rather than guessed.
 */

var crypto = require("crypto");

var PREFIX = "/media/"; // must match the nginx location and WA_CDN_BASE_URL's path

/** objectKeyOf recovers the signed key from the request URI. */
function objectKeyOf(r) {
    var uri = r.uri;
    if (uri.indexOf(PREFIX) !== 0) return null;
    // decodeURIComponent because the signer escaped each path segment.
    try {
        return decodeURIComponent(uri.substring(PREFIX.length));
    } catch (e) {
        return null;
    }
}

/** base64url, unpadded — matches Go's base64.RawURLEncoding. */
function b64url(buf) {
    return buf.toString("base64").replace(/\+/g, "-").replace(/\//g, "_").replace(/=+$/, "");
}

/** constant-time-ish comparison: always walks the full length. */
function equals(a, b) {
    if (a.length !== b.length) return false;
    var diff = 0;
    for (var i = 0; i < a.length; i++) diff |= a.charCodeAt(i) ^ b.charCodeAt(i);
    return diff === 0;
}

function checkToken(r) {
    var secret = r.variables.cdn_signing_key;
    if (!secret) {
        r.error("cdn: no signing key configured — refusing every request");
        return "0";
    }

    var key = objectKeyOf(r);
    var exp = r.args[ "e" ];
    var sig = r.args[ "s" ];
    if (key === null || !exp || !sig) return "0";

    // Expiry must be an integer; anything else is malformed, not merely expired.
    if (!/^\d+$/.test(exp)) return "0";

    var want = b64url(crypto.createHmac("sha256", secret).update(key + "\n" + exp).digest());
    if (!equals(want, sig)) return "0";

    // Signature first, then freshness — same ordering as the Go verifier.
    var now = Math.floor(Date.now() / 1000);
    if (now >= parseInt(exp, 10)) return "0";

    return "1";
}

export default { checkToken };

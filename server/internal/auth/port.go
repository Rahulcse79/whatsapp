package auth

// Identity is the authenticated caller attached to every request and frame.
type Identity struct {
	UserID    string
	DeviceID  string
	SessionID string
}

// TokenVerifier is the port other contexts (and ws-gateway via gRPC/pubkey)
// use to authenticate callers. Nothing else of auth's internals crosses the
// context boundary (core-api-lld §1).
type TokenVerifier interface {
	Verify(token string) (Identity, error)
}

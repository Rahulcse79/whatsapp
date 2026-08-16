package auth

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strings"

	"github.com/whatsapp-v2/server/internal/platform/httpx"
)

// LoginObserver is notified after a successful sign-in so an auditor can record
// it (IP, user-agent, device) — the suspicious-login surface (T10.02). Optional
// and best-effort; the call runs in the background and never blocks the login.
type LoginObserver interface {
	Observe(ctx context.Context, userID, deviceID, ip, userAgent string)
}

// Routes mounts the auth REST surface (auth-users-api.md §Auth) on mux. Any
// LoginObserver is notified after a successful verify.
func Routes(mux *http.ServeMux, s *Service, observers ...LoginObserver) {
	mux.HandleFunc("POST /v1/auth/request-otp", handleRequestOTP(s))
	mux.HandleFunc("POST /v1/auth/verify-otp", handleVerifyOTP(s, observers))
	mux.HandleFunc("POST /v1/auth/verify-pin", handleVerifyPIN(s, observers))
	mux.HandleFunc("POST /v1/auth/refresh", handleRefresh(s))
	mux.HandleFunc("POST /v1/auth/logout", handleLogout(s))
	mux.HandleFunc("PUT /v1/auth/pin", handleSetPIN(s))
}

// notifyLogin fires the observers for a completed sign-in (skips the PIN-pending
// first hop). Runs in the background on a detached context so it never adds
// latency to the login response.
func notifyLogin(r *http.Request, observers []LoginObserver, pair TokenPair) {
	if len(observers) == 0 || pair.UserID == "" || pair.RequiresPIN {
		return
	}
	ip, ua := clientIP(r), r.UserAgent()
	for _, o := range observers {
		o := o
		go o.Observe(context.Background(), pair.UserID, pair.DeviceID, ip, ua)
	}
}

func handleRequestOTP(s *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Phone string `json:"phone"`
		}
		if !decodeBody(w, r, &body) {
			return
		}
		out, err := s.RequestOTP(r.Context(), OTPRequest{Handle: body.Phone, IP: clientIP(r)})
		if err != nil {
			writeErr(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, out)
	}
}

type deviceBody struct {
	Platform    string `json:"platform"`
	Name        string `json:"name"`
	IdentityKey string `json:"identity_key"` // base64 public Signal identity key
}

func (d deviceBody) info(w http.ResponseWriter, r *http.Request) (DeviceInfo, bool) {
	if d.IdentityKey == "" {
		// Absent device info is legal on the PIN-gated first hop; the
		// registration path re-validates (VALIDATION_DEVICE).
		return DeviceInfo{}, true
	}
	ik, err := base64.StdEncoding.DecodeString(d.IdentityKey)
	if err != nil {
		httpx.Error(w, r, http.StatusBadRequest, httpx.ErrorObj{
			Code: "VALIDATION_DEVICE", Message: "identity_key must be base64"})
		return DeviceInfo{}, false
	}
	return DeviceInfo{Platform: d.Platform, Name: d.Name, IdentityKey: ik}, true
}

func handleVerifyOTP(s *Service, observers []LoginObserver) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ChallengeID string     `json:"challenge_id"`
			Code        string     `json:"code"`
			Device      deviceBody `json:"device"`
		}
		if !decodeBody(w, r, &body) {
			return
		}
		dev, ok := body.Device.info(w, r)
		if !ok {
			return
		}
		pair, err := s.VerifyOTP(r.Context(), body.ChallengeID, body.Code, dev)
		if err != nil {
			writeErr(w, r, err)
			return
		}
		notifyLogin(r, observers, pair)
		httpx.JSON(w, http.StatusOK, pair)
	}
}

func handleVerifyPIN(s *Service, observers []LoginObserver) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			ChallengeID string     `json:"challenge_id"`
			PIN         string     `json:"pin"`
			Device      deviceBody `json:"device"`
		}
		if !decodeBody(w, r, &body) {
			return
		}
		dev, ok := body.Device.info(w, r)
		if !ok {
			return
		}
		pair, err := s.VerifyPIN(r.Context(), body.ChallengeID, body.PIN, dev)
		if err != nil {
			writeErr(w, r, err)
			return
		}
		notifyLogin(r, observers, pair)
		httpx.JSON(w, http.StatusOK, pair)
	}
}

func handleRefresh(s *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			RefreshToken string `json:"refresh_token"`
		}
		if !decodeBody(w, r, &body) {
			return
		}
		pair, err := s.Refresh(r.Context(), body.RefreshToken)
		if err != nil {
			writeErr(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, pair)
	}
}

func handleLogout(s *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ident, ok := bearer(w, r, s)
		if !ok {
			return
		}
		if err := s.Logout(r.Context(), ident); err != nil {
			writeErr(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func handleSetPIN(s *Service) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ident, ok := bearer(w, r, s)
		if !ok {
			return
		}
		var body struct {
			OldPIN string `json:"old_pin"`
			NewPIN string `json:"new_pin"`
		}
		if !decodeBody(w, r, &body) {
			return
		}
		if err := s.SetPIN(r.Context(), ident, body.OldPIN, body.NewPIN); err != nil {
			writeErr(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

// ── shared plumbing ──────────────────────────────────────────────────────

func decodeBody[T any](w http.ResponseWriter, r *http.Request, dst *T) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, httpx.ErrorObj{
			Code: "VALIDATION_BODY", Message: "malformed JSON body"})
		return false
	}
	return true
}

func bearer(w http.ResponseWriter, r *http.Request, s *Service) (Identity, bool) {
	return BearerIdentity(w, r, s)
}

// BearerIdentity authenticates a request's Authorization: Bearer token via v,
// writing the uniform 401 envelope on failure. Other contexts use this to
// gate their handlers through auth's public port.
func BearerIdentity(w http.ResponseWriter, r *http.Request, v TokenVerifier) (Identity, bool) {
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || tok == "" {
		httpx.Error(w, r, http.StatusUnauthorized, httpx.ErrorObj{
			Code: "AUTH_TOKEN_INVALID", Message: "missing bearer token"})
		return Identity{}, false
	}
	ident, err := v.Verify(tok)
	if err != nil {
		httpx.Error(w, r, http.StatusUnauthorized, httpx.ErrorObj{
			Code: "AUTH_TOKEN_EXPIRED", Message: "invalid or expired token"})
		return Identity{}, false
	}
	return ident, true
}

func writeErr(w http.ResponseWriter, r *http.Request, err error) {
	var ae *Error
	if errors.As(err, &ae) {
		httpx.Error(w, r, ae.Status, httpx.ErrorObj{
			Code: ae.Code, Message: ae.Message, Retryable: ae.Retryable,
			RetryAfterMS: ae.RetryAfter.Milliseconds(),
		})
		return
	}
	httpx.Error(w, r, http.StatusInternalServerError, httpx.ErrorObj{
		Code: "INTERNAL", Message: "internal error", Retryable: true})
}

// clientIP prefers the edge-set X-Forwarded-For (Envoy) over the socket peer.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		if first, _, ok := strings.Cut(xff, ","); ok {
			return strings.TrimSpace(first)
		}
		return strings.TrimSpace(xff)
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

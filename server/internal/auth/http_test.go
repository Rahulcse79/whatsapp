package auth

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/whatsapp-v2/server/internal/platform/httpx"
)

func newHTTPHarness(t *testing.T) (*harness, *httptest.Server) {
	t.Helper()
	h := newHarness(t)
	mux := http.NewServeMux()
	Routes(mux, h.svc)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return h, srv
}

// postJSON returns the status code and body bytes, closing the response body
// itself so callers never juggle it (keeps bodyclose/errcheck happy).
func postJSON(t *testing.T, url string, body any, bearer string) (int, []byte) {
	t.Helper()
	raw, _ := json.Marshal(body)
	req, _ := http.NewRequest(http.MethodPost, url, bytes.NewReader(raw))
	req.Header.Set("Content-Type", "application/json")
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	var buf bytes.Buffer
	_, _ = buf.ReadFrom(resp.Body)
	return resp.StatusCode, buf.Bytes()
}

func wireDevice() map[string]any {
	return map[string]any{
		"platform":     "android",
		"name":         "Pixel",
		"identity_key": base64.StdEncoding.EncodeToString([]byte("pubkey")),
	}
}

func TestHTTP_FullRegistrationFlow(t *testing.T) {
	h, srv := newHTTPHarness(t)

	// 1. request-otp
	status, body := postJSON(t, srv.URL+"/v1/auth/request-otp",
		map[string]string{"phone": "+14155550200"}, "")
	if status != http.StatusOK {
		t.Fatalf("request-otp: %d %s", status, body)
	}
	var ch OTPChallenge
	if err := json.Unmarshal(body, &ch); err != nil || ch.ChallengeID == "" {
		t.Fatalf("bad challenge response: %s", body)
	}

	// 2. verify-otp with a wrong code → uniform error envelope
	status, body = postJSON(t, srv.URL+"/v1/auth/verify-otp", map[string]any{
		"challenge_id": ch.ChallengeID, "code": "000000", "device": wireDevice()}, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("wrong code: status %d", status)
	}
	var envelope httpx.ErrorBody
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.Error.Code != "AUTH_OTP_INVALID" {
		t.Fatalf("bad error envelope: %s", body)
	}

	// 3. verify-otp with the real code → tokens
	code := h.sender.codeFor("+14155550200")
	status, body = postJSON(t, srv.URL+"/v1/auth/verify-otp", map[string]any{
		"challenge_id": ch.ChallengeID, "code": code, "device": wireDevice()}, "")
	if status != http.StatusOK {
		t.Fatalf("verify-otp: %d %s", status, body)
	}
	var pair TokenPair
	if err := json.Unmarshal(body, &pair); err != nil || pair.AccessJWT == "" || pair.RefreshToken == "" {
		t.Fatalf("no tokens: %s", body)
	}

	// 4. refresh rotates
	status, body = postJSON(t, srv.URL+"/v1/auth/refresh",
		map[string]string{"refresh_token": pair.RefreshToken}, "")
	if status != http.StatusOK {
		t.Fatalf("refresh: %d %s", status, body)
	}
	var pair2 TokenPair
	_ = json.Unmarshal(body, &pair2)
	if pair2.RefreshToken == "" || pair2.RefreshToken == pair.RefreshToken {
		t.Fatal("refresh did not rotate")
	}

	// 5. logout with bearer → 204; refresh afterwards fails
	status, _ = postJSON(t, srv.URL+"/v1/auth/logout", struct{}{}, pair2.AccessJWT)
	if status != http.StatusNoContent {
		t.Fatalf("logout: %d", status)
	}
	status, body = postJSON(t, srv.URL+"/v1/auth/refresh",
		map[string]string{"refresh_token": pair2.RefreshToken}, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("refresh after logout: %d %s", status, body)
	}
}

func TestHTTP_RateLimitEnvelope(t *testing.T) {
	h, srv := newHTTPHarness(t)
	h.svc.d.Limiter = denyLimiter{}

	status, body := postJSON(t, srv.URL+"/v1/auth/request-otp",
		map[string]string{"phone": "+14155550201"}, "")
	if status != http.StatusTooManyRequests {
		t.Fatalf("status %d", status)
	}
	var envelope httpx.ErrorBody
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatal(err)
	}
	e := envelope.Error
	if e.Code != "RATE_LIMITED" || !e.Retryable || e.RetryAfterMS != 42000 {
		t.Fatalf("bad 429 envelope: %+v", e)
	}
}

func TestHTTP_MalformedBody(t *testing.T) {
	_, srv := newHTTPHarness(t)
	req, _ := http.NewRequest(http.MethodPost, srv.URL+"/v1/auth/request-otp",
		bytes.NewReader([]byte("{not json")))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("status %d", resp.StatusCode)
	}
}

func TestHTTP_LogoutWithoutBearer(t *testing.T) {
	_, srv := newHTTPHarness(t)
	status, _ := postJSON(t, srv.URL+"/v1/auth/logout", struct{}{}, "")
	if status != http.StatusUnauthorized {
		t.Fatalf("status %d", status)
	}
}

func TestHTTP_MethodNotAllowed(t *testing.T) {
	_, srv := newHTTPHarness(t)
	resp, err := http.Get(fmt.Sprintf("%s/v1/auth/request-otp", srv.URL))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusMethodNotAllowed {
		t.Fatalf("GET on POST route: %d", resp.StatusCode)
	}
}

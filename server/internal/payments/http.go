package payments

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
)

// maxWebhookBody caps a callback. Generous for a JSON event, small enough that
// an unauthenticated endpoint cannot be used to exhaust memory.
const maxWebhookBody = 1 << 20 // 1 MiB

// Routes mounts the payments surface (T15.05).
//
// Note what is absent: there is no endpoint that accepts a card number, expiry
// or CVV, and there never should be. A client starts a checkout here and is
// redirected to the processor's own page to enter payment details.
func Routes(mux *http.ServeMux, s *Service, v auth.TokenVerifier) {
	// ── Buying ──────────────────────────────────────────────────────────────
	mux.HandleFunc("POST /v1/payments/premium", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		var body struct {
			AmountCents int64  `json:"amount_cents"`
			Currency    string `json:"currency"`
		}
		if !decode(w, r, &body) {
			return
		}
		co, err := s.StartPremium(r.Context(), ident, body.AmountCents, body.Currency)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusCreated, co)
	})

	mux.HandleFunc("POST /v1/payments/channels/{id}/subscribe", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		var body struct {
			AmountCents int64  `json:"amount_cents"`
			Currency    string `json:"currency"`
		}
		if !decode(w, r, &body) {
			return
		}
		co, err := s.StartChannelSubscription(r.Context(), ident, r.PathValue("id"), body.AmountCents, body.Currency)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusCreated, co)
	})

	mux.HandleFunc("POST /v1/payments/transfers", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		var body struct {
			ToUserID    string `json:"to_user_id"`
			AmountCents int64  `json:"amount_cents"`
			Currency    string `json:"currency"`
			Memo        string `json:"memo"`
		}
		if !decode(w, r, &body) {
			return
		}
		p, err := s.SendP2P(r.Context(), ident, body.ToUserID, body.AmountCents, body.Currency, body.Memo)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusCreated, p)
	})

	// ── Reading ─────────────────────────────────────────────────────────────
	mux.HandleFunc("GET /v1/payments", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		list, err := s.MyPayments(r.Context(), ident, limit)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"payments": list})
	})

	mux.HandleFunc("GET /v1/payments/subscriptions", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		list, err := s.MySubscriptions(r.Context(), ident)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"subscriptions": list})
	})

	mux.HandleFunc("DELETE /v1/payments/subscriptions/{id}", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		if err := s.CancelSubscription(r.Context(), ident, r.PathValue("id")); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	// ── The provider callback ───────────────────────────────────────────────
	//
	// Deliberately NOT bearer-gated: the caller is the payment processor, not a
	// user. Its authenticity comes from the signature over the raw body, which
	// is why the body is read as bytes and never re-encoded.
	mux.HandleFunc("POST /v1/payments/webhook", func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, maxWebhookBody))
		if err != nil {
			httpx.WriteError(w, r, httpx.Reject(http.StatusBadRequest, "WEBHOOK_BODY", "could not read the callback body"))
			return
		}
		sig := r.Header.Get("X-Payment-Signature")
		if err := s.HandleWebhook(r.Context(), raw, sig); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		// 200 with no body: processors treat any 2xx as delivered and stop
		// retrying, which is what we want once the event is recorded.
		w.WriteHeader(http.StatusOK)
	})
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16))
	if err := dec.Decode(v); err != nil {
		httpx.WriteError(w, r, httpx.Reject(http.StatusBadRequest, "VALIDATION_JSON", "invalid JSON body"))
		return false
	}
	return true
}

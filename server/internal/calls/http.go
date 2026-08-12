package calls

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/calls/domain"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
)

// Routes mounts the call control-plane surface (calls-ptt-api.md §Calls). The
// REST endpoints are bearer-gated; the LiveKit webhook authenticates by its
// signed-body JWT instead (verified by wh).
func Routes(mux *http.ServeMux, s *Service, v auth.TokenVerifier, wh *WebhookVerifier) {
	mux.HandleFunc("POST /v1/calls", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		var body struct {
			CalleeIDs []string `json:"callee_ids"`
			Kind      string   `json:"kind"`
		}
		if !decode(w, r, &body) {
			return
		}
		kind, ok := parseKind(body.Kind)
		if !ok {
			httpx.WriteError(w, r, httpx.Reject(http.StatusBadRequest, "VALIDATION_KIND", "kind must be voice or video"))
			return
		}
		res, err := s.Create(r.Context(), ident, body.CalleeIDs, kind)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusCreated, res)
	})

	mux.HandleFunc("POST /v1/calls/{ring_id}/answer", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		token, err := s.Answer(r.Context(), ident, r.PathValue("ring_id"))
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"join_token": token})
	})

	mux.HandleFunc("POST /v1/calls/{ring_id}/decline", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		if err := s.Decline(r.Context(), ident, r.PathValue("ring_id")); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("POST /v1/calls/{room_id}/rejoin", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		token, err := s.Rejoin(r.Context(), ident, r.PathValue("room_id"))
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"join_token": token})
	})

	mux.HandleFunc("GET /v1/calls/history", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		recs, next, err := s.History(r.Context(), ident, r.URL.Query().Get("cursor"), limit)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"calls": recs, "next_cursor": next})
	})

	// LiveKit → call-ctl webhook. Authenticated by the signed-body JWT, not a
	// bearer token. Always 200 once verified (idempotent handler); a bad
	// signature is 401. The room name in the body is our room_id.
	mux.HandleFunc("POST /v1/calls/webhook", func(w http.ResponseWriter, r *http.Request) {
		raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
		if err != nil {
			httpx.WriteError(w, r, httpx.Reject(http.StatusBadRequest, "VALIDATION_BODY", "unreadable body"))
			return
		}
		ev, err := wh.Verify(r.Header.Get("Authorization"), raw)
		if err != nil {
			httpx.WriteError(w, r, httpx.Reject(http.StatusUnauthorized, "AUTH_WEBHOOK", "invalid webhook signature"))
			return
		}
		if err := s.HandleWebhook(r.Context(), ev); err != nil {
			httpx.WriteError(w, r, httpx.Transient())
			return
		}
		w.WriteHeader(http.StatusOK)
	})
}

func parseKind(s string) (domain.CallKind, bool) {
	switch s {
	case "voice":
		return domain.KindVoice, true
	case "video":
		return domain.KindVideo, true
	default:
		return 0, false
	}
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := dec.Decode(v); err != nil {
		httpx.WriteError(w, r, httpx.Reject(http.StatusBadRequest, "VALIDATION_JSON", "invalid JSON body"))
		return false
	}
	return true
}

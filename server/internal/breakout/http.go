package breakout

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
)

// Routes mounts the advanced live-session surface (T9.03): breakout rooms,
// streaming egress, and recording consent. All bearer-gated; host-only actions
// 404 for non-hosts inside the service.
func Routes(mux *http.ServeMux, s *Service, v auth.TokenVerifier) {
	mux.HandleFunc("POST /v1/live", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		var body struct {
			MainRoom string `json:"main_room"`
		}
		if !decode(w, r, &body) {
			return
		}
		res, err := s.Open(r.Context(), ident, body.MainRoom)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusCreated, res)
	})

	mux.HandleFunc("GET /v1/live/{id}", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		res, err := s.Status(r.Context(), ident, r.PathValue("id"))
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, res)
	})

	mux.HandleFunc("GET /v1/live/{id}/me", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		res, err := s.Me(r.Context(), ident, r.PathValue("id"))
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, res)
	})

	mux.HandleFunc("POST /v1/live/{id}/rooms", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		var body struct {
			Names []string `json:"names"`
		}
		if !decode(w, r, &body) {
			return
		}
		rooms, err := s.CreateRooms(r.Context(), ident, r.PathValue("id"), body.Names)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusCreated, map[string]any{"rooms": rooms})
	})

	mux.HandleFunc("POST /v1/live/{id}/assign", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		var body struct {
			UserID string  `json:"user_id"`
			RoomID *string `json:"room_id"` // null = the main room
		}
		if !decode(w, r, &body) {
			return
		}
		if err := s.Assign(r.Context(), ident, r.PathValue("id"), body.UserID, body.RoomID); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("POST /v1/live/{id}/rooms/close", host204(v, s.CloseRooms))
	mux.HandleFunc("POST /v1/live/{id}/close", host204(v, s.Close))
	mux.HandleFunc("POST /v1/live/{id}/egress/stop", host204(v, s.StopEgress))
	mux.HandleFunc("POST /v1/live/{id}/recording/request", host204(v, s.RequestRecording))
	mux.HandleFunc("POST /v1/live/{id}/recording/start", host204(v, s.StartRecording))
	mux.HandleFunc("POST /v1/live/{id}/recording/stop", host204(v, s.StopRecording))

	mux.HandleFunc("POST /v1/live/{id}/egress/start", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		var body struct {
			Kind string `json:"kind"`
			URL  string `json:"url"`
		}
		if !decode(w, r, &body) {
			return
		}
		if err := s.StartEgress(r.Context(), ident, r.PathValue("id"), body.Kind, body.URL); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("POST /v1/live/{id}/recording/consent", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		var body struct {
			Consent bool `json:"consent"`
		}
		if !decode(w, r, &body) {
			return
		}
		if err := s.Consent(r.Context(), ident, r.PathValue("id"), body.Consent); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

// host204 shares the shape of the no-body, host-only actions that return 204.
func host204(v auth.TokenVerifier, action func(ctx context.Context, ident auth.Identity, sessionID string) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		if err := action(r.Context(), ident, r.PathValue("id")); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
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

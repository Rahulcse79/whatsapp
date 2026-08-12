package stories

import (
	"encoding/json"
	"net/http"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
)

// Routes mounts the stories REST surface (media-stories-api.md §Stories). All
// bearer-gated. Per-story E2EE keys never transit here — the client distributes
// them over WS (MsgSend{STORY_KEY}).
func Routes(mux *http.ServeMux, s *Service, v auth.TokenVerifier) {
	mux.HandleFunc("POST /v1/stories", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		var body struct {
			MediaRef         *string   `json:"media_ref"`
			Kind             string    `json:"kind"`
			AudienceOverride *[]string `json:"audience_override"`
		}
		if !decode(w, r, &body) {
			return
		}
		var override []string
		if body.AudienceOverride != nil {
			override = *body.AudienceOverride
		}
		res, err := s.Post(r.Context(), ident, body.Kind, body.MediaRef, override)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusCreated, res)
	})

	mux.HandleFunc("GET /v1/stories/feed", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		feed, err := s.Feed(r.Context(), ident)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"stories": feed})
	})

	mux.HandleFunc("POST /v1/stories/{id}/view", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		if err := s.View(r.Context(), ident, r.PathValue("id")); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("GET /v1/stories/{id}/viewers", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		viewers, err := s.Viewers(r.Context(), ident, r.PathValue("id"))
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"viewers": viewers})
	})

	mux.HandleFunc("DELETE /v1/stories/{id}", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		if err := s.Delete(r.Context(), ident, r.PathValue("id")); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	if err := dec.Decode(v); err != nil {
		httpx.WriteError(w, r, httpx.Reject(http.StatusBadRequest, "VALIDATION_JSON", "invalid JSON body"))
		return false
	}
	return true
}

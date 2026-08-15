package polls

import (
	"encoding/json"
	"net/http"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
)

// Routes mounts the poll control surface (T6.02). All bearer-gated. Poll content
// (question/options) is E2EE and never transits here — only lifecycle + votes by
// option index.
func Routes(mux *http.ServeMux, s *Service, v auth.TokenVerifier) {
	mux.HandleFunc("POST /v1/polls", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		var body struct {
			ConversationID string `json:"conversation_id"`
			OptionCount    int    `json:"option_count"`
			Multi          bool   `json:"multi"`
			ClosesAtMS     int64  `json:"closes_at_ms"`
		}
		if !decode(w, r, &body) {
			return
		}
		res, err := s.Create(r.Context(), ident, body.ConversationID, body.OptionCount, body.Multi, body.ClosesAtMS)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusCreated, res)
	})

	mux.HandleFunc("POST /v1/polls/{id}/vote", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		var body struct {
			OptionIndices []int `json:"option_indices"`
		}
		if !decode(w, r, &body) {
			return
		}
		if err := s.Vote(r.Context(), ident, r.PathValue("id"), body.OptionIndices); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("POST /v1/polls/{id}/close", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		if err := s.Close(r.Context(), ident, r.PathValue("id")); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("GET /v1/polls/{id}", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		res, err := s.Results(r.Context(), ident, r.PathValue("id"))
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, res)
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

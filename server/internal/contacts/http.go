package contacts

import (
	"encoding/json"
	"net/http"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
)

// Routes mounts the contacts REST surface (auth-users-api.md §Contacts). All
// routes require a bearer token except GET /invite/{token}, which a shared link
// opens before the invitee has an account (the token is the capability).
func Routes(mux *http.ServeMux, s *Service, v auth.TokenVerifier) {
	mux.HandleFunc("POST /v1/contacts/sync", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		var body struct {
			Handles []string `json:"handles"`
		}
		if !decode(w, r, &body) {
			return
		}
		matched, err := s.Sync(r.Context(), ident, body.Handles)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"matched": matched})
	})

	mux.HandleFunc("GET /v1/contacts/search", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		results, err := s.Search(r.Context(), ident, r.URL.Query().Get("u"))
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"results": results})
	})

	mux.HandleFunc("GET /v1/contacts/favorites", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		favs, err := s.ListFavorites(r.Context(), ident)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"favorites": favs})
	})

	mux.HandleFunc("PUT /v1/contacts/favorites/{user_id}", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		if err := s.AddFavorite(r.Context(), ident, r.PathValue("user_id")); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("DELETE /v1/contacts/favorites/{user_id}", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		if err := s.RemoveFavorite(r.Context(), ident, r.PathValue("user_id")); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("POST /v1/contacts/invite", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		link, err := s.CreateInvite(r.Context(), ident)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusCreated, link)
	})

	// Public: a shared invite link resolves before the invitee has an account.
	mux.HandleFunc("GET /v1/contacts/invite/{token}", func(w http.ResponseWriter, r *http.Request) {
		info, err := s.ResolveInvite(r.Context(), r.PathValue("token"))
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, info)
	})

	mux.HandleFunc("DELETE /v1/contacts/invite/{token}", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		if err := s.RevokeInvite(r.Context(), ident, r.PathValue("token")); err != nil {
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

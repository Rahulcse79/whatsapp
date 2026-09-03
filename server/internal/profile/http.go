package profile

import (
	"encoding/json"
	"net/http"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
	"github.com/whatsapp-v2/server/internal/platform/id"
)

// Routes mounts the self-profile, privacy, public-profile, and block-list REST
// surface (FR-USER-01..03). All routes are bearer-gated.
func Routes(mux *http.ServeMux, s *Service, v auth.TokenVerifier) {
	mux.HandleFunc("GET /v1/me", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		p, err := s.Get(r.Context(), ident.UserID)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, p)
	})

	mux.HandleFunc("PUT /v1/me", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		// EVERY field is a POINTER so absent means "leave unchanged". This
		// endpoint is a partial update: the client edits the profile form and
		// the picture through separate calls, and an avatar-only request must
		// not blank the display name and about — which is exactly what happened
		// when the text fields were plain strings.
		var body struct {
			DisplayName *string `json:"display_name"`
			Username    *string `json:"username"`
			About       *string `json:"about"`
			AvatarRef   *string `json:"avatar_ref"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			httpx.Error(w, r, http.StatusBadRequest, httpx.ErrorObj{Code: "VALIDATION_BODY", Message: "invalid JSON body"})
			return
		}

		if body.DisplayName != nil || body.Username != nil || body.About != nil {
			// Fill absent fields from what is stored so a partial edit does not
			// blank the rest.
			cur, err := s.Get(r.Context(), ident.UserID)
			if err != nil {
				httpx.WriteError(w, r, err)
				return
			}
			if err := s.Update(r.Context(), ident.UserID,
				strOr(body.DisplayName, cur.DisplayName),
				strOr(body.Username, cur.Username),
				strOr(body.About, cur.About),
			); err != nil {
				httpx.WriteError(w, r, err)
				return
			}
		}

		if body.AvatarRef != nil {
			if err := s.SetAvatar(r.Context(), ident.UserID, *body.AvatarRef); err != nil {
				httpx.WriteError(w, r, err)
				return
			}
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("PUT /v1/me/privacy", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		var body struct {
			Privacy map[string]string `json:"privacy"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			httpx.Error(w, r, http.StatusBadRequest, httpx.ErrorObj{Code: "VALIDATION_BODY", Message: "invalid JSON body"})
			return
		}
		if err := s.SetPrivacy(r.Context(), ident.UserID, body.Privacy); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("GET /v1/users/{id}", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := auth.BearerIdentity(w, r, v); !ok {
			return
		}
		uid := r.PathValue("id")
		if _, err := id.Parse(uid); err != nil {
			httpx.WriteError(w, r, httpx.Reject(http.StatusBadRequest, "VALIDATION_ID", "id must be a UUID"))
			return
		}
		p, err := s.Public(r.Context(), uid)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, p)
	})

	mux.HandleFunc("GET /v1/blocks", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		ids, err := s.Blocked(r.Context(), ident.UserID)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"blocked": ids})
	})

	mux.HandleFunc("POST /v1/blocks", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		var body struct {
			UserID string `json:"user_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.UserID == "" {
			httpx.Error(w, r, http.StatusBadRequest, httpx.ErrorObj{Code: "VALIDATION_BODY", Message: "user_id required"})
			return
		}
		if _, err := id.Parse(body.UserID); err != nil {
			httpx.WriteError(w, r, httpx.Reject(http.StatusBadRequest, "VALIDATION_ID", "user_id must be a UUID"))
			return
		}
		if err := s.Block(r.Context(), ident.UserID, body.UserID); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("DELETE /v1/blocks/{id}", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		if err := s.Unblock(r.Context(), ident.UserID, r.PathValue("id")); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

// strOr returns the supplied value, or the current one when the field was
// absent from a partial update.
func strOr(v *string, current string) string {
	if v != nil {
		return *v
	}
	return current
}

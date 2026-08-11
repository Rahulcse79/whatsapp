package groups

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/groups/domain"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
)

// Routes mounts the groups REST surface (messaging-groups-api.md) on mux. Every
// route requires a bearer token; permission is enforced per operation.
func Routes(mux *http.ServeMux, s *Service, v auth.TokenVerifier) {
	mux.HandleFunc("POST /v1/groups", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		var body struct {
			Name        string   `json:"name"`
			Description string   `json:"description"`
			MemberIDs   []string `json:"member_ids"`
		}
		if !decode(w, r, &body) {
			return
		}
		g, err := s.Create(r.Context(), ident, body.Name, body.Description, body.MemberIDs)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusCreated, map[string]any{"group": g, "conversation_id": g.ID})
	})

	mux.HandleFunc("GET /v1/groups/{id}", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		g, err := s.Get(r.Context(), ident, r.PathValue("id"))
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, g)
	})

	mux.HandleFunc("PATCH /v1/groups/{id}", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		var body struct {
			Name        *string `json:"name"`
			Description *string `json:"description"`
			AvatarRef   *string `json:"avatar_ref"`
		}
		if !decode(w, r, &body) {
			return
		}
		if err := s.UpdateInfo(r.Context(), ident, r.PathValue("id"), body.Name, body.Description, body.AvatarRef); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("PUT /v1/groups/{id}/settings", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		var st domain.Settings
		if !decode(w, r, &st) {
			return
		}
		if err := s.SetSettings(r.Context(), ident, r.PathValue("id"), st); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("GET /v1/groups/{id}/members", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		members, next, err := s.ListMembers(r.Context(), ident, r.PathValue("id"), r.URL.Query().Get("cursor"), limit)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"members": members, "next_cursor": next})
	})

	mux.HandleFunc("POST /v1/groups/{id}/members", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		var body struct {
			UserIDs []string `json:"user_ids"`
		}
		if !decode(w, r, &body) {
			return
		}
		added, err := s.AddMembers(r.Context(), ident, r.PathValue("id"), body.UserIDs)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"added": added})
	})

	mux.HandleFunc("DELETE /v1/groups/{id}/members/{uid}", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		if err := s.RemoveMember(r.Context(), ident, r.PathValue("id"), r.PathValue("uid")); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("PUT /v1/groups/{id}/members/{uid}/role", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		var body struct {
			Role int16 `json:"role"`
		}
		if !decode(w, r, &body) {
			return
		}
		if err := s.SetRole(r.Context(), ident, r.PathValue("id"), r.PathValue("uid"), domain.Role(body.Role)); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("POST /v1/groups/{id}/leave", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		if err := s.Leave(r.Context(), ident, r.PathValue("id")); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("DELETE /v1/groups/{id}", func(w http.ResponseWriter, r *http.Request) {
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

	mux.HandleFunc("POST /v1/groups/{id}/invite-links", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		var body struct {
			ExpiresAt *time.Time `json:"expires_at"`
			MaxUses   *int       `json:"max_uses"`
		}
		if !decode(w, r, &body) {
			return
		}
		res, err := s.CreateInviteLink(r.Context(), ident, r.PathValue("id"), body.ExpiresAt, body.MaxUses)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusCreated, res)
	})

	mux.HandleFunc("DELETE /v1/invite-links/{token}", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		if err := s.RevokeInviteLink(r.Context(), ident, r.PathValue("token")); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("POST /v1/groups/join", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		var body struct {
			Token string `json:"token"`
		}
		if !decode(w, r, &body) {
			return
		}
		g, err := s.Join(r.Context(), ident, body.Token)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"group": g})
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

package communities

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
)

// Routes mounts the communities surface (T8.01). All bearer-gated. Discover/
// search are literal segments — Go 1.22's ServeMux prefers them over the
// {id} wildcard at the same position, so they coexist without a conflict panic.
func Routes(mux *http.ServeMux, s *Service, v auth.TokenVerifier) {
	mux.HandleFunc("POST /v1/communities", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		var body struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Kind        string `json:"kind"`
		}
		if !decode(w, r, &body) {
			return
		}
		res, err := s.Create(r.Context(), ident, body.Name, body.Description, body.Kind)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusCreated, res)
	})

	mux.HandleFunc("GET /v1/communities/discover", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := auth.BearerIdentity(w, r, v); !ok {
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		out, err := s.Discover(r.Context(), limit)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"communities": out})
	})

	mux.HandleFunc("GET /v1/communities/search", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := auth.BearerIdentity(w, r, v); !ok {
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		out, err := s.Search(r.Context(), r.URL.Query().Get("q"), limit)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"communities": out})
	})

	mux.HandleFunc("GET /v1/communities/{id}", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		view, err := s.Get(r.Context(), ident, r.PathValue("id"))
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, view)
	})

	mux.HandleFunc("DELETE /v1/communities/{id}", func(w http.ResponseWriter, r *http.Request) {
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

	mux.HandleFunc("POST /v1/communities/{id}/join", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		if err := s.Join(r.Context(), ident, r.PathValue("id")); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("POST /v1/communities/{id}/leave", func(w http.ResponseWriter, r *http.Request) {
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

	mux.HandleFunc("GET /v1/communities/{id}/members", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		ms, err := s.Members(r.Context(), ident, r.PathValue("id"))
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		out := make([]map[string]any, len(ms))
		for i, m := range ms {
			out[i] = map[string]any{"user_id": m.UserID, "role": m.Role.String()}
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"members": out})
	})

	mux.HandleFunc("PUT /v1/communities/{id}/members/{user_id}/role", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		var body struct {
			Role string `json:"role"`
		}
		if !decode(w, r, &body) {
			return
		}
		if err := s.SetRole(r.Context(), ident, r.PathValue("id"), r.PathValue("user_id"), body.Role); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("DELETE /v1/communities/{id}/members/{user_id}", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		if err := s.RemoveMember(r.Context(), ident, r.PathValue("id"), r.PathValue("user_id")); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("GET /v1/communities/{id}/groups", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		gs, err := s.Groups(r.Context(), ident, r.PathValue("id"))
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"group_ids": gs})
	})

	mux.HandleFunc("POST /v1/communities/{id}/groups", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		var body struct {
			GroupID string `json:"group_id"`
		}
		if !decode(w, r, &body) {
			return
		}
		if err := s.AddGroup(r.Context(), ident, r.PathValue("id"), body.GroupID); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("DELETE /v1/communities/{id}/groups/{group_id}", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		if err := s.RemoveGroup(r.Context(), ident, r.PathValue("id"), r.PathValue("group_id")); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("GET /v1/communities/{id}/events", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		es, err := s.Events(r.Context(), ident, r.PathValue("id"))
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"events": es})
	})

	mux.HandleFunc("POST /v1/communities/{id}/events", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		var body struct {
			Title       string `json:"title"`
			Description string `json:"description"`
			StartsAtMS  int64  `json:"starts_at_ms"`
		}
		if !decode(w, r, &body) {
			return
		}
		ev, err := s.CreateEvent(r.Context(), ident, r.PathValue("id"), body.Title, body.Description, body.StartsAtMS)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusCreated, ev)
	})

	mux.HandleFunc("DELETE /v1/communities/{id}/events/{event_id}", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		if err := s.DeleteEvent(r.Context(), ident, r.PathValue("id"), r.PathValue("event_id")); err != nil {
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

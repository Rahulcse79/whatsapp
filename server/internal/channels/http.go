package channels

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
)

// Routes mounts the channels REST surface under /v1/channels. All bearer-gated.
// Channel content is server-visible (broadcast plane); access is by kind +
// membership, enforced in the service.
func Routes(mux *http.ServeMux, s *Service, v auth.TokenVerifier) {
	// ── discovery / search (before {id} so the literal paths win) ──
	mux.HandleFunc("GET /v1/channels/discover", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := auth.BearerIdentity(w, r, v); !ok {
			return
		}
		out, err := s.Discover(r.Context(), limit(r))
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"channels": out})
	})

	mux.HandleFunc("GET /v1/channels/search", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := auth.BearerIdentity(w, r, v); !ok {
			return
		}
		out, err := s.Search(r.Context(), r.URL.Query().Get("q"), limit(r))
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"channels": out})
	})

	// ── channel CRUD ──
	mux.HandleFunc("POST /v1/channels", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		var body struct {
			Handle      string `json:"handle"`
			Name        string `json:"name"`
			Description string `json:"description"`
			Kind        string `json:"kind"`
		}
		if !decode(w, r, &body) {
			return
		}
		c, err := s.Create(r.Context(), ident, body.Handle, body.Name, body.Description, body.Kind)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusCreated, c)
	})

	mux.HandleFunc("GET /v1/channels/{id}", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		c, err := s.Get(r.Context(), ident, r.PathValue("id"))
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, c)
	})

	mux.HandleFunc("PATCH /v1/channels/{id}", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		var body struct {
			Name        *string `json:"name"`
			Description *string `json:"description"`
		}
		if !decode(w, r, &body) {
			return
		}
		if err := s.Update(r.Context(), ident, r.PathValue("id"), body.Name, body.Description); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("DELETE /v1/channels/{id}", func(w http.ResponseWriter, r *http.Request) {
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

	// ── membership ──
	mux.HandleFunc("POST /v1/channels/{id}/follow", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		if err := s.Follow(r.Context(), ident, r.PathValue("id")); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("DELETE /v1/channels/{id}/follow", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		if err := s.Unfollow(r.Context(), ident, r.PathValue("id")); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("GET /v1/channels/{id}/members", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		out, err := s.Members(r.Context(), ident, r.PathValue("id"), limit(r))
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"members": out})
	})

	mux.HandleFunc("PUT /v1/channels/{id}/members/{uid}/role", func(w http.ResponseWriter, r *http.Request) {
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
		if err := s.SetRole(r.Context(), ident, r.PathValue("id"), r.PathValue("uid"), body.Role); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	// ── posts ──
	mux.HandleFunc("POST /v1/channels/{id}/posts", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		var body struct {
			Body        string  `json:"body"`
			MediaRef    *string `json:"media_ref"`
			PublishAtMS int64   `json:"publish_at_ms"`
		}
		if !decode(w, r, &body) {
			return
		}
		p, err := s.CreatePost(r.Context(), ident, r.PathValue("id"), body.Body, body.MediaRef, body.PublishAtMS)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusCreated, p)
	})

	mux.HandleFunc("GET /v1/channels/{id}/posts", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		out, err := s.Posts(r.Context(), ident, r.PathValue("id"), limit(r))
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"posts": out})
	})

	mux.HandleFunc("DELETE /v1/channel-posts/{postId}", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		if err := s.DeletePost(r.Context(), ident, r.PathValue("postId")); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	// ── reactions ──
	mux.HandleFunc("POST /v1/channel-posts/{postId}/react", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		var body struct {
			Emoji string `json:"emoji"`
			On    *bool  `json:"on"`
		}
		if !decode(w, r, &body) {
			return
		}
		on := body.On == nil || *body.On
		if err := s.React(r.Context(), ident, r.PathValue("postId"), body.Emoji, on); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	// ── comments ──
	mux.HandleFunc("POST /v1/channel-posts/{postId}/comments", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		var body struct {
			Body string `json:"body"`
		}
		if !decode(w, r, &body) {
			return
		}
		c, err := s.Comment(r.Context(), ident, r.PathValue("postId"), body.Body)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusCreated, c)
	})

	mux.HandleFunc("GET /v1/channel-posts/{postId}/comments", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		out, err := s.Comments(r.Context(), ident, r.PathValue("postId"), limit(r))
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"comments": out})
	})

	mux.HandleFunc("DELETE /v1/channel-comments/{commentId}", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		if err := s.DeleteComment(r.Context(), ident, r.PathValue("commentId")); err != nil {
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

func limit(r *http.Request) int {
	n, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	return n
}

package webinar

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
)

// Routes mounts the webinar/live-mode surface (T9.02). All bearer-gated.
func Routes(mux *http.ServeMux, s *Service, v auth.TokenVerifier) {
	mux.HandleFunc("POST /v1/webinars", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		var body struct {
			Title string `json:"title"`
		}
		if !decode(w, r, &body) {
			return
		}
		res, err := s.Create(r.Context(), ident, body.Title)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusCreated, res)
	})

	mux.HandleFunc("POST /v1/webinars/{id}/join", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		res, err := s.Join(r.Context(), ident, r.PathValue("id"))
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, res)
	})

	mux.HandleFunc("GET /v1/webinars/{id}/me", func(w http.ResponseWriter, r *http.Request) {
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

	mux.HandleFunc("POST /v1/webinars/{id}/leave", func(w http.ResponseWriter, r *http.Request) {
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

	mux.HandleFunc("GET /v1/webinars/{id}/roster", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		roster, err := s.Roster(r.Context(), ident, r.PathValue("id"))
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"roster": roster})
	})

	mux.HandleFunc("POST /v1/webinars/{id}/admit", userTargetHandler(v, s.Admit))
	mux.HandleFunc("POST /v1/webinars/{id}/deny", userTargetHandler(v, s.Deny))

	mux.HandleFunc("POST /v1/webinars/{id}/hand", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		var body struct {
			Raised bool `json:"raised"`
		}
		if !decode(w, r, &body) {
			return
		}
		if err := s.SetHand(r.Context(), ident, r.PathValue("id"), body.Raised); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("PUT /v1/webinars/{id}/participants/{user_id}/role", func(w http.ResponseWriter, r *http.Request) {
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

	mux.HandleFunc("POST /v1/webinars/{id}/end", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		if err := s.End(r.Context(), ident, r.PathValue("id")); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	// ── Q&A ──
	mux.HandleFunc("GET /v1/webinars/{id}/questions", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		qs, err := s.Questions(r.Context(), ident, r.PathValue("id"))
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"questions": qs})
	})

	mux.HandleFunc("POST /v1/webinars/{id}/questions", func(w http.ResponseWriter, r *http.Request) {
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
		q, err := s.AskQuestion(r.Context(), ident, r.PathValue("id"), body.Body)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusCreated, q)
	})

	mux.HandleFunc("POST /v1/webinars/{id}/questions/{qid}/upvote", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		if err := s.UpvoteQuestion(r.Context(), ident, r.PathValue("id"), r.PathValue("qid")); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("POST /v1/webinars/{id}/questions/{qid}/answer", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		if err := s.AnswerQuestion(r.Context(), ident, r.PathValue("id"), r.PathValue("qid")); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})
}

// userTargetHandler shares the admit/deny shape (body {user_id} + host action).
func userTargetHandler(v auth.TokenVerifier, action func(ctx context.Context, ident auth.Identity, webinarID, userID string) error) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		var body struct {
			UserID string `json:"user_id"`
		}
		if !decode(w, r, &body) {
			return
		}
		if err := action(r.Context(), ident, r.PathValue("id"), body.UserID); err != nil {
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

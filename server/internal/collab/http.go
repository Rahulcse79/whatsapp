package collab

import (
	"encoding/json"
	"net/http"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
)

// Routes mounts the collaboration surface (T12.01). All bearer-gated; the service
// gates on conversation membership.
func Routes(mux *http.ServeMux, s *Service, v auth.TokenVerifier) {
	// ── notes ──
	mux.HandleFunc("GET /v1/conversations/{id}/notes", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		notes, err := s.Notes(r.Context(), ident, r.PathValue("id"))
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"notes": notes})
	})

	mux.HandleFunc("POST /v1/conversations/{id}/notes", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		var body struct {
			Title string `json:"title"`
			Body  string `json:"body"`
		}
		if !decode(w, r, &body) {
			return
		}
		nv, err := s.CreateNote(r.Context(), ident, r.PathValue("id"), body.Title, body.Body)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusCreated, nv)
	})

	mux.HandleFunc("PUT /v1/notes/{noteId}", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		var body struct {
			Title       string `json:"title"`
			Body        string `json:"body"`
			BaseVersion int    `json:"base_version"`
		}
		if !decode(w, r, &body) {
			return
		}
		nv, err := s.UpdateNote(r.Context(), ident, r.PathValue("noteId"), body.Title, body.Body, body.BaseVersion)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, nv)
	})

	mux.HandleFunc("GET /v1/notes/{noteId}/revisions", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		revs, err := s.Revisions(r.Context(), ident, r.PathValue("noteId"))
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"revisions": revs})
	})

	// ── approvals ──
	mux.HandleFunc("POST /v1/notes/{noteId}/approval/request", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		if err := s.RequestApproval(r.Context(), ident, r.PathValue("noteId")); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("POST /v1/notes/{noteId}/approval/decide", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		var body struct {
			Approve bool `json:"approve"`
		}
		if !decode(w, r, &body) {
			return
		}
		if err := s.DecideApproval(r.Context(), ident, r.PathValue("noteId"), body.Approve); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	// ── comments ──
	mux.HandleFunc("GET /v1/notes/{noteId}/comments", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		cs, err := s.Comments(r.Context(), ident, r.PathValue("noteId"))
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"comments": cs})
	})

	mux.HandleFunc("POST /v1/notes/{noteId}/comments", func(w http.ResponseWriter, r *http.Request) {
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
		cv, err := s.AddComment(r.Context(), ident, r.PathValue("noteId"), body.Body)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusCreated, cv)
	})

	// ── tasks ──
	mux.HandleFunc("GET /v1/conversations/{id}/tasks", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		tasks, err := s.Tasks(r.Context(), ident, r.PathValue("id"))
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"tasks": tasks})
	})

	mux.HandleFunc("POST /v1/conversations/{id}/tasks", func(w http.ResponseWriter, r *http.Request) {
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
		tv, err := s.CreateTask(r.Context(), ident, r.PathValue("id"), body.Title)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusCreated, tv)
	})

	mux.HandleFunc("POST /v1/conversations/{id}/tasks/{taskId}/done", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		var body struct {
			Done bool `json:"done"`
		}
		if !decode(w, r, &body) {
			return
		}
		if err := s.ToggleTask(r.Context(), ident, r.PathValue("id"), r.PathValue("taskId"), body.Done); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("PUT /v1/conversations/{id}/tasks/{taskId}/assignee", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		var body struct {
			Assignee *string `json:"assignee"` // null clears the assignee
		}
		if !decode(w, r, &body) {
			return
		}
		if err := s.AssignTask(r.Context(), ident, r.PathValue("id"), r.PathValue("taskId"), body.Assignee); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("DELETE /v1/conversations/{id}/tasks/{taskId}", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		if err := s.DeleteTask(r.Context(), ident, r.PathValue("id"), r.PathValue("taskId")); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	// ── activity timeline ──
	mux.HandleFunc("GET /v1/conversations/{id}/activity", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		acts, err := s.Activity(r.Context(), ident, r.PathValue("id"))
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"activity": acts})
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

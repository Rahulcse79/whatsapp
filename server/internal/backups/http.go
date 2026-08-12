package backups

import (
	"encoding/json"
	"net/http"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/media"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
)

// Routes mounts the encrypted-backup surface (media-stories-api.md §Encrypted
// backups). Bearer-gated; the archive is client-encrypted, so only ciphertext
// refs cross here. POST /complete finalizes the multipart upload (as media
// uploads do); the two documented endpoints are create + restore.
func Routes(mux *http.ServeMux, s *Service, v auth.TokenVerifier) {
	mux.HandleFunc("POST /v1/backups", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		var body struct {
			Size int64 `json:"size"`
		}
		if !decode(w, r, &body) {
			return
		}
		res, err := s.Create(r.Context(), ident, body.Size)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusCreated, res)
	})

	mux.HandleFunc("POST /v1/backups/{id}/complete", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		var body struct {
			PartsEtags []media.PartETag `json:"parts_etags"`
		}
		if !decode(w, r, &body) {
			return
		}
		res, err := s.Complete(r.Context(), ident, r.PathValue("id"), body.PartsEtags)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, res)
	})

	mux.HandleFunc("GET /v1/backups/latest", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		res, err := s.Latest(r.Context(), ident)
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

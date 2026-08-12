package media

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
)

// Routes mounts the media REST surface (media-stories-api.md §Media). Every
// route requires a bearer token; blobs never transit here (presigned URLs only).
func Routes(mux *http.ServeMux, s *Service, v auth.TokenVerifier) {
	mux.HandleFunc("POST /v1/media/uploads", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		var body struct {
			Size        int64  `json:"size"`
			ContentHash string `json:"content_hash"` // base64
			MimeClaimed string `json:"mime_claimed"`
		}
		if !decode(w, r, &body) {
			return
		}
		hash, err := base64.StdEncoding.DecodeString(body.ContentHash)
		if err != nil {
			httpx.WriteError(w, r, httpx.Reject(http.StatusBadRequest, "VALIDATION_HASH", "content_hash must be base64"))
			return
		}
		res, err := s.CreateUpload(r.Context(), ident, body.Size, hash, body.MimeClaimed)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusCreated, res)
	})

	mux.HandleFunc("POST /v1/media/uploads/{id}/presign", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		var body struct {
			MissingParts []int `json:"missing_parts"`
		}
		if !decode(w, r, &body) {
			return
		}
		urls, err := s.PresignMissing(r.Context(), ident, r.PathValue("id"), body.MissingParts)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"part_urls": urls})
	})

	mux.HandleFunc("POST /v1/media/uploads/{id}/complete", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		var body struct {
			PartsEtags []PartETag `json:"parts_etags"`
		}
		if !decode(w, r, &body) {
			return
		}
		mediaID, err := s.CompleteUpload(r.Context(), ident, r.PathValue("id"), body.PartsEtags)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"media_id": mediaID})
	})

	mux.HandleFunc("POST /v1/media/download-urls", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		var body struct {
			ObjectKeys []string `json:"object_keys"`
		}
		if !decode(w, r, &body) {
			return
		}
		urls, err := s.DownloadURLs(r.Context(), ident, body.ObjectKeys)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"urls": urls})
	})
}

// GifRoutes mounts the server-side GIF search proxy (FR-MED-05). Bearer-gated so
// the search is rate-limited per user; the client's IP never reaches the provider.
func GifRoutes(mux *http.ServeMux, g *GifService, v auth.TokenVerifier) {
	mux.HandleFunc("GET /v1/media/gif/search", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit")) // 0 on parse error → service default
		results, err := g.Search(r.Context(), ident, r.URL.Query().Get("q"), limit)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"results": results})
	})
}

// StickerRoutes mounts the sticker-pack catalog + per-user install list. Packs
// are public catalog content (not E2EE media); every route is bearer-gated so
// installs are scoped to the caller.
func StickerRoutes(mux *http.ServeMux, s *StickerService, v auth.TokenVerifier) {
	mux.HandleFunc("GET /v1/media/stickers/packs", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := auth.BearerIdentity(w, r, v); !ok {
			return
		}
		packs, err := s.Packs(r.Context())
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"packs": packs})
	})

	mux.HandleFunc("GET /v1/media/stickers/packs/{id}", func(w http.ResponseWriter, r *http.Request) {
		if _, ok := auth.BearerIdentity(w, r, v); !ok {
			return
		}
		pack, err := s.Pack(r.Context(), r.PathValue("id"))
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, pack)
	})

	mux.HandleFunc("GET /v1/media/stickers/installed", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		packs, err := s.Installed(r.Context(), ident)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"packs": packs})
	})

	mux.HandleFunc("POST /v1/media/stickers/packs/{id}/install", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		if err := s.Install(r.Context(), ident, r.PathValue("id")); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("DELETE /v1/media/stickers/packs/{id}/install", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		if err := s.Uninstall(r.Context(), ident, r.PathValue("id")); err != nil {
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

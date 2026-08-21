package discovery

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/discovery/domain"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
)

// Routes mounts the public discovery search (T13.01). Bearer-gated; every result
// is public metadata (no E2EE content).
func Routes(mux *http.ServeMux, s *Service, v auth.TokenVerifier) {
	mux.HandleFunc("GET /v1/discover", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		q := r.URL.Query().Get("q")
		var kinds []domain.Kind
		for _, t := range strings.Split(r.URL.Query().Get("type"), ",") {
			k := domain.Kind(strings.TrimSpace(t))
			if domain.KindValid(k) {
				kinds = append(kinds, k)
			}
		}
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		results, err := s.Search(r.Context(), ident, q, kinds, limit)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"results": results})
	})
}

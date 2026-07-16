package keys

import (
	"encoding/json"
	"net/http"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
)

// Routes mounts the keys REST surface (auth-users-api.md §Keys) on mux.
// v authenticates callers via auth's public port.
func Routes(mux *http.ServeMux, s *Service, v auth.TokenVerifier) {
	mux.HandleFunc("PUT /v1/keys/prekeys", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		var body struct {
			SignedPrekey   SignedPrekey    `json:"signed_prekey"`
			OneTimePrekeys []OneTimePrekey `json:"one_time_prekeys"`
		}
		r.Body = http.MaxBytesReader(w, r.Body, 256<<10)
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			httpx.Error(w, r, http.StatusBadRequest, httpx.ErrorObj{
				Code: "VALIDATION_BODY", Message: "malformed JSON body"})
			return
		}
		out, err := s.Publish(r.Context(), ident.DeviceID, body.SignedPrekey, body.OneTimePrekeys)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, out)
	})

	mux.HandleFunc("GET /v1/keys/bundle/{user_id}", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		target := r.PathValue("user_id")
		if target == "" {
			httpx.Error(w, r, http.StatusBadRequest, httpx.ErrorObj{
				Code: "VALIDATION_USER_ID", Message: "user_id path segment required"})
			return
		}
		bundles, err := s.FetchBundle(r.Context(), ident.UserID, target)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"devices": bundles})
	})
}

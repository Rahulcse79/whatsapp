package devices

import (
	"encoding/base64"
	"encoding/json"
	"net/http"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
)

// Routes mounts the devices REST surface (auth-users-api.md §Devices) on mux.
// link/init and link/complete are unauthenticated — the link_token is the
// capability; every other route requires a bearer token.
func Routes(mux *http.ServeMux, s *Service, v auth.TokenVerifier) {
	mux.HandleFunc("GET /v1/devices", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		views, err := s.List(r.Context(), ident)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"devices": views})
	})

	mux.HandleFunc("PATCH /v1/devices/{id}", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		var body struct {
			Name string `json:"name"`
		}
		if !decode(w, r, &body) {
			return
		}
		if err := s.Rename(r.Context(), ident, r.PathValue("id"), body.Name); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("DELETE /v1/devices/{id}", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		if err := s.Revoke(r.Context(), ident, r.PathValue("id")); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("POST /v1/devices/link/init", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Platform    string `json:"platform"`
			Name        string `json:"name"`
			IdentityKey string `json:"identity_key"`
		}
		if !decode(w, r, &body) {
			return
		}
		ik, err := base64.StdEncoding.DecodeString(body.IdentityKey)
		if err != nil {
			httpx.Error(w, r, http.StatusBadRequest, httpx.ErrorObj{
				Code: "VALIDATION_DEVICE", Message: "identity_key must be base64"})
			return
		}
		out, err := s.LinkInit(r.Context(), LinkInitRequest{
			Platform: body.Platform, Name: body.Name, IdentityKey: ik})
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, out)
	})

	mux.HandleFunc("POST /v1/devices/link/approve", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		var body struct {
			LinkToken string `json:"link_token"`
			Cert      string `json:"cert"` // base64 primary signature over identity_key
		}
		if !decode(w, r, &body) {
			return
		}
		cert, err := base64.StdEncoding.DecodeString(body.Cert)
		if err != nil {
			httpx.Error(w, r, http.StatusBadRequest, httpx.ErrorObj{
				Code: "VALIDATION_CERT", Message: "cert must be base64"})
			return
		}
		if err := s.LinkApprove(r.Context(), ident, body.LinkToken, cert); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("POST /v1/devices/link/complete", func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			LinkToken string `json:"link_token"`
		}
		if !decode(w, r, &body) {
			return
		}
		out, err := s.LinkComplete(r.Context(), body.LinkToken)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, out)
	})
}

func decode[T any](w http.ResponseWriter, r *http.Request, dst *T) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		httpx.Error(w, r, http.StatusBadRequest, httpx.ErrorObj{
			Code: "VALIDATION_BODY", Message: "malformed JSON body"})
		return false
	}
	return true
}

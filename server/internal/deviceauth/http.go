package deviceauth

import (
	"encoding/json"
	"net/http"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
)

// Routes mounts the device-auth surface (T10.02): passkey ceremonies + the
// recent-logins audit. All bearer-gated.
func Routes(mux *http.ServeMux, s *Service, v auth.TokenVerifier) {
	mux.HandleFunc("POST /v1/auth/passkeys/register/begin", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		opts, err := s.BeginRegistration(r.Context(), ident)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, opts)
	})

	mux.HandleFunc("POST /v1/auth/passkeys/register/finish", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		var body struct {
			CredentialID   string `json:"credential_id"`
			Alg            int    `json:"alg"`
			PublicKey      string `json:"public_key"`
			ClientDataJSON string `json:"client_data_json"`
			Name           string `json:"name"`
		}
		if !decode(w, r, &body) {
			return
		}
		err := s.FinishRegistration(r.Context(), ident, FinishRegistrationInput{
			CredentialID: body.CredentialID, Alg: body.Alg, PublicKeyB64: body.PublicKey,
			ClientDataJSON: body.ClientDataJSON, Name: body.Name,
		})
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("POST /v1/auth/passkeys/login/begin", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		opts, err := s.BeginLogin(r.Context(), ident)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, opts)
	})

	mux.HandleFunc("POST /v1/auth/passkeys/login/finish", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		var body struct {
			CredentialID      string `json:"credential_id"`
			AuthenticatorData string `json:"authenticator_data"`
			ClientDataJSON    string `json:"client_data_json"`
			Signature         string `json:"signature"`
		}
		if !decode(w, r, &body) {
			return
		}
		err := s.FinishLogin(r.Context(), ident, FinishLoginInput{
			CredentialID: body.CredentialID, AuthenticatorData: body.AuthenticatorData,
			ClientDataJSON: body.ClientDataJSON, Signature: body.Signature,
		})
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]bool{"ok": true})
	})

	mux.HandleFunc("GET /v1/auth/passkeys", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		list, err := s.ListPasskeys(r.Context(), ident)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"passkeys": list})
	})

	mux.HandleFunc("DELETE /v1/auth/passkeys/{id}", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		if err := s.DeletePasskey(r.Context(), ident, r.PathValue("id")); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("GET /v1/auth/logins", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		logins, err := s.RecentLogins(r.Context(), ident)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"logins": logins})
	})
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16))
	if err := dec.Decode(v); err != nil {
		httpx.WriteError(w, r, httpx.Reject(http.StatusBadRequest, "VALIDATION_JSON", "invalid JSON body"))
		return false
	}
	return true
}

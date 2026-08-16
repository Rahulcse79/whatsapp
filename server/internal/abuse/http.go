package abuse

import (
	"encoding/json"
	"net/http"

	"github.com/whatsapp-v2/server/internal/abuse/domain"
	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
)

// Routes mounts the report surface (T10.03). Bearer-gated + rate-limited.
func Routes(mux *http.ServeMux, s *Service, v auth.TokenVerifier) {
	mux.HandleFunc("POST /v1/reports", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		var body struct {
			TargetUserID string `json:"target_user_id"`
			Reason       int16  `json:"reason"`
			Note         string `json:"note"`
			Disclosed    []byte `json:"disclosed_ciphertext"` // base64 (optional, consent only)
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
		if err := dec.Decode(&body); err != nil {
			httpx.WriteError(w, r, httpx.Reject(http.StatusBadRequest, "VALIDATION_JSON", "invalid JSON body"))
			return
		}
		res, err := s.Report(r.Context(), ident, body.TargetUserID, domain.Reason(body.Reason), body.Note, body.Disclosed)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusCreated, res)
	})
}

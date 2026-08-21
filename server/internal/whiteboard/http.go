package whiteboard

import (
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
)

// Routes mounts the whiteboard surface (T12.02). Bearer-gated; the service gates
// on conversation membership.
func Routes(mux *http.ServeMux, s *Service, v auth.TokenVerifier) {
	// Append a batch of board ops.
	mux.HandleFunc("POST /v1/conversations/{id}/board/ops", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		var body struct {
			Ops []json.RawMessage `json:"ops"`
		}
		dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<20))
		if err := dec.Decode(&body); err != nil {
			httpx.WriteError(w, r, httpx.Reject(http.StatusBadRequest, "VALIDATION_JSON", "invalid JSON body"))
			return
		}
		ops := make([]Op, 0, len(body.Ops))
		for _, raw := range body.Ops {
			var hdr struct {
				T   string `json:"t"`
				ID  string `json:"id"`
				Seq int64  `json:"seq"`
			}
			if err := json.Unmarshal(raw, &hdr); err != nil {
				httpx.WriteError(w, r, httpx.Reject(http.StatusBadRequest, "VALIDATION_OP", "malformed op"))
				return
			}
			ops = append(ops, Op{ID: hdr.ID, Kind: hdr.T, Seq: hdr.Seq, Data: raw})
		}
		if err := s.Append(r.Context(), ident, r.PathValue("id"), ops); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	// Sync: ops since a cursor.
	mux.HandleFunc("GET /v1/conversations/{id}/board/ops", func(w http.ResponseWriter, r *http.Request) {
		ident, ok := auth.BearerIdentity(w, r, v)
		if !ok {
			return
		}
		since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
		res, err := s.Sync(r.Context(), ident, r.PathValue("id"), since)
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		if res.Ops == nil {
			res.Ops = []OpView{}
		}
		httpx.JSON(w, http.StatusOK, res)
	})
}

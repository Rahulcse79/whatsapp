package admin

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/whatsapp-v2/server/internal/admin/domain"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
)

// Routes mounts the admin plane's REST surface under /admin/v1. Every route is
// gated by an OIDC ID token (HLD §15.6: SSO); the network-level controls the
// HLD also requires — separate hostname, IP allowlist, hardware-key 2FA — live
// at the edge (Envoy) in front of this, not in application code.
func Routes(mux *http.ServeMux, s *Service) {
	// Report queue.
	mux.HandleFunc("GET /admin/v1/reports", func(w http.ResponseWriter, r *http.Request) {
		admin, ok := authenticate(w, r, s)
		if !ok {
			return
		}
		out, err := s.ListReports(r.Context(), admin, queryLimit(r))
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"reports": out})
	})

	mux.HandleFunc("GET /admin/v1/reports/{id}", func(w http.ResponseWriter, r *http.Request) {
		admin, ok := authenticate(w, r, s)
		if !ok {
			return
		}
		rep, err := s.GetReport(r.Context(), admin, r.PathValue("id"))
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, rep)
	})

	mux.HandleFunc("POST /admin/v1/reports/{id}/resolve", func(w http.ResponseWriter, r *http.Request) {
		admin, ok := authenticate(w, r, s)
		if !ok {
			return
		}
		var body struct {
			Resolution string `json:"resolution"`
			Reason     string `json:"reason"`
		}
		if !decode(w, r, &body) {
			return
		}
		if err := s.ResolveReport(r.Context(), admin, r.PathValue("id"), domain.Resolution(body.Resolution), body.Reason); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	// User metadata search + account actions.
	mux.HandleFunc("GET /admin/v1/users", func(w http.ResponseWriter, r *http.Request) {
		admin, ok := authenticate(w, r, s)
		if !ok {
			return
		}
		out, err := s.SearchUsers(r.Context(), admin, r.URL.Query().Get("q"), queryLimit(r))
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"users": out})
	})

	mux.HandleFunc("GET /admin/v1/users/{id}", func(w http.ResponseWriter, r *http.Request) {
		admin, ok := authenticate(w, r, s)
		if !ok {
			return
		}
		sum, err := s.UserMetadata(r.Context(), admin, r.PathValue("id"))
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, sum)
	})

	mux.HandleFunc("POST /admin/v1/users/{id}/suspend", func(w http.ResponseWriter, r *http.Request) {
		admin, ok := authenticate(w, r, s)
		if !ok {
			return
		}
		reason, ok := decodeReason(w, r)
		if !ok {
			return
		}
		if err := s.SuspendUser(r.Context(), admin, r.PathValue("id"), reason); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	mux.HandleFunc("POST /admin/v1/users/{id}/reactivate", func(w http.ResponseWriter, r *http.Request) {
		admin, ok := authenticate(w, r, s)
		if !ok {
			return
		}
		reason, ok := decodeReason(w, r)
		if !ok {
			return
		}
		if err := s.ReactivateUser(r.Context(), admin, r.PathValue("id"), reason); err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	})

	// Audit review (owner only, enforced in the service).
	mux.HandleFunc("GET /admin/v1/audit", func(w http.ResponseWriter, r *http.Request) {
		admin, ok := authenticate(w, r, s)
		if !ok {
			return
		}
		out, err := s.ListAudit(r.Context(), admin, queryLimit(r))
		if err != nil {
			httpx.WriteError(w, r, err)
			return
		}
		httpx.JSON(w, http.StatusOK, map[string]any{"audit": out})
	})
}

// authenticate resolves the request's OIDC bearer token to an admin Identity,
// writing the uniform error envelope (401 unauthenticated / 403 not-an-admin)
// on failure.
func authenticate(w http.ResponseWriter, r *http.Request, s *Service) (Identity, bool) {
	tok, ok := strings.CutPrefix(r.Header.Get("Authorization"), "Bearer ")
	if !ok || tok == "" {
		httpx.WriteError(w, r, errUnauthorized)
		return Identity{}, false
	}
	admin, err := s.Authenticate(r.Context(), tok)
	if err != nil {
		httpx.WriteError(w, r, err)
		return Identity{}, false
	}
	return admin, true
}

func decode(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16)).Decode(v); err != nil {
		httpx.WriteError(w, r, httpx.Reject(http.StatusBadRequest, "ADMIN_BAD_BODY", "invalid request body"))
		return false
	}
	return true
}

func decodeReason(w http.ResponseWriter, r *http.Request) (string, bool) {
	var body struct {
		Reason string `json:"reason"`
	}
	if !decode(w, r, &body) {
		return "", false
	}
	return body.Reason, true
}

func queryLimit(r *http.Request) int {
	n, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	return n // 0 (or negative) → service default
}

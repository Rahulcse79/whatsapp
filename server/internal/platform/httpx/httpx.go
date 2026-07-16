// Package httpx carries the uniform REST envelope shared by every
// deployable's HTTP surface. Wire contract: Docs/04-api/api-standards.md §2.
package httpx

import (
	"encoding/json"
	"net/http"

	"github.com/whatsapp-v2/server/internal/platform/logging"
)

// ErrorBody is the wire shape of every REST error.
type ErrorBody struct {
	Error ErrorObj `json:"error"`
}

// ErrorObj mirrors whatsapp.common.v1.Error.
type ErrorObj struct {
	Code         string `json:"code"`
	Message      string `json:"message"`
	Retryable    bool   `json:"retryable"`
	RetryAfterMS int64  `json:"retry_after_ms,omitempty"`
	TraceID      string `json:"trace_id,omitempty"`
}

// JSON writes v as the response body with the given status.
func JSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v) // headers are sent; nothing useful left to do on error
}

// Error writes the uniform error envelope, attaching the request trace id.
func Error(w http.ResponseWriter, r *http.Request, status int, e ErrorObj) {
	e.TraceID = logging.TraceID(r.Context())
	JSON(w, status, ErrorBody{Error: e})
}

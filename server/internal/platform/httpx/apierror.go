package httpx

import (
	"errors"
	"net/http"
	"time"
)

// APIError is a domain rejection with its wire mapping. Contexts return it;
// WriteError renders it as the uniform envelope. (auth predates this and
// keeps its own equivalent type — consolidation is a later cleanup.)
type APIError struct {
	Code       string
	Status     int
	Message    string
	Retryable  bool
	RetryAfter time.Duration
}

func (e *APIError) Error() string { return e.Code + ": " + e.Message }

// Reject builds a non-retryable APIError.
func Reject(status int, code, msg string) *APIError {
	return &APIError{Code: code, Status: status, Message: msg}
}

// Transient builds the standard retryable-infrastructure error.
func Transient() *APIError {
	return &APIError{Code: "TRANSIENT_UNAVAILABLE", Status: http.StatusServiceUnavailable,
		Message: "temporarily unavailable", Retryable: true}
}

// WriteError renders err as the uniform envelope. A non-APIError becomes a
// generic 500 so internal details never cross the edge.
func WriteError(w http.ResponseWriter, r *http.Request, err error) {
	var ae *APIError
	if errors.As(err, &ae) {
		Error(w, r, ae.Status, ErrorObj{
			Code: ae.Code, Message: ae.Message, Retryable: ae.Retryable,
			RetryAfterMS: ae.RetryAfter.Milliseconds(),
		})
		return
	}
	Error(w, r, http.StatusInternalServerError, ErrorObj{
		Code: "INTERNAL", Message: "internal error", Retryable: true})
}

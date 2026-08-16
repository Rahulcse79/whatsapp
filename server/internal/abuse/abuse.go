// Package abuse is the anti-abuse control plane (T10.03): it files user reports
// into the trust-and-safety queue (the reports table the admin console drains,
// T4.01) and rate-limits the report path. Spam/phishing/scam detection is
// on-device (E2EE), and block/unblock live in the profile context; here we own
// the report→admin pipeline + its abuse controls.
package abuse

import (
	"context"
	"time"

	"github.com/whatsapp-v2/server/internal/abuse/domain"
	"github.com/whatsapp-v2/server/internal/platform/ratelimit"
)

// Report is a metadata report filed against a user. DisclosedCiphertext is set
// ONLY when the reporter explicitly consented to attach the offending message
// (FR-ADMIN-05) — otherwise the server sees no content.
type Report struct {
	ID                  string
	ReporterID          string
	TargetUserID        string
	Reason              domain.Reason
	Note                string
	DisclosedCiphertext []byte
	CreatedAt           time.Time
}

// FileResult is POST /v1/reports.
type FileResult struct {
	ReportID string `json:"report_id"`
}

// Store files reports and checks the target exists.
type Store interface {
	FileReport(ctx context.Context, r Report) error
	UserExists(ctx context.Context, userID string) (bool, error)
}

// Limiter is the abuse rate-limit port (Valkey GCRA in prod).
type Limiter interface {
	Allow(ctx context.Context, key string, p ratelimit.Params) (ratelimit.Result, error)
}

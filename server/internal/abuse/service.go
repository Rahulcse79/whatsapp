package abuse

import (
	"context"
	"net/http"
	"time"

	"github.com/whatsapp-v2/server/internal/abuse/domain"
	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
	"github.com/whatsapp-v2/server/internal/platform/id"
	"github.com/whatsapp-v2/server/internal/platform/ratelimit"
)

// reportLimit caps report filing at 20/day per reporter (tuned abuse control for
// the report path — stops a bad actor mass-reporting to grief the queue).
var reportLimit = ratelimit.Params{Rate: 20.0 / 86400, Burst: 20}

// Service files trust-and-safety reports into the admin queue.
type Service struct {
	store   Store
	limiter Limiter
	now     func() time.Time
	newID   func() string
}

func NewService(store Store, limiter Limiter) *Service {
	return &Service{store: store, limiter: limiter, now: time.Now, newID: id.New}
}

// Report files a report against targetUserID. disclosed is attached only when the
// reporter consented (FR-ADMIN-05); pass nil otherwise.
func (s *Service) Report(ctx context.Context, ident auth.Identity, targetUserID string, reason domain.Reason, note string, disclosed []byte) (FileResult, error) {
	if err := domain.ValidateReport(reason, note); err != nil {
		return FileResult{}, httpx.Reject(http.StatusBadRequest, "VALIDATION_REPORT", err.Error())
	}
	if targetUserID == "" || targetUserID == ident.UserID {
		return FileResult{}, httpx.Reject(http.StatusBadRequest, "VALIDATION_TARGET", domain.ErrSelf.Error())
	}

	res, err := s.limiter.Allow(ctx, "rl:report:"+ident.UserID, reportLimit)
	if err != nil {
		return FileResult{}, httpx.Transient() // fail closed on limiter outage
	}
	if !res.Allowed {
		return FileResult{}, httpx.Reject(http.StatusTooManyRequests, "RATE_LIMITED", "too many reports; try again later")
	}

	exists, err := s.store.UserExists(ctx, targetUserID)
	if err != nil {
		return FileResult{}, httpx.Transient()
	}
	if !exists {
		return FileResult{}, httpx.Reject(http.StatusNotFound, "USER_NOT_FOUND", "user not found")
	}

	r := Report{
		ID: s.newID(), ReporterID: ident.UserID, TargetUserID: targetUserID,
		Reason: reason, Note: domain.NormalizeNote(note), DisclosedCiphertext: disclosed, CreatedAt: s.now(),
	}
	if err := s.store.FileReport(ctx, r); err != nil {
		return FileResult{}, httpx.Transient()
	}
	return FileResult{ReportID: r.ID}, nil
}

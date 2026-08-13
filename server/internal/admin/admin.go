package admin

// Trust-and-safety + operations plane (HLD §15.6, security-architecture §4).
// Package doc lives in doc.go. It is deliberately narrowed by E2EE: admins
// search users by metadata, view report history, and action reports — they can
// never read a message, view media, or see who talks to whom. There is no God
// mode because the data does not exist server-side. Every mutating action
// writes an append-only audit_log row in the SAME transaction as the mutation.

import (
	"context"
	"time"

	"github.com/whatsapp-v2/server/internal/admin/domain"
)

// Claims is what an OIDC ID token yields after verification: the admin's stable
// subject, their email, and the raw role string from the configured role claim.
type Claims struct {
	Subject string
	Email   string
	Role    string // raw claim value; mapped to domain.Role by the service
}

// Identity is an authenticated admin: a verified OIDC subject plus the RBAC role
// resolved from their token. Handlers receive it and the service gates on Role.
type Identity struct {
	Subject string
	Email   string
	Role    domain.Role
}

// Report is a trust-and-safety report row (metadata only). HasDisclosure is true
// only when the reporter explicitly consented to attach ciphertext (FR-ADMIN-05);
// the admin plane never decrypts it — it is evidence handed to a legal process.
type Report struct {
	ID            string
	ReporterID    string // "" if the reporter was deleted (FK ON DELETE SET NULL)
	TargetUserID  string
	Reason        int16
	Note          string
	State         domain.ReportState
	HasDisclosure bool
	CreatedAt     time.Time
}

// UserSummary is the metadata an admin may see about a user — no content, no
// contact graph. DeviceCount and ReportCount are aggregates, not lists.
type UserSummary struct {
	ID          string
	Username    string
	DisplayName string
	Status      int16 // 0 active | 1 suspended | 2 deleted
	DeviceCount int
	ReportCount int // reports filed AGAINST this user
	CreatedAt   time.Time
}

// AuditEntry is the append-only record of a mutating admin action. The service
// builds it and hands it to the mutating store method, which writes it in the
// same transaction — the mutation cannot land without its audit row.
type AuditEntry struct {
	Actor  string // admin OIDC subject
	Action string // e.g. "report.suspend", "user.reactivate"
	Target string // affected user/report id ("" if none)
	Reason string
}

// AuditRecord is a persisted audit_log row, for the owner-only review surface.
type AuditRecord struct {
	ID     int64
	Actor  string
	Action string
	Target string
	Reason string
	At     time.Time
}

// Verifier validates an OIDC ID token and returns its claims. The gateway hands
// the bearer token straight to Verify; a failure is an authentication failure.
type Verifier interface {
	Verify(ctx context.Context, token string) (Claims, error)
}

// Reports is the report-queue store. Resolve is transactional: it moves the
// report to its final state AND writes the audit row (and, for a suspension,
// flips the target's status) atomically.
type Reports interface {
	ListOpen(ctx context.Context, limit int) ([]Report, error)
	Get(ctx context.Context, id string) (Report, error)
	// Resolve moves report id to state, optionally suspends suspendUser
	// (empty = none), and appends audit — all in one transaction.
	Resolve(ctx context.Context, id string, state domain.ReportState, suspendUser string, audit AuditEntry) error
}

// Users is the admin view over the identity store: metadata search and the
// status mutation, the latter always paired with its audit row in one tx.
type Users interface {
	Search(ctx context.Context, query string, limit int) ([]UserSummary, error)
	Summary(ctx context.Context, userID string) (UserSummary, error)
	// SetStatus updates users.status and appends audit in one transaction.
	SetStatus(ctx context.Context, userID string, status int16, audit AuditEntry) error
}

// Audit is the read side of the append-only log (owner review). Writes happen
// only inside the mutating methods above — never as a standalone call — so an
// action can never be performed without leaving its trace.
type Audit interface {
	List(ctx context.Context, limit int) ([]AuditRecord, error)
}

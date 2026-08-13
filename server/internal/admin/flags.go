package admin

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/whatsapp-v2/server/internal/admin/domain"
	"github.com/whatsapp-v2/server/internal/platform/flags"
	"github.com/whatsapp-v2/server/internal/platform/httpx"
)

// FlagStore is the write side of feature-flag management: an upsert or delete
// paired with its audit_log row in ONE transaction (security-architecture §4),
// so a flag can never change without leaving a trace. The read side is
// flags.Store (evaluation), reused here for listing.
type FlagStore interface {
	UpsertFlag(ctx context.Context, flag string, rules []byte, audit AuditEntry) error
	DeleteFlag(ctx context.Context, flag string, audit AuditEntry) error
}

// FlagConsole is the admin surface over feature flags + kill-switches
// (core-api-lld §5, T4.02). Reading a flag is triage (agent+); changing one is
// an operational action (operator+). Every write appends audit in-tx, then
// busts the shared cache so the change lands immediately on this pod and within
// flags.CacheTTL everywhere else.
type FlagConsole struct {
	read  flags.Store
	write FlagStore
	cache flags.Cache
}

func NewFlagConsole(read flags.Store, write FlagStore, cache flags.Cache) *FlagConsole {
	return &FlagConsole{read: read, write: write, cache: cache}
}

var errFlagName = httpx.Reject(http.StatusBadRequest, "ADMIN_FLAG_NAME", "a flag name is required")

// List returns every stored flag with its rule (agent+).
func (c *FlagConsole) List(ctx context.Context, admin Identity) ([]flags.Named, error) {
	if err := require(admin, domain.RoleAgent); err != nil {
		return nil, err
	}
	out, err := c.read.List(ctx)
	if err != nil {
		return nil, httpx.Transient()
	}
	return out, nil
}

// Set upserts a flag's rule (operator+). Rollout is validated to [0,100]; the
// rule is stored, audited, and the cache busted.
func (c *FlagConsole) Set(ctx context.Context, admin Identity, flag string, rule flags.Rule, reason string) error {
	if err := require(admin, domain.RoleOperator); err != nil {
		return err
	}
	flag = strings.TrimSpace(flag)
	if flag == "" {
		return errFlagName
	}
	if strings.TrimSpace(reason) == "" {
		return errReason
	}
	if rule.Rollout < 0 || rule.Rollout > 100 {
		return httpx.Reject(http.StatusBadRequest, "ADMIN_FLAG_ROLLOUT", "rollout must be between 0 and 100")
	}
	raw, err := json.Marshal(rule)
	if err != nil {
		return httpx.Reject(http.StatusBadRequest, "ADMIN_FLAG_RULE", "invalid rule")
	}
	entry := AuditEntry{Actor: admin.Subject, Action: "flag.set:" + flag, Reason: reason}
	if err := c.write.UpsertFlag(ctx, flag, raw, entry); err != nil {
		return mapNotFound(err)
	}
	_ = c.cache.Del(ctx, flag) // best-effort bust; the TTL is the backstop
	return nil
}

// Delete removes a flag entirely (operator+). A missing flag is a 404.
func (c *FlagConsole) Delete(ctx context.Context, admin Identity, flag, reason string) error {
	if err := require(admin, domain.RoleOperator); err != nil {
		return err
	}
	flag = strings.TrimSpace(flag)
	if flag == "" {
		return errFlagName
	}
	if strings.TrimSpace(reason) == "" {
		return errReason
	}
	entry := AuditEntry{Actor: admin.Subject, Action: "flag.delete:" + flag, Reason: reason}
	if err := c.write.DeleteFlag(ctx, flag, entry); err != nil {
		return mapNotFound(err)
	}
	_ = c.cache.Del(ctx, flag)
	return nil
}

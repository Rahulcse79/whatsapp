// Package adapters implements the groups context's persistence and event ports
// over PostgreSQL and NATS.
package adapters

import (
	"context"
	"encoding/json"
	"log/slog"

	"github.com/nats-io/nats.go"

	"github.com/whatsapp-v2/server/internal/groups/domain"
)

// groupEventsSubject is the per-group ordered subject
// (internal-events-nats.md: group.events.{group_id} on the DOMAIN stream).
func groupEventsSubject(groupID string) string { return "group.events." + groupID }

// NATSEvents publishes ordered group events. Clients rotate Sender Keys on
// membership events; the fan-out cache tracks membership. Best-effort core NATS
// publish — the mutation already committed to PostgreSQL (the source of truth),
// and consumers reconcile. Proto payloads (events.v1.GroupEvent) replace this
// JSON once generated code is committed to the build.
type NATSEvents struct {
	nc  *nats.Conn
	log *slog.Logger
}

func NewNATSEvents(nc *nats.Conn, log *slog.Logger) *NATSEvents {
	return &NATSEvents{nc: nc, log: log}
}

func (e *NATSEvents) publish(groupID string, ev map[string]any) {
	payload, _ := json.Marshal(ev)
	if err := e.nc.Publish(groupEventsSubject(groupID), payload); err != nil {
		e.log.Warn("publishing group event failed (best-effort)",
			"group", groupID, "kind", ev["kind"], "err", err)
	}
}

func (e *NATSEvents) MemberAdded(_ context.Context, groupID string, version int64, actor, subject string) {
	e.publish(groupID, map[string]any{"kind": "member_added", "group": groupID, "version": version, "actor": actor, "subject": subject})
}

func (e *NATSEvents) MemberRemoved(_ context.Context, groupID string, version int64, actor, subject string) {
	e.publish(groupID, map[string]any{"kind": "member_removed", "group": groupID, "version": version, "actor": actor, "subject": subject})
}

func (e *NATSEvents) RoleChanged(_ context.Context, groupID string, version int64, actor, subject string, role domain.Role) {
	e.publish(groupID, map[string]any{"kind": "role_changed", "group": groupID, "version": version, "actor": actor, "subject": subject, "role": int(role)})
}

func (e *NATSEvents) InfoChanged(_ context.Context, groupID string, version int64, actor string) {
	e.publish(groupID, map[string]any{"kind": "info_changed", "group": groupID, "version": version, "actor": actor})
}

func (e *NATSEvents) SettingsChanged(_ context.Context, groupID string, version int64, actor string) {
	e.publish(groupID, map[string]any{"kind": "settings_changed", "group": groupID, "version": version, "actor": actor})
}

func (e *NATSEvents) GroupDeleted(_ context.Context, groupID, actor string) {
	e.publish(groupID, map[string]any{"kind": "group_deleted", "group": groupID, "actor": actor})
}

// NoopEvents drops events — used until NATS is wired into a running deployable.
type NoopEvents struct{}

func (NoopEvents) MemberAdded(context.Context, string, int64, string, string)              {}
func (NoopEvents) MemberRemoved(context.Context, string, int64, string, string)            {}
func (NoopEvents) RoleChanged(context.Context, string, int64, string, string, domain.Role) {}
func (NoopEvents) InfoChanged(context.Context, string, int64, string)                      {}
func (NoopEvents) SettingsChanged(context.Context, string, int64, string)                  {}
func (NoopEvents) GroupDeleted(context.Context, string, string)                            {}

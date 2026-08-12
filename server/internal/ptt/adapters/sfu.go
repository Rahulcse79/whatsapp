package adapters

import (
	"context"
	"log/slog"
)

// SFU enforcement (rtc-lld §4). A grant flips LiveKit publish permission for the
// current fence only; a demoted/lapsed speaker is denied publish, so its RTP is
// dropped at the SFU (fenced token → stale audio is dead). The real flip is a
// LiveKit RoomService.UpdateParticipant admin call (participant permissions +
// fence in metadata) — the same LiveKit-admin seam the call-ctl webhook path
// uses. NoopSFU is the default until the RoomService client is wired: the floor
// control, fencing, and signaling are complete and correct without it; only the
// media-plane mute is deferred.
type NoopSFU struct{ log *slog.Logger }

func NewNoopSFU(log *slog.Logger) *NoopSFU { return &NoopSFU{log: log} }

func (s *NoopSFU) AllowPublish(_ context.Context, room, participant string, fence int64) error {
	s.log.Debug("ptt sfu: allow publish (seam)", "room", room, "participant", participant, "fence", fence)
	return nil
}

func (s *NoopSFU) DenyPublish(_ context.Context, room, participant string) error {
	s.log.Debug("ptt sfu: deny publish (seam)", "room", room, "participant", participant)
	return nil
}

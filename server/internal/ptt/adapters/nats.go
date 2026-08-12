package adapters

import (
	"context"
	"strings"

	"github.com/nats-io/nats.go"
	"google.golang.org/protobuf/proto"

	wsv1 "github.com/whatsapp-v2/server/internal/proto/gen/whatsapp/ws/v1"
)

// Signaler publishes PTT WS frames (PttGrant / PttQueuePos / PttRelease) to a
// participant's device via the per-device call subject dev.{deviceId}.call (the
// same fan-out the 1:1 call frames use; the gateway forwards it). Ephemeral core
// NATS — a frame missed while a device is between subscriptions is corrected by
// the next heartbeat/grant.
type Signaler struct{ nc *nats.Conn }

func NewSignaler(nc *nats.Conn) *Signaler { return &Signaler{nc: nc} }

func callSubject(deviceID string) string { return "dev." + deviceID + ".call" }

// participant is "userID:deviceID".
func splitParticipant(p string) (userID, deviceID string) {
	if i := strings.LastIndex(p, ":"); i >= 0 {
		return p[:i], p[i+1:]
	}
	return "", p
}

func (s *Signaler) Grant(_ context.Context, room, participant string, fence, maxSpeakMS int64) error {
	user, device := splitParticipant(participant)
	return s.send(device, &wsv1.Frame{Body: &wsv1.Frame_PttGrant{PttGrant: &wsv1.PttGrant{
		RoomId: room, Fence: fence, HolderUserId: user, HolderDeviceId: device, MaxSpeakMs: uint64(maxSpeakMS),
	}}})
}

func (s *Signaler) QueuePos(_ context.Context, room, participant string, position int) error {
	_, device := splitParticipant(participant)
	return s.send(device, &wsv1.Frame{Body: &wsv1.Frame_PttQueuePos{PttQueuePos: &wsv1.PttQueuePos{
		RoomId: room, Position: int32(position),
	}}})
}

func (s *Signaler) Revoke(_ context.Context, room, participant, reason string) error {
	_, device := splitParticipant(participant)
	return s.send(device, &wsv1.Frame{Body: &wsv1.Frame_PttRelease{PttRelease: &wsv1.PttRelease{
		RoomId: room, Reason: reason,
	}}})
}

func (s *Signaler) send(deviceID string, frame *wsv1.Frame) error {
	payload, err := proto.Marshal(frame)
	if err != nil {
		return err
	}
	return s.nc.Publish(callSubject(deviceID), payload)
}

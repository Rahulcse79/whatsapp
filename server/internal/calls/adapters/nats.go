package adapters

import (
	"context"
	"time"

	"github.com/nats-io/nats.go"
	"google.golang.org/protobuf/proto"

	"github.com/whatsapp-v2/server/internal/calls"
	"github.com/whatsapp-v2/server/internal/calls/domain"
	eventsv1 "github.com/whatsapp-v2/server/internal/proto/gen/whatsapp/events/v1"
	wsv1 "github.com/whatsapp-v2/server/internal/proto/gen/whatsapp/ws/v1"
)

// CallSubject is the per-device call-signaling subject, mirroring the existing
// dev.{id}.out (delivery) and dev.{id}.receipt fan-out. The ws-gateway forwards
// frames on this subject to the device; call frames don't ride dev.*.out because
// that path is decoded as events.v1.Delivery (chat inbox items) only.
func CallSubject(deviceID string) string { return "dev." + deviceID + ".call" }

// Signaler implements calls.Signaler over core NATS: it marshals wsv1 call frames
// and publishes them to each recipient device's call subject. Ephemeral, like
// presence — a frame missed while a device is offline is recovered by the VoIP
// push waking it, then the ring state fetched on connect.
type Signaler struct{ nc *nats.Conn }

func NewSignaler(nc *nats.Conn) *Signaler { return &Signaler{nc: nc} }

func (s *Signaler) Offer(_ context.Context, deviceIDs []string, o calls.CallOfferSignal) error {
	frame := &wsv1.Frame{Body: &wsv1.Frame_CallOffer{CallOffer: &wsv1.CallOffer{
		RingId:             o.RingID,
		RoomId:             o.RoomID,
		Kind:               wsv1.CallKind(o.Kind),
		CallerUserId:       o.CallerUserID,
		CallerDeviceId:     o.CallerDeviceID,
		ParticipantUserIds: o.ParticipantUserIDs,
	}}}
	return s.fanout(deviceIDs, frame)
}

func (s *Signaler) Ring(_ context.Context, deviceIDs []string, ringID string, state domain.RingState, byUserID string) error {
	frame := &wsv1.Frame{Body: &wsv1.Frame_CallRing{CallRing: &wsv1.CallRing{
		RingId:   ringID,
		State:    wsv1.RingState(state),
		ByUserId: byUserID,
	}}}
	return s.fanout(deviceIDs, frame)
}

func (s *Signaler) End(_ context.Context, deviceIDs []string, ringID, roomID, reason string) error {
	frame := &wsv1.Frame{Body: &wsv1.Frame_CallEnd{CallEnd: &wsv1.CallEnd{
		RingId: ringID,
		RoomId: roomID,
		Reason: reason,
	}}}
	return s.fanout(deviceIDs, frame)
}

func (s *Signaler) fanout(deviceIDs []string, frame *wsv1.Frame) error {
	payload, err := proto.Marshal(frame)
	if err != nil {
		return err
	}
	for _, d := range deviceIDs {
		if err := s.nc.Publish(CallSubject(d), payload); err != nil {
			return err
		}
	}
	return nil
}

var _ calls.Signaler = (*Signaler)(nil)

// Pusher implements calls.Pusher by enqueuing VoIP/missed pushes on push.dispatch
// (notification-svc's work queue). Best-effort core publish — the PUSH JetStream
// stream captures the subject; a lost enqueue is complemented by the ring machine.
type Pusher struct{ nc *nats.Conn }

func NewPusher(nc *nats.Conn) *Pusher { return &Pusher{nc: nc} }

func (p *Pusher) VoIP(_ context.Context, deviceIDs []string, inv calls.CallInvite) error {
	return p.dispatch(deviceIDs, inv.RingID, inv.RingID) // collapse_key = ring: coalesce re-rings
}

func (p *Pusher) Missed(_ context.Context, deviceIDs []string, ringID string) error {
	return p.dispatch(deviceIDs, ringID, "missed:"+ringID)
}

func (p *Pusher) dispatch(deviceIDs []string, ringID, collapseKey string) error {
	now := time.Now().UnixMilli()
	for _, d := range deviceIDs {
		payload, err := proto.Marshal(&eventsv1.PushDispatch{
			RecipientDeviceId: d,
			Kind:              eventsv1.PushKind_PUSH_KIND_CALL,
			CollapseKey:       collapseKey,
			RingId:            ringID,
			EnqueuedAtMs:      now,
		})
		if err != nil {
			return err
		}
		if err := p.nc.Publish("push.dispatch", payload); err != nil {
			return err
		}
	}
	return nil
}

var _ calls.Pusher = (*Pusher)(nil)

package ptt

import (
	"context"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/ptt/domain"
)

type logger interface {
	Warn(msg string, args ...any)
}

// Service turns PTT WS requests into floor transitions, then applies each
// transition's effects: flip SFU publish permission (fence-scoped) and push the
// PttGrant / PttQueuePos / PttRelease frames back to the affected devices.
type Service struct {
	floor FloorStore
	sfu   SFU
	sig   Signaler
	log   logger
}

func NewService(floor FloorStore, sfu SFU, sig Signaler, log logger) *Service {
	return &Service{floor: floor, sfu: sfu, sig: sig, log: log}
}

// Acquire handles PttRequest{ACQUIRE}: grant the floor, or queue the requester.
// Any lapsed holder is muted and any promoted queue head is granted as a side
// effect (the floor store computes these atomically).
func (s *Service) Acquire(ctx context.Context, room string, ident auth.Identity) error {
	p := participantOf(ident.UserID, ident.DeviceID)
	r, err := s.floor.Acquire(ctx, room, p)
	if err != nil {
		return err
	}
	s.applyDemote(ctx, room, r.Demoted)
	s.applyGrant(ctx, room, r.Promoted)
	switch {
	case r.Granted != nil:
		s.applyGrant(ctx, room, r.Granted)
	case r.Full:
		s.try("revoke-full", s.sig.Revoke(ctx, room, p, "queue_full"))
	case r.Position > 0:
		s.try("queue-pos", s.sig.QueuePos(ctx, room, p, r.Position))
	}
	return nil
}

// Heartbeat handles PttRequest{HEARTBEAT}: refresh the lease, or tell a device it
// lost the floor (missed too many heartbeats / superseded).
func (s *Service) Heartbeat(ctx context.Context, room string, ident auth.Identity) error {
	p := participantOf(ident.UserID, ident.DeviceID)
	r, err := s.floor.Heartbeat(ctx, room, p)
	if err != nil {
		return err
	}
	if !r.Held {
		s.try("deny-lost", s.sfu.DenyPublish(ctx, room, p))
		s.try("revoke-lost", s.sig.Revoke(ctx, room, p, "lost"))
	}
	return nil
}

// Release handles PttRequest{RELEASE}: drop the floor (button-up) and promote the
// next waiter.
func (s *Service) Release(ctx context.Context, room string, ident auth.Identity) error {
	p := participantOf(ident.UserID, ident.DeviceID)
	r, err := s.floor.Release(ctx, room, p)
	if err != nil {
		return err
	}
	if r.Released {
		s.try("deny-release", s.sfu.DenyPublish(ctx, room, p))
		s.applyGrant(ctx, room, r.Next)
	}
	return nil
}

// SweepAll promotes queue heads for rooms whose holder stopped heartbeating.
// Run on a ticker (~the heartbeat interval) so a waiter still gets the floor when
// the holder vanishes and no one else acquires.
func (s *Service) SweepAll(ctx context.Context) (int, error) {
	rooms, err := s.floor.ActiveRooms(ctx)
	if err != nil {
		return 0, err
	}
	promoted := 0
	for _, room := range rooms {
		r, err := s.floor.Sweep(ctx, room)
		if err != nil {
			s.log.Warn("ptt: sweep failed", "room", room, "err", err)
			continue
		}
		s.applyDemote(ctx, room, r.Demoted)
		if r.Promoted != nil {
			s.applyGrant(ctx, room, r.Promoted)
			promoted++
		}
	}
	return promoted, nil
}

// State returns the room's current speaker + queue length (the join response).
func (s *Service) State(ctx context.Context, room string) (RoomState, error) {
	holder, queueLen, err := s.floor.Snapshot(ctx, room)
	if err != nil {
		return RoomState{}, err
	}
	return RoomState{CurrentSpeaker: holder, QueueLen: queueLen}, nil
}

func (s *Service) applyGrant(ctx context.Context, room string, g *domain.Grant) {
	if g == nil {
		return
	}
	s.try("allow-publish", s.sfu.AllowPublish(ctx, room, g.Device, g.Fence))
	s.try("grant", s.sig.Grant(ctx, room, g.Device, g.Fence, int64(maxSpeakMS)))
}

func (s *Service) applyDemote(ctx context.Context, room, demoted string) {
	if demoted == "" {
		return
	}
	s.try("deny-demote", s.sfu.DenyPublish(ctx, room, demoted))
	s.try("revoke-demote", s.sig.Revoke(ctx, room, demoted, "lapsed"))
}

func (s *Service) try(step string, err error) {
	if err != nil {
		s.log.Warn("ptt: signaling step failed", "step", step, "err", err)
	}
}

package calls

import (
	"time"

	"github.com/whatsapp-v2/server/internal/calls/domain"
)

// RingRecord is the server-authoritative ring state (RingStore; Valkey, TTL by
// state — short while ringing, call-length once answered).
type RingRecord struct {
	RingID         string
	RoomID         string
	Kind           domain.CallKind
	CallerID       string
	CallerDeviceID string
	CalleeIDs      []string
	State          domain.RingState
	StartedAt      time.Time
	Deadline       time.Time // miss deadline (StartedAt + MissedAfter)
	AnsweredBy     string    // device id that answered
}

// answered reports whether the call was ever answered (drives history outcome).
func (r RingRecord) answered() bool { return r.AnsweredBy != "" }

// CreateResult is the POST /v1/calls response.
type CreateResult struct {
	RoomID    string `json:"room_id"`
	JoinToken string `json:"join_token"`
	RingID    string `json:"ring_id"`
}

// Outcome mirrors call_records.outcome.
type Outcome int16

const (
	OutcomeCompleted Outcome = 0
	OutcomeMissed    Outcome = 1
	OutcomeDeclined  Outcome = 2
	OutcomeFailed    Outcome = 3
)

// CallRecord is a call_records history row (metadata only, 90-day retention).
type CallRecord struct {
	ID           string          `json:"id"`
	RoomID       string          `json:"room_id"`
	Kind         domain.CallKind `json:"kind"`
	Initiator    string          `json:"initiator"`
	Participants []string        `json:"participants"`
	StartedAt    *time.Time      `json:"started_at,omitempty"`
	EndedAt      *time.Time      `json:"ended_at,omitempty"`
	Outcome      Outcome         `json:"outcome"`
}

// CallOfferSignal is the CallOffer WS frame the callee devices receive.
type CallOfferSignal struct {
	RingID             string
	RoomID             string
	Kind               domain.CallKind
	CallerUserID       string
	CallerDeviceID     string
	ParticipantUserIDs []string
}

// CallInvite is the VoIP-push payload that wakes a callee device to ring.
type CallInvite struct {
	RingID       string
	RoomID       string
	Kind         domain.CallKind
	CallerUserID string
}

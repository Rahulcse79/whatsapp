// Package domain holds the advanced live-session pure logic (T9.03): breakout
// rooms, streaming egress (RTMP/HLS) target validation, and recording-consent
// state. No I/O. Media rides the existing LiveKit plane; breakout participants
// get a fresh role-scoped join token per room, egress/recording are host-driven
// with a consent gate that clients honour (the SFU only ever sees ciphertext).
package domain

import (
	"errors"
	"net/url"
	"strings"
)

// EgressKind is the streaming-out transport.
type EgressKind int16

const (
	EgressRTMP EgressKind = 0 // push to an RTMP(S) ingest (YouTube/Twitch/…)
	EgressHLS  EgressKind = 1 // segment to an HLS target (storage/CDN prefix)
)

func (k EgressKind) String() string {
	if k == EgressHLS {
		return "hls"
	}
	return "rtmp"
}

// ParseEgressKind maps the wire value to a kind.
func ParseEgressKind(s string) (EgressKind, bool) {
	switch s {
	case "rtmp":
		return EgressRTMP, true
	case "hls":
		return EgressHLS, true
	default:
		return 0, false
	}
}

// EgressState is the streaming lifecycle.
type EgressState int16

const (
	EgressOff  EgressState = 0
	EgressLive EgressState = 1
)

func (s EgressState) String() string {
	if s == EgressLive {
		return "live"
	}
	return "off"
}

// RecordingState is the recording lifecycle. "requested" is the consent window:
// the host asked to record, clients are prompted; "active" means recording is on
// (clients that consented capture locally, and every client shows the indicator).
type RecordingState int16

const (
	RecordingOff       RecordingState = 0
	RecordingRequested RecordingState = 1
	RecordingActive    RecordingState = 2
)

func (s RecordingState) String() string {
	switch s {
	case RecordingRequested:
		return "requested"
	case RecordingActive:
		return "active"
	default:
		return "off"
	}
}

const (
	MaxRoomName      = 60
	MaxBreakoutRooms = 50
	maxTargetLen     = 2048
)

var (
	ErrBadRoomName  = errors.New("breakout: room name is required (max 60 chars)")
	ErrBadRoomCount = errors.New("breakout: create between 1 and 50 rooms")
	ErrBadEgressURL = errors.New("breakout: invalid streaming target for this kind")
	ErrEmptyTarget  = errors.New("breakout: streaming target is required")
)

// ValidateRoomName checks a breakout room label.
func ValidateRoomName(name string) error {
	n := strings.TrimSpace(name)
	if n == "" || len(name) > MaxRoomName {
		return ErrBadRoomName
	}
	return nil
}

// ValidateRoomCount bounds a bulk create.
func ValidateRoomCount(n int) error {
	if n < 1 || n > MaxBreakoutRooms {
		return ErrBadRoomCount
	}
	return nil
}

// ValidateEgressTarget checks the streaming target for the chosen kind: RTMP must
// be an rtmp(s):// URL; HLS must be a non-empty https target or storage prefix.
func ValidateEgressTarget(kind EgressKind, target string) error {
	t := strings.TrimSpace(target)
	if t == "" || len(t) > maxTargetLen {
		return ErrEmptyTarget
	}
	switch kind {
	case EgressRTMP:
		u, err := url.Parse(t)
		if err != nil || (u.Scheme != "rtmp" && u.Scheme != "rtmps") || u.Host == "" {
			return ErrBadEgressURL
		}
	case EgressHLS:
		u, err := url.Parse(t)
		if err != nil || (u.Scheme != "https" && u.Scheme != "s3" && u.Scheme != "gs") || u.Host == "" {
			return ErrBadEgressURL
		}
	default:
		return ErrBadEgressURL
	}
	return nil
}

// ConsentSummary tallies recording consent across present participants — what the
// host sees, and what tells the recorder which tracks it may capture.
type ConsentSummary struct {
	Total     int `json:"total"`
	Consented int `json:"consented"`
	Declined  int `json:"declined"`
	Pending   int `json:"pending"`
}

// Tally reduces a set of decisions (consented?, decided?) to a summary.
func Tally(decisions []Decision) ConsentSummary {
	var c ConsentSummary
	c.Total = len(decisions)
	for _, d := range decisions {
		switch {
		case !d.Decided:
			c.Pending++
		case d.Consented:
			c.Consented++
		default:
			c.Declined++
		}
	}
	return c
}

// Decision is one participant's recording-consent answer.
type Decision struct {
	Decided   bool
	Consented bool
}

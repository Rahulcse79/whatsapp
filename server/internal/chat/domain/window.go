// Package domain holds the chat context's pure logic. No I/O — enforced by
// depguard (domain-stays-pure).
package domain

import (
	"time"

	"github.com/whatsapp-v2/server/internal/platform/id"
)

// Overlay windows (FR-MSG-05/06).
const (
	EditWindow   = 15 * time.Minute
	DeleteWindow = 48 * time.Hour
)

// WindowResult classifies an overlay-window check.
type WindowResult int

const (
	WindowOK WindowResult = iota
	WindowEditClosed
	WindowDeleteClosed
	WindowBadTarget
)

// CheckOverlayWindow validates that an edit/delete overlay falls within its
// allowed window.
//
// The window is anchored to the ORIGINAL message's UUIDv7 embedded timestamp
// (targetUUID), not a stored server timestamp — the server keeps no message
// metadata after delivery/ACK, and a v7 id carries its own millisecond
// creation time. A client cannot widen the window: targetUUID must equal the
// real original message's id (recipients match overlays by it), and that id's
// timestamp is fixed at original-send time.
//
// editWindow/deleteWindow are injectable so tests avoid real time.
func CheckOverlayWindow(editKind, deleteKind bool, targetUUID string, now time.Time) WindowResult {
	if !editKind && !deleteKind {
		return WindowOK // reactions/pins have no time limit
	}
	u, err := id.Parse(targetUUID)
	if err != nil {
		return WindowBadTarget
	}
	age := now.Sub(id.TimeOf(u))
	switch {
	case editKind && age > EditWindow:
		return WindowEditClosed
	case deleteKind && age > DeleteWindow:
		return WindowDeleteClosed
	default:
		return WindowOK
	}
}

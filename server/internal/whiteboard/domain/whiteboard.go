// Package domain holds the whiteboard pure logic (T12.02): op validation. The
// board is an append-only CRDT op-log (grow-only stroke set + erase tombstones +
// clear barrier) — the client (@wa/client-core) owns the merge/render; the server
// stores and relays ops under a monotonic seq, gated on conversation membership.
package domain

import "errors"

const (
	MaxDataBytes = 64 * 1024 // one op's JSON payload (a long stroke)
	MaxBatch     = 200       // ops per append request
)

var (
	ErrBadKind = errors.New("whiteboard: op kind must be stroke, erase, or clear")
	ErrBadSeq  = errors.New("whiteboard: op seq must be positive")
	ErrBadData = errors.New("whiteboard: op payload too large")
	ErrBadID   = errors.New("whiteboard: op id is required")
)

// KindValid reports whether an op kind is one the board understands.
func KindValid(kind string) bool {
	return kind == "stroke" || kind == "erase" || kind == "clear"
}

// ValidateOp checks one op's envelope before it's stored.
func ValidateOp(id, kind string, seq int64, dataLen int) error {
	if id == "" {
		return ErrBadID
	}
	if !KindValid(kind) {
		return ErrBadKind
	}
	if seq <= 0 {
		return ErrBadSeq
	}
	if dataLen > MaxDataBytes {
		return ErrBadData
	}
	return nil
}

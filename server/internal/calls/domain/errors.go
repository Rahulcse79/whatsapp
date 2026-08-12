package domain

import "errors"

var (
	ErrBadKind        = errors.New("calls: kind must be voice or video")
	ErrNoCallees      = errors.New("calls: at least one callee required")
	ErrTooManyCallees = errors.New("calls: too many callees (max 31)")
)

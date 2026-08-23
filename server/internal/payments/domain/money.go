// Package domain holds the payments pure logic (T15.05): money validation, the
// payment state machine, PSP webhook signature verification, and the card-data
// guard. No I/O.
//
// THE INVARIANT OF THIS PACKAGE: raw card data never reaches this system. We
// integrate with a PSP in its hosted/tokenised mode — the payer enters card
// details on the processor's own surface, and we only ever hold opaque
// processor references. `RejectsCardData` exists to make that invariant
// enforceable rather than merely intended.
package domain

import (
	"errors"
	"strings"
)

// Currency is an ISO-4217 alphabetic code, upper-case.
type Currency string

// Supported currencies. Deliberately a short allow-list: an unknown currency is
// a bug or an attack, never something to guess at.
var supportedCurrencies = map[Currency]struct{}{
	"USD": {}, "EUR": {}, "GBP": {}, "INR": {}, "AUD": {}, "CAD": {}, "JPY": {}, "SGD": {},
}

// MaxAmountCents caps a single operation. A ceiling is a blast-radius control:
// a bug or a compromised caller cannot request an unbounded charge.
const MaxAmountCents int64 = 1_000_000_00 // 1,000,000 major units

var (
	ErrBadCurrency = errors.New("payments: unsupported currency")
	ErrBadAmount   = errors.New("payments: amount must be > 0 and within the cap")
	ErrZeroAmount  = errors.New("payments: amount must be greater than zero")
)

// Money is a minor-unit amount in a currency. Integer minor units only — never
// a float, because binary floats cannot represent decimal money exactly.
type Money struct {
	Cents    int64    `json:"cents"`
	Currency Currency `json:"currency"`
}

// NewMoney validates and constructs an amount.
func NewMoney(cents int64, currency string) (Money, error) {
	c := Currency(strings.ToUpper(strings.TrimSpace(currency)))
	if _, ok := supportedCurrencies[c]; !ok {
		return Money{}, ErrBadCurrency
	}
	if cents <= 0 {
		return Money{}, ErrZeroAmount
	}
	if cents > MaxAmountCents {
		return Money{}, ErrBadAmount
	}
	return Money{Cents: cents, Currency: c}, nil
}

// Validate re-checks an amount that arrived from storage or the wire.
func (m Money) Validate() error {
	_, err := NewMoney(m.Cents, string(m.Currency))
	return err
}

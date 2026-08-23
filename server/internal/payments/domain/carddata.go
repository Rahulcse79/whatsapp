package domain

import (
	"errors"
	"regexp"
	"strings"
)

// The card-data guard.
//
// The architectural promise of this context is that raw card data never reaches
// our servers: the payer enters it on the PSP's hosted surface and we hold only
// opaque references. That promise is easy to state and easy to erode — someone
// adds a "note" or "metadata" field, a client puts a PAN in it, and now card
// numbers are in our database and logs, and the deployment has silently left
// PCI-DSS SAQ-A.
//
// So the boundary rejects anything that looks like card data instead of trusting
// callers. False positives are acceptable here: a free-text field that happens
// to contain a 16-digit Luhn-valid number is vanishingly rare compared to the
// cost of storing a real PAN.

var (
	ErrCardData = errors.New("payments: input looks like raw card data and was rejected — card details must go directly to the payment processor, never through this API")
)

// digitRun finds runs of 12–19 digits, optionally separated by spaces or dashes
// in groups — the shape of a PAN as a human or a form would write it.
var digitRun = regexp.MustCompile(`\d(?:[ -]?\d){11,18}`)

// cvvLike catches an explicit CVV/CVC label followed by 3–4 digits. A bare
// 3-digit number is far too common to flag, but a labelled one is unambiguous.
var cvvLike = regexp.MustCompile(`(?i)\b(?:cvv|cvc|cvv2|cid|security\s*code)\b\D{0,10}\d{3,4}`)

// RejectsCardData reports an error when s plausibly contains a card number or a
// labelled security code. Used on every free-text field this context accepts.
func RejectsCardData(s string) error {
	if s == "" {
		return nil
	}
	if cvvLike.MatchString(s) {
		return ErrCardData
	}
	for _, candidate := range digitRun.FindAllString(s, -1) {
		digits := stripSeparators(candidate)
		if len(digits) < 12 || len(digits) > 19 {
			continue
		}
		if luhnValid(digits) {
			return ErrCardData
		}
	}
	return nil
}

// RejectsCardDataIn applies the guard to several fields at once.
func RejectsCardDataIn(fields ...string) error {
	for _, f := range fields {
		if err := RejectsCardData(f); err != nil {
			return err
		}
	}
	return nil
}

func stripSeparators(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r >= '0' && r <= '9' {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// luhnValid is the check digit every major card scheme uses. It is what makes
// the guard specific: a random 16-digit order number passes through, a real PAN
// does not.
func luhnValid(digits string) bool {
	sum, alt := 0, false
	for i := len(digits) - 1; i >= 0; i-- {
		d := int(digits[i] - '0')
		if alt {
			d *= 2
			if d > 9 {
				d -= 9
			}
		}
		sum += d
		alt = !alt
	}
	return sum%10 == 0
}

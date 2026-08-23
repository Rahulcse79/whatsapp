package domain

import "testing"

func TestNewMoney(t *testing.T) {
	if _, err := NewMoney(999, "usd"); err != nil {
		t.Fatalf("lower-case currency should be accepted: %v", err)
	}
	cases := []struct {
		name  string
		cents int64
		cur   string
		want  error
	}{
		{"zero", 0, "USD", ErrZeroAmount},
		{"negative", -1, "USD", ErrZeroAmount},
		{"over cap", MaxAmountCents + 1, "USD", ErrBadAmount},
		{"unknown currency", 100, "XYZ", ErrBadCurrency},
		{"empty currency", 100, "", ErrBadCurrency},
	}
	for _, c := range cases {
		if _, err := NewMoney(c.cents, c.cur); err != c.want {
			t.Errorf("%s: got %v want %v", c.name, err, c.want)
		}
	}
}

// ── The card-data guard: the invariant of this whole context ────────────────

func TestRejectsCardData(t *testing.T) {
	// Luhn-valid test PANs published by the card schemes for exactly this use.
	pans := []string{
		"4242424242424242",                   // Visa
		"4242 4242 4242 4242",                // spaced
		"4242-4242-4242-4242",                // dashed
		"5555555555554444",                   // Mastercard
		"378282246310005",                    // Amex (15)
		"6011111111111117",                   // Discover
		"my card is 4242424242424242 thanks", // embedded in prose
	}
	for _, pan := range pans {
		if err := RejectsCardData(pan); err != ErrCardData {
			t.Errorf("PAN %q must be rejected, got %v", pan, err)
		}
	}

	// A labelled security code is unambiguous even without a PAN.
	for _, s := range []string{"cvv 123", "CVC: 4567", "security code 999"} {
		if err := RejectsCardData(s); err != ErrCardData {
			t.Errorf("%q must be rejected, got %v", s, err)
		}
	}
}

func TestRejectsCardDataAllowsOrdinaryText(t *testing.T) {
	// The guard must not swallow legitimate input, or callers will route around
	// it. None of these are Luhn-valid card numbers.
	ok := []string{
		"",
		"thanks for lunch",
		"invoice 12345",
		"order 4242424242424241",               // one digit off: Luhn-invalid
		"+14155550123",                         // phone number
		"2026-08-23",                           // a date
		"01a02d70-6b49-759d-b334-11f006d9eca6", // a UUID
		"123",                                  // a bare short number
		"999999999999",                         // 12 digits, Luhn-invalid
	}
	for _, s := range ok {
		if err := RejectsCardData(s); err != nil {
			t.Errorf("%q should pass the guard, got %v", s, err)
		}
	}
}

func TestRejectsCardDataInAnyField(t *testing.T) {
	if err := RejectsCardDataIn("fine", "also fine", "4242424242424242"); err != ErrCardData {
		t.Fatalf("the guard must check every field, got %v", err)
	}
	if err := RejectsCardDataIn("a", "b", "c"); err != nil {
		t.Fatalf("clean fields should pass, got %v", err)
	}
}

// ── State machine ───────────────────────────────────────────────────────────

func TestTransitions(t *testing.T) {
	legal := [][2]Status{
		{StatusPending, StatusSucceeded},
		{StatusPending, StatusFailed},
		{StatusPending, StatusCanceled},
		{StatusSucceeded, StatusRefunded},
	}
	for _, p := range legal {
		changed, err := Transition(p[0], p[1])
		if err != nil || !changed {
			t.Errorf("%s→%s should be legal: changed=%v err=%v", p[0], p[1], changed, err)
		}
	}

	illegal := [][2]Status{
		{StatusFailed, StatusSucceeded},   // a decline cannot become a success
		{StatusRefunded, StatusSucceeded}, // nor can a refund be undone
		{StatusCanceled, StatusSucceeded},
		{StatusSucceeded, StatusFailed},
		{StatusSucceeded, StatusPending}, // no going backwards
	}
	for _, p := range illegal {
		if _, err := Transition(p[0], p[1]); err != ErrBadTransition {
			t.Errorf("%s→%s must be rejected, got %v", p[0], p[1], err)
		}
	}
}

// A PSP redelivering the same event must be a no-op, not an error: a handler
// that fails on a duplicate gets retried forever.
func TestRedeliveryIsIdempotent(t *testing.T) {
	for _, s := range []Status{StatusPending, StatusSucceeded, StatusFailed, StatusCanceled, StatusRefunded} {
		changed, err := Transition(s, s)
		if err != nil {
			t.Errorf("re-applying %s should not error, got %v", s, err)
		}
		if changed {
			t.Errorf("re-applying %s should report no change", s)
		}
	}
}

func TestTerminal(t *testing.T) {
	if StatusPending.Terminal() || StatusSucceeded.Terminal() {
		t.Error("pending and succeeded still have moves")
	}
	for _, s := range []Status{StatusFailed, StatusCanceled, StatusRefunded} {
		if !s.Terminal() {
			t.Errorf("%s should be terminal", s)
		}
	}
}

func TestParse(t *testing.T) {
	if _, err := ParseStatus("nonsense"); err != ErrBadStatus {
		t.Error("unknown status must be rejected")
	}
	if _, err := ParsePurpose("mining"); err != ErrBadPurpose {
		t.Error("unknown purpose must be rejected")
	}
	for _, p := range []Purpose{PurposePremium, PurposeChannel, PurposeP2P} {
		if _, err := ParsePurpose(string(p)); err != nil {
			t.Errorf("%s should parse: %v", p, err)
		}
	}
}

// ── Webhook authentication ──────────────────────────────────────────────────

func TestVerifyWebhook(t *testing.T) {
	secret, body := []byte("psp-signing-secret"), []byte(`{"id":"evt_1","type":"payment.succeeded"}`)
	sig := SignWebhook(secret, body)

	if err := VerifyWebhook(secret, body, sig); err != nil {
		t.Fatalf("a genuine signature must verify: %v", err)
	}
	if err := VerifyWebhook([]byte("other"), body, sig); err != ErrBadSignature {
		t.Error("a forged event signed with the wrong secret must be rejected")
	}
	if err := VerifyWebhook(secret, []byte(`{"id":"evt_1","type":"payment.refunded"}`), sig); err != ErrBadSignature {
		t.Error("tampering with the body must invalidate the signature")
	}
	if err := VerifyWebhook(secret, body, ""); err != ErrBadSignature {
		t.Error("a missing signature must be rejected, never treated as absent-so-fine")
	}
}

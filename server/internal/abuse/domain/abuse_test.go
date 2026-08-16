package domain

import "testing"

func TestValidateReport(t *testing.T) {
	if err := ValidateReport(ReasonSpam, "not a person"); err != nil {
		t.Fatalf("valid report rejected: %v", err)
	}
	if err := ValidateReport(Reason(99), ""); err != ErrBadReason {
		t.Fatal("unknown reason should fail")
	}
	long := make([]byte, MaxNote+1)
	if err := ValidateReport(ReasonScam, string(long)); err != ErrBadNote {
		t.Fatal("over-long note should fail")
	}
}

func TestReasonString(t *testing.T) {
	for r, want := range map[Reason]string{
		ReasonSpam: "spam", ReasonHarassment: "harassment", ReasonScam: "scam",
		ReasonImpersonation: "impersonation", ReasonOther: "other",
	} {
		if got := r.String(); got != want {
			t.Errorf("Reason(%d).String() = %q, want %q", r, got, want)
		}
	}
}

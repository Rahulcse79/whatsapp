package domain

import "testing"

func TestValidate(t *testing.T) {
	if err := ValidateNote("", "x"); err != ErrBadTitle {
		t.Fatal("blank title should fail")
	}
	if err := ValidateNote("Plan", "ok"); err != nil {
		t.Fatalf("valid note rejected: %v", err)
	}
	if err := ValidateTask(""); err != ErrBadTask {
		t.Fatal("blank task should fail")
	}
	if err := ValidateComment(""); err != ErrBadComment {
		t.Fatal("blank comment should fail")
	}
}

func TestCheckVersion(t *testing.T) {
	next, err := CheckVersion(3, 3)
	if err != nil || next != 4 {
		t.Fatalf("in-sync edit → next version: %d %v", next, err)
	}
	if _, err := CheckVersion(3, 2); err != ErrStale {
		t.Fatal("stale base should conflict")
	}
}

func TestApprovalStateMachine(t *testing.T) {
	if !CanRequestApproval(ApprovalNone) || !CanRequestApproval(ApprovalRejected) {
		t.Fatal("none/rejected can request")
	}
	if CanRequestApproval(ApprovalPending) || CanRequestApproval(ApprovalApproved) {
		t.Fatal("pending/approved can't re-request")
	}
	if s, err := DecideApproval(ApprovalPending, true); err != nil || s != ApprovalApproved {
		t.Fatalf("pending→approved: %v %v", s, err)
	}
	if s, err := DecideApproval(ApprovalPending, false); err != nil || s != ApprovalRejected {
		t.Fatalf("pending→rejected: %v %v", s, err)
	}
	if _, err := DecideApproval(ApprovalApproved, true); err != ErrBadApproval {
		t.Fatal("can't decide a non-pending note")
	}
	if ApprovalPending.String() != "pending" || ApprovalApproved.String() != "approved" {
		t.Fatal("state strings")
	}
}

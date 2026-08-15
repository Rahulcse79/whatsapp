package domain

import (
	"errors"
	"testing"
)

func TestPublishGate(t *testing.T) {
	if CanPublish(RoleAttendee) {
		t.Fatal("attendees must not publish (1-to-many)")
	}
	if !CanPublish(RoleSpeaker) || !CanPublish(RoleHost) {
		t.Fatal("speaker + host publish")
	}
	if !CanHost(RoleHost) || CanHost(RoleSpeaker) || CanHost(RoleAttendee) {
		t.Fatal("host-only actions gate on host")
	}
}

func TestValidateCreate(t *testing.T) {
	if err := ValidateCreate("Launch Q3"); err != nil {
		t.Fatalf("valid title: %v", err)
	}
	if !errors.Is(ValidateCreate("   "), ErrBadTitle) {
		t.Fatal("blank title rejected")
	}
	if !errors.Is(ValidateCreate(string(make([]byte, 121))), ErrBadTitle) {
		t.Fatal("long title rejected")
	}
}

func TestValidateQuestion(t *testing.T) {
	if err := ValidateQuestion("How does billing work?"); err != nil {
		t.Fatalf("valid question: %v", err)
	}
	if !errors.Is(ValidateQuestion(""), ErrBadQuestion) {
		t.Fatal("empty question rejected")
	}
}

func TestStatusRoleStrings(t *testing.T) {
	if RoleHost.String() != "host" || RoleSpeaker.String() != "speaker" || RoleAttendee.String() != "attendee" {
		t.Fatal("role strings")
	}
	if StatusWaiting.String() != "waiting" || StatusAdmitted.String() != "admitted" || StatusLeft.String() != "left" {
		t.Fatal("status strings")
	}
}

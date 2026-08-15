package domain

import (
	"errors"
	"testing"
	"time"
)

func TestRolePermissions(t *testing.T) {
	if !CanManageGroups(RoleAdmin) || CanManageGroups(RoleMember) {
		t.Fatal("manage-groups is admin+")
	}
	if !CanChangeRoles(RoleOwner) || CanChangeRoles(RoleAdmin) {
		t.Fatal("change-roles is owner-only")
	}
	if !CanDelete(RoleOwner) || CanDelete(RoleAdmin) {
		t.Fatal("delete is owner-only")
	}
	if !CanModerate(RoleAdmin) || CanModerate(RoleMember) {
		t.Fatal("moderate is admin+")
	}
	if RoleOwner.Assignable() || !RoleAdmin.Assignable() || !RoleMember.Assignable() {
		t.Fatal("owner is not assignable; member/admin are")
	}
}

func TestValidateCreate(t *testing.T) {
	if err := ValidateCreate("Devs", "hi", KindPublic); err != nil {
		t.Fatalf("valid create: %v", err)
	}
	if !errors.Is(ValidateCreate("", "x", KindPublic), ErrBadName) {
		t.Fatal("empty name rejected")
	}
	if !errors.Is(ValidateCreate("ok", string(make([]byte, 501)), KindPublic), ErrBadDesc) {
		t.Fatal("long description rejected")
	}
	if !errors.Is(ValidateCreate("ok", "x", Kind(9)), ErrBadKind) {
		t.Fatal("bad kind rejected")
	}
}

func TestValidateEvent(t *testing.T) {
	now := time.UnixMilli(1_000_000)
	if err := ValidateEvent("Standup", "", now); err != nil {
		t.Fatalf("valid event: %v", err)
	}
	if !errors.Is(ValidateEvent("", "", now), ErrBadEventTitle) {
		t.Fatal("empty title rejected")
	}
	if !errors.Is(ValidateEvent("ok", "", time.Time{}), ErrBadEventTime) {
		t.Fatal("zero start time rejected")
	}
}

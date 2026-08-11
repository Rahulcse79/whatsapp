package domain

import "testing"

func TestRoleValidAndString(t *testing.T) {
	for r := RoleMember; r <= RoleOwner; r++ {
		if !r.Valid() {
			t.Fatalf("role %d should be valid", r)
		}
	}
	if Role(3).Valid() || Role(-1).Valid() {
		t.Fatal("out-of-range roles must be invalid")
	}
	if RoleOwner.String() != "owner" || RoleAdmin.String() != "admin" || RoleMember.String() != "member" {
		t.Fatal("role names wrong")
	}
	if RoleOwner.Assignable() {
		t.Fatal("owner must not be directly assignable via PUT role")
	}
	if !RoleAdmin.Assignable() || !RoleMember.Assignable() {
		t.Fatal("member/admin must be assignable")
	}
}

func TestSettingsValidity(t *testing.T) {
	if !DefaultSettings().Valid() {
		t.Fatal("default settings must be valid")
	}
	if (Settings{WhoCanPost: "everyone", WhoCanEditInfo: PolicyAll}).Valid() {
		t.Fatal("bad who_can_post accepted")
	}
	if (Settings{WhoCanPost: PolicyAll, WhoCanEditInfo: "nobody"}).Valid() {
		t.Fatal("bad who_can_edit_info accepted")
	}
}

func TestPermissionMatrix(t *testing.T) {
	// members: manage/roles/delete/settings all denied
	if CanManageMembers(RoleMember) || CanChangeRoles(RoleMember) || CanDeleteGroup(RoleMember) || CanEditSettings(RoleMember) {
		t.Fatal("member has too much power")
	}
	// admins manage members + settings, but not roles/delete
	if !CanManageMembers(RoleAdmin) || !CanEditSettings(RoleAdmin) {
		t.Fatal("admin should manage members + settings")
	}
	if CanChangeRoles(RoleAdmin) || CanDeleteGroup(RoleAdmin) {
		t.Fatal("admin must not change roles or delete")
	}
	// owner can do everything
	if !CanChangeRoles(RoleOwner) || !CanDeleteGroup(RoleOwner) {
		t.Fatal("owner should change roles + delete")
	}
}

func TestCanEditInfo(t *testing.T) {
	open := Settings{WhoCanPost: PolicyAll, WhoCanEditInfo: PolicyAll}
	locked := Settings{WhoCanPost: PolicyAll, WhoCanEditInfo: PolicyAdmins}
	if !CanEditInfo(RoleMember, open) {
		t.Fatal("who_can_edit_info=all lets members edit")
	}
	if CanEditInfo(RoleMember, locked) {
		t.Fatal("who_can_edit_info=admins blocks members")
	}
	if !CanEditInfo(RoleAdmin, locked) {
		t.Fatal("admins always edit info")
	}
}

func TestCanPost(t *testing.T) {
	open := Settings{WhoCanPost: PolicyAll, WhoCanEditInfo: PolicyAll}
	announce := Settings{WhoCanPost: PolicyAll, WhoCanEditInfo: PolicyAll, Announcements: true}
	adminsOnly := Settings{WhoCanPost: PolicyAdmins, WhoCanEditInfo: PolicyAll}
	if !CanPost(RoleMember, open) {
		t.Fatal("open group lets members post")
	}
	if CanPost(RoleMember, announce) {
		t.Fatal("announcements mode blocks member posts")
	}
	if CanPost(RoleMember, adminsOnly) || !CanPost(RoleAdmin, adminsOnly) {
		t.Fatal("who_can_post=admins gates by role")
	}
}

func TestCanRemove(t *testing.T) {
	if CanRemove(RoleAdmin, RoleOwner) || CanRemove(RoleOwner, RoleOwner) {
		t.Fatal("owner can never be removed")
	}
	if CanRemove(RoleAdmin, RoleAdmin) {
		t.Fatal("admin must not remove another admin")
	}
	if !CanRemove(RoleOwner, RoleAdmin) {
		t.Fatal("owner may remove an admin")
	}
	if !CanRemove(RoleAdmin, RoleMember) {
		t.Fatal("admin may remove a member")
	}
	if CanRemove(RoleMember, RoleMember) {
		t.Fatal("member cannot remove anyone")
	}
}

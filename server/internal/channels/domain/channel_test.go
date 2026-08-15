package domain

import (
	"strings"
	"testing"
)

func TestRolePermissions(t *testing.T) {
	if CanPost(RoleFollower) || !CanPost(RoleAdmin) || !CanPost(RoleOwner) {
		t.Fatal("post: admin+ only")
	}
	if CanChangeRoles(RoleAdmin) || !CanChangeRoles(RoleOwner) {
		t.Fatal("roles: owner only")
	}
	if CanDelete(RoleAdmin) || !CanDelete(RoleOwner) {
		t.Fatal("delete: owner only")
	}
	if CanEditInfo(RoleFollower) || !CanEditInfo(RoleAdmin) {
		t.Fatal("edit info: admin+ only")
	}
	if RoleOwner.Assignable() || !RoleAdmin.Assignable() || !RoleFollower.Assignable() {
		t.Fatal("owner is not assignable via the role endpoint")
	}
	if RoleOwner.String() != "owner" || RoleAdmin.String() != "admin" || RoleFollower.String() != "follower" {
		t.Fatal("role labels")
	}
	if Role(9).Valid() || !RoleAdmin.Valid() {
		t.Fatal("role validity bounds")
	}
}

func TestValidHandle(t *testing.T) {
	ok := []string{"news", "wa_official", "abc", strings.Repeat("a", 30)}
	bad := []string{"ab", "", "UPPER", "has space", "dot.dot", strings.Repeat("a", 31), "emoji😀"}
	for _, h := range ok {
		if !ValidHandle(h) {
			t.Errorf("expected %q valid", h)
		}
	}
	for _, h := range bad {
		if ValidHandle(h) {
			t.Errorf("expected %q invalid", h)
		}
	}
}

func TestValidateCreate(t *testing.T) {
	if err := ValidateCreate("news", "News", "desc", KindPublic); err != nil {
		t.Fatalf("valid channel rejected: %v", err)
	}
	if ValidateCreate("no", "News", "", KindPublic) != ErrBadHandle {
		t.Fatal("short handle")
	}
	if ValidateCreate("news", "", "", KindPublic) != ErrBadName {
		t.Fatal("empty name")
	}
	if ValidateCreate("news", strings.Repeat("n", 81), "", KindPublic) != ErrBadName {
		t.Fatal("long name")
	}
	if ValidateCreate("news", "News", strings.Repeat("d", 501), KindPublic) != ErrBadDesc {
		t.Fatal("long description")
	}
	if ValidateCreate("news", "News", "", Kind(5)) != ErrBadKind {
		t.Fatal("bad kind")
	}
}

func TestValidatePostAndComment(t *testing.T) {
	if ValidatePost("hello") != nil || ValidatePost("") != ErrBadPost || ValidatePost(strings.Repeat("x", MaxPostBody+1)) != ErrBadPost {
		t.Fatal("post body bounds")
	}
	if ValidateComment("nice") != nil || ValidateComment("") != ErrBadComment || ValidateComment(strings.Repeat("x", MaxCommentBody+1)) != ErrBadComment {
		t.Fatal("comment body bounds")
	}
}

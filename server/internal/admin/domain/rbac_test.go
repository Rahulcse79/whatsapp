package domain

import "testing"

func TestRoleAtLeast(t *testing.T) {
	// The ladder is strictly ordered; AtLeast is "≥".
	if !RoleOwner.AtLeast(RoleAgent) {
		t.Error("owner should satisfy an agent gate")
	}
	if !RoleAgent.AtLeast(RoleAgent) {
		t.Error("a role should satisfy its own gate (≥, not >)")
	}
	if RoleViewer.AtLeast(RoleOperator) {
		t.Error("viewer must not satisfy an operator gate")
	}
	if RoleAgent.AtLeast(RoleOwner) {
		t.Error("agent must not satisfy an owner gate")
	}
}

func TestParseRole(t *testing.T) {
	for s, want := range map[string]Role{
		"viewer": RoleViewer, "agent": RoleAgent, "operator": RoleOperator, "owner": RoleOwner,
	} {
		got, ok := ParseRole(s)
		if !ok || got != want {
			t.Errorf("ParseRole(%q) = %v, %v; want %v, true", s, got, ok, want)
		}
	}
	for _, bad := range []string{"", "root", "admin", "Owner", "superuser"} {
		if _, ok := ParseRole(bad); ok {
			t.Errorf("ParseRole(%q) accepted an unknown role", bad)
		}
	}
}

func TestResolutionSemantics(t *testing.T) {
	cases := []struct {
		res   Resolution
		valid bool
		final ReportState
		min   Role
	}{
		{Dismiss, true, ReportDismissed, RoleAgent},
		{Warn, true, ReportActioned, RoleAgent},
		{Suspend, true, ReportActioned, RoleOperator}, // suspension is operator-level
		{Resolution("nuke"), false, 0, 0},
	}
	for _, c := range cases {
		if got := c.res.Valid(); got != c.valid {
			t.Errorf("%q.Valid() = %v, want %v", c.res, got, c.valid)
		}
		if !c.valid {
			continue
		}
		if got := c.res.FinalState(); got != c.final {
			t.Errorf("%q.FinalState() = %v, want %v", c.res, got, c.final)
		}
		if got := c.res.MinRole(); got != c.min {
			t.Errorf("%q.MinRole() = %v, want %v", c.res, got, c.min)
		}
	}
}

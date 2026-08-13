// Package domain holds the admin plane's pure logic: the RBAC role lattice and
// the report state machine (HLD §15.6, security-architecture §4). No I/O. E2EE
// bounds what an admin can ever do — there is no content here to read, only
// metadata and trust-and-safety actions, every mutation of which is audited.
package domain

// Role is the least-privilege ladder: viewer → T&S agent → operator → owner.
// Higher is more privileged; a check is "at least this role".
type Role int16

const (
	RoleViewer   Role = 0 // read dashboards + reports
	RoleAgent    Role = 1 // action reports (dismiss/warn)
	RoleOperator Role = 2 // suspend/reactivate users, action-with-suspend
	RoleOwner    Role = 3 // audit review + everything
)

// ParseRole maps an OIDC role claim to a Role (the IdP owns admin membership).
func ParseRole(s string) (Role, bool) {
	switch s {
	case "viewer":
		return RoleViewer, true
	case "agent":
		return RoleAgent, true
	case "operator":
		return RoleOperator, true
	case "owner":
		return RoleOwner, true
	default:
		return RoleViewer, false
	}
}

func (r Role) String() string {
	switch r {
	case RoleViewer:
		return "viewer"
	case RoleAgent:
		return "agent"
	case RoleOperator:
		return "operator"
	case RoleOwner:
		return "owner"
	default:
		return "unknown"
	}
}

// AtLeast reports whether this role meets a minimum (RBAC gate).
func (r Role) AtLeast(min Role) bool { return r >= min }

// ReportState is the trust-and-safety report lifecycle (reports.state).
type ReportState int16

const (
	ReportOpen      ReportState = 0
	ReportActioned  ReportState = 1
	ReportDismissed ReportState = 2
)

// Resolution is how an agent closes a report.
type Resolution string

const (
	Dismiss Resolution = "dismiss" // no wrongdoing → dismissed
	Warn    Resolution = "warn"    // warn the target → actioned, no suspension
	Suspend Resolution = "suspend" // suspend the target → actioned + status change
)

// Valid reports whether s is a known resolution.
func (res Resolution) Valid() bool {
	return res == Dismiss || res == Warn || res == Suspend
}

// FinalState is the report state a resolution moves to.
func (res Resolution) FinalState() ReportState {
	if res == Dismiss {
		return ReportDismissed
	}
	return ReportActioned
}

// MinRole is the least privilege a resolution requires: dismissing/warning is
// agent-level; suspending a user is operator-level.
func (res Resolution) MinRole() Role {
	if res == Suspend {
		return RoleOperator
	}
	return RoleAgent
}

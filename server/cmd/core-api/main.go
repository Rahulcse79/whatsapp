// Command core-api is the modular-monolith deployable: auth, users/contacts,
// chat, groups, call-control + PTT, stories, presence logic, and admin.
//
// LLD: Docs/05-services/core-api-lld.md
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "core-api: scaffold only — implementation starts at T0.05 (Docs/12-planning/task-breakdown.md)")
	os.Exit(1)
}

// Command core-api is the modular-monolith deployable: auth, users/contacts,
// chat, groups, call-control + PTT, stories, presence logic, and admin.
//
// LLD: Docs/05-services/core-api-lld.md
package main

import (
	"fmt"
	"os"
)

// Stamped by CI at release: -ldflags "-X main.version=… -X main.commit=…".
var (
	version = "dev"
	commit  = "none"
)

func main() {
	fmt.Fprintf(os.Stderr, "core-api %s (%s): scaffold only — implementation starts at T0.05 (Docs/12-planning/task-breakdown.md)\n", version, commit)
	os.Exit(1)
}

// Command ws-gateway is the stateless WebSocket tier: connection registry,
// session resume, frame routing (~20k connections per pod).
//
// LLD: Docs/05-services/ws-gateway-lld.md
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
	fmt.Fprintf(os.Stderr, "ws-gateway %s (%s): scaffold only — implementation starts at T0.10 (Docs/12-planning/task-breakdown.md)\n", version, commit)
	os.Exit(1)
}

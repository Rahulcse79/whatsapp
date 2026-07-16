// Command ws-gateway is the stateless WebSocket tier: connection registry,
// session resume, frame routing (~20k connections per pod).
//
// LLD: Docs/05-services/ws-gateway-lld.md
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "ws-gateway: scaffold only — implementation starts at T0.10 (Docs/12-planning/task-breakdown.md)")
	os.Exit(1)
}

// Command notification-svc consumes the push.dispatch stream and delivers
// wake signals via FCM / APNs / ntfy / WebPush. Payloads never contain
// plaintext content (FR-NOTIF-01).
//
// LLD: Docs/05-services/notification-svc-lld.md
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
	fmt.Fprintf(os.Stderr, "notification-svc %s (%s): scaffold only — implementation starts at T0.16 (Docs/12-planning/task-breakdown.md)\n", version, commit)
	os.Exit(1)
}

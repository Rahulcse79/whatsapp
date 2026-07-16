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

func main() {
	fmt.Fprintln(os.Stderr, "notification-svc: scaffold only — implementation starts at T0.16 (Docs/12-planning/task-breakdown.md)")
	os.Exit(1)
}

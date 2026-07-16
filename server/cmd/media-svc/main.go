// Command media-svc orchestrates uploads: presigned multipart URLs, quotas,
// completion verification, and garbage collection against MinIO.
//
// LLD: Docs/05-services/media-svc-lld.md
package main

import (
	"fmt"
	"os"
)

func main() {
	fmt.Fprintln(os.Stderr, "media-svc: scaffold only — implementation starts at T1.04 (Docs/12-planning/task-breakdown.md)")
	os.Exit(1)
}

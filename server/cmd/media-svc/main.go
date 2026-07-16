// Command media-svc orchestrates uploads: presigned multipart URLs, quotas,
// completion verification, and garbage collection against MinIO.
//
// LLD: Docs/05-services/media-svc-lld.md
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
	fmt.Fprintf(os.Stderr, "media-svc %s (%s): scaffold only — implementation starts at T1.04 (Docs/12-planning/task-breakdown.md)\n", version, commit)
	os.Exit(1)
}

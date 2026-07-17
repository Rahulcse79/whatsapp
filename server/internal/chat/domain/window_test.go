package domain

import (
	"testing"
	"time"

	"github.com/whatsapp-v2/server/internal/platform/id"
)

func TestCheckOverlayWindow(t *testing.T) {
	target := id.NewUUID()
	sent := id.TimeOf(target)
	ts := target.String()

	cases := []struct {
		name       string
		edit, del  bool
		targetUUID string
		now        time.Time
		want       WindowResult
	}{
		{"edit within window", true, false, ts, sent.Add(14 * time.Minute), WindowOK},
		{"edit at boundary ok", true, false, ts, sent.Add(15 * time.Minute), WindowOK},
		{"edit past window", true, false, ts, sent.Add(15*time.Minute + time.Second), WindowEditClosed},
		{"delete within window", false, true, ts, sent.Add(47 * time.Hour), WindowOK},
		{"delete past window", false, true, ts, sent.Add(48*time.Hour + time.Minute), WindowDeleteClosed},
		{"reaction has no window", false, false, ts, sent.Add(1000 * time.Hour), WindowOK},
		{"edit bad target", true, false, "not-a-uuid", sent, WindowBadTarget},
		{"non-overlay ignores target", false, false, "", sent, WindowOK},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := CheckOverlayWindow(tc.edit, tc.del, tc.targetUUID, tc.now); got != tc.want {
				t.Fatalf("got %v, want %v", got, tc.want)
			}
		})
	}
}

package domain

import (
	"reflect"
	"testing"
)

func TestValidate(t *testing.T) {
	ok := DefaultPrefs()
	if err := Validate(ok); err != nil {
		t.Fatalf("default prefs should validate: %v", err)
	}
	// unknown channel bit
	bad := ok
	bad.Channels = 1 << 6
	if err := Validate(bad); err != ErrBadChannels {
		t.Fatalf("want ErrBadChannels, got %v", err)
	}
	// only one quiet endpoint set
	half := ok
	half.QuietStart = 60
	half.QuietEnd = -1
	if err := Validate(half); err != ErrBadQuiet {
		t.Fatalf("want ErrBadQuiet for half-open window, got %v", err)
	}
	// out-of-range minute
	oor := ok
	oor.QuietStart = 0
	oor.QuietEnd = 1440
	if err := Validate(oor); err != ErrBadQuiet {
		t.Fatalf("want ErrBadQuiet for minute 1440, got %v", err)
	}
}

func TestInQuietHours(t *testing.T) {
	same := Prefs{QuietStart: 540, QuietEnd: 1020} // 09:00–17:00 same-day
	wrap := Prefs{QuietStart: 1320, QuietEnd: 420} // 22:00–07:00 overnight
	off := Prefs{QuietStart: -1, QuietEnd: -1}
	allDay := Prefs{QuietStart: 300, QuietEnd: 300}

	cases := []struct {
		name string
		p    Prefs
		min  int
		want bool
	}{
		{"same-day inside", same, 600, true},
		{"same-day before", same, 480, false},
		{"same-day at end is exclusive", same, 1020, false},
		{"wrap late night", wrap, 1380, true},  // 23:00
		{"wrap early morning", wrap, 60, true}, // 01:00
		{"wrap midday out", wrap, 720, false},  // 12:00
		{"off never", off, 720, false},
		{"all-day always", allDay, 0, true},
	}
	for _, c := range cases {
		if got := c.p.InQuietHours(c.min); got != c.want {
			t.Errorf("%s: InQuietHours(%d)=%v want %v", c.name, c.min, got, c.want)
		}
	}
}

func TestRoute(t *testing.T) {
	p := Prefs{Channels: ChannelPush | ChannelEmail | ChannelDesktop, QuietStart: 1320, QuietEnd: 420} // quiet 22:00–07:00
	const now = int64(1_000_000)

	// Normal message, awake hours, not snoozed → all enabled channels, ordered.
	got := Route(p, 0, now, 720, KindMessage)
	want := []Channel{ChannelPush, ChannelEmail, ChannelDesktop}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("awake message: got %v want %v", got, want)
	}

	// Snoozed conversation (mutedUntil in the future) → nothing.
	if got := Route(p, now+1, now, 720, KindMessage); got != nil {
		t.Fatalf("snoozed message should route nowhere, got %v", got)
	}

	// Quiet hours → nothing for a message.
	if got := Route(p, 0, now, 60, KindMessage); got != nil {
		t.Fatalf("quiet-hours message should route nowhere, got %v", got)
	}

	// A CALL breaks through quiet hours AND snooze.
	if got := Route(p, now+1, now, 60, KindCall); !reflect.DeepEqual(got, want) {
		t.Fatalf("call must break through, got %v want %v", got, want)
	}
}

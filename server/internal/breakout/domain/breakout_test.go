package domain

import "testing"

func TestValidateEgressTarget(t *testing.T) {
	cases := []struct {
		kind   EgressKind
		target string
		ok     bool
	}{
		{EgressRTMP, "rtmp://a.rtmp.youtube.com/live2/key", true},
		{EgressRTMP, "rtmps://live.twitch.tv/app/key", true},
		{EgressRTMP, "https://not-rtmp/x", false},
		{EgressRTMP, "", false},
		{EgressRTMP, "rtmp://", false}, // no host
		{EgressHLS, "https://cdn.example.com/live/index", true},
		{EgressHLS, "s3://bucket/prefix", true},
		{EgressHLS, "rtmp://x/y", false},
		{EgressHLS, "", false},
	}
	for _, c := range cases {
		err := ValidateEgressTarget(c.kind, c.target)
		if (err == nil) != c.ok {
			t.Errorf("ValidateEgressTarget(%v, %q) err=%v, want ok=%v", c.kind, c.target, err, c.ok)
		}
	}
}

func TestValidateRoom(t *testing.T) {
	if err := ValidateRoomName(""); err == nil {
		t.Error("blank room name should fail")
	}
	if err := ValidateRoomName("Group A"); err != nil {
		t.Errorf("valid name rejected: %v", err)
	}
	if err := ValidateRoomCount(0); err == nil {
		t.Error("0 rooms should fail")
	}
	if err := ValidateRoomCount(MaxBreakoutRooms + 1); err == nil {
		t.Error("over-max rooms should fail")
	}
	if err := ValidateRoomCount(4); err != nil {
		t.Errorf("valid count rejected: %v", err)
	}
}

func TestParseEgressKindAndStrings(t *testing.T) {
	if k, ok := ParseEgressKind("hls"); !ok || k != EgressHLS {
		t.Fatal("parse hls")
	}
	if _, ok := ParseEgressKind("srt"); ok {
		t.Fatal("unknown kind should fail")
	}
	if EgressLive.String() != "live" || EgressOff.String() != "off" {
		t.Fatal("egress state strings")
	}
	if RecordingRequested.String() != "requested" || RecordingActive.String() != "active" || RecordingOff.String() != "off" {
		t.Fatal("recording state strings")
	}
}

func TestTally(t *testing.T) {
	got := Tally([]Decision{
		{Decided: true, Consented: true},
		{Decided: true, Consented: false},
		{Decided: false},
		{Decided: true, Consented: true},
	})
	if got.Total != 4 || got.Consented != 2 || got.Declined != 1 || got.Pending != 1 {
		t.Fatalf("tally: %+v", got)
	}
}

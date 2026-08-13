package crash

import (
	"context"
	"errors"
	"math"
	"strings"
	"testing"
)

func TestCrashFreeTracker(t *testing.T) {
	tr := NewCrashFreeTracker()
	if tr.Ratio() != 1.0 {
		t.Fatalf("no data → ratio %v, want 1.0", tr.Ratio())
	}
	tr.Observe(100, 5)
	if tr.Ratio() != 0.95 {
		t.Fatalf("ratio = %v, want 0.95", tr.Ratio())
	}
	tr.Observe(100, 0) // cumulative: 200 sessions, 5 crashed
	if math.Abs(tr.Ratio()-0.975) > 1e-9 {
		t.Fatalf("ratio = %v, want 0.975", tr.Ratio())
	}
}

func TestCrashFreeTracker_ClampsBadInput(t *testing.T) {
	tr := NewCrashFreeTracker()
	tr.Observe(10, 50) // crashed > sessions → clamped to 10 → 0% crash-free
	if tr.Ratio() != 0.0 {
		t.Fatalf("ratio = %v, want 0.0 (clamped)", tr.Ratio())
	}
}

type capture struct {
	msg  string
	tags map[string]string
	sent bool
}

func (c *capture) Send(_ context.Context, message string, tags map[string]string) {
	c.msg, c.tags, c.sent = message, tags, true
}

func TestScrubbingReporter_RedactsBeforeSend(t *testing.T) {
	cap := &capture{}
	r := NewReporter(cap)

	r.Capture(context.Background(), errors.New("login failed for +14155550123"),
		map[string]string{"authorization": "Bearer s3cr3t", "screen": "login"})

	if !cap.sent {
		t.Fatal("nothing sent")
	}
	if strings.Contains(cap.msg, "+14155550123") {
		t.Errorf("message leaks the phone number: %q", cap.msg)
	}
	if !strings.Contains(cap.msg, "[redacted-phone]") {
		t.Errorf("message not scrubbed: %q", cap.msg)
	}
	if cap.tags["authorization"] != "[redacted]" {
		t.Errorf("authorization tag not dropped: %q", cap.tags["authorization"])
	}
	if cap.tags["screen"] != "login" {
		t.Errorf("benign tag altered: %q", cap.tags["screen"])
	}
}

func TestScrubbingReporter_NilErrorIsNoOp(t *testing.T) {
	cap := &capture{}
	NewReporter(cap).Capture(context.Background(), nil, nil)
	if cap.sent {
		t.Error("a nil error should not be reported")
	}
}

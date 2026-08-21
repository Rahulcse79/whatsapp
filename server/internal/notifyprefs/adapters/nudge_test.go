package adapters

import (
	"context"
	"strings"
	"testing"

	"github.com/whatsapp-v2/server/internal/notifyprefs"
	"github.com/whatsapp-v2/server/internal/notifyprefs/domain"
)

type fakeMail struct {
	to, subject, body string
	calls             int
}

func (m *fakeMail) SendMail(_ context.Context, to, subject, body string) error {
	m.to, m.subject, m.body, m.calls = to, subject, body, m.calls+1
	return nil
}

type fakeSMS struct {
	to, text string
	calls    int
}

func (g *fakeSMS) SendSMS(_ context.Context, to, text string) error {
	g.to, g.text, g.calls = to, text, g.calls+1
	return nil
}

func TestEmailNudgeIsContentFree(t *testing.T) {
	m := &fakeMail{}
	e := NewEmailNudgeSender(m)
	if e.Channel() != domain.ChannelEmail {
		t.Fatalf("channel: %v", e.Channel())
	}
	// The nudge title is a generic hint; the actual message ("secret plans") must
	// never reach the sender — only a generic "new activity" body does.
	if err := e.Send(context.Background(), "alice@example.com", notifyprefs.Nudge{Kind: domain.KindMessage, Title: "New messages"}); err != nil {
		t.Fatal(err)
	}
	if m.to != "alice@example.com" || m.calls != 1 {
		t.Fatalf("mail not sent to recipient: %+v", m)
	}
	if strings.Contains(m.body, "secret") {
		t.Fatal("nudge body must be content-free")
	}
	if !strings.Contains(strings.ToLower(m.body), "open the app") {
		t.Fatalf("expected a generic open-the-app nudge, got %q", m.body)
	}
}

func TestSMSNudgeIsContentFree(t *testing.T) {
	g := &fakeSMS{}
	s := NewSMSNudgeSender(g)
	if s.Channel() != domain.ChannelSMS {
		t.Fatalf("channel: %v", s.Channel())
	}
	if err := s.Send(context.Background(), "+15551234567", notifyprefs.Nudge{Kind: domain.KindMessage}); err != nil {
		t.Fatal(err)
	}
	if g.to != "+15551234567" || g.calls != 1 {
		t.Fatalf("sms not sent: %+v", g)
	}
	if !strings.Contains(strings.ToLower(g.text), "open the app") {
		t.Fatalf("expected a generic nudge text, got %q", g.text)
	}
}

func TestNudgeSendersRefuseWhenUnconfigured(t *testing.T) {
	if err := (&EmailNudgeSender{}).Send(context.Background(), "a@b.c", notifyprefs.Nudge{}); err == nil {
		t.Fatal("email nudge with no sender should error, not silently drop")
	}
	if err := (&SMSNudgeSender{}).Send(context.Background(), "+1555", notifyprefs.Nudge{}); err == nil {
		t.Fatal("sms nudge with no gateway should error, not silently drop")
	}
}

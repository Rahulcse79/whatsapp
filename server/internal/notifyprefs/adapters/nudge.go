package adapters

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/smtp"
	"time"

	"github.com/whatsapp-v2/server/internal/notifyprefs"
	"github.com/whatsapp-v2/server/internal/notifyprefs/domain"
)

// nudgeHTTPClient is shared by HTTP-based nudge senders with a tight timeout — a
// slow gateway must fail fast, not pin a worker.
var nudgeHTTPClient = &http.Client{Timeout: 5 * time.Second}

// ── Email nudge (SMTP) ─────────────────────────────────────────────────────

// MailSender is the minimal SMTP seam so the driver stays dependency-free and
// testable. The default adapter wraps net/smtp.SendMail.
type MailSender interface {
	SendMail(ctx context.Context, to string, subject, body string) error
}

// SMTPMailSender sends via a configured SMTP relay. Credentials/host come from
// deployment config; the offline profile leaves this unconfigured (email off).
type SMTPMailSender struct {
	Addr string    // host:port
	From string    // envelope + header From
	Auth smtp.Auth // may be nil for an unauthenticated relay
}

func (m SMTPMailSender) SendMail(_ context.Context, to, subject, body string) error {
	msg := []byte(fmt.Sprintf("From: %s\r\nTo: %s\r\nSubject: %s\r\n\r\n%s\r\n", m.From, to, subject, body))
	return smtp.SendMail(m.Addr, m.Auth, m.From, []string{to}, msg)
}

// EmailNudgeSender delivers a CONTENT-FREE email nudge ("you have new activity —
// open the app"). It never carries message content: the E2EE invariant holds.
type EmailNudgeSender struct{ mail MailSender }

func NewEmailNudgeSender(mail MailSender) *EmailNudgeSender { return &EmailNudgeSender{mail: mail} }

func (EmailNudgeSender) Channel() domain.Channel { return domain.ChannelEmail }

func (e EmailNudgeSender) Send(ctx context.Context, destination string, n notifyprefs.Nudge) error {
	if e.mail == nil {
		return errors.New("email nudge: no mail sender configured")
	}
	subject := n.Title
	if subject == "" {
		subject = "New activity on WhatsApp V2"
	}
	// Body is deliberately generic — a wake nudge, never message content.
	body := "You have new activity on WhatsApp V2. Open the app to view your messages.\n\n" +
		"(For your privacy, notifications never include message content.)"
	return e.mail.SendMail(ctx, destination, subject, body)
}

// ── SMS nudge (HTTP gateway) ───────────────────────────────────────────────

// SMSGateway is the seam over an SMS provider's REST API. The default adapter
// POSTs a small JSON body to a configured endpoint with a bearer token.
type SMSGateway interface {
	SendSMS(ctx context.Context, toE164, text string) error
}

// HTTPSMSGateway posts {to, text} to a configured SMS provider endpoint. The
// exact provider shape is deployment config; this is a generic REST adapter.
type HTTPSMSGateway struct {
	Endpoint string
	Token    string // bearer token; empty for an open gateway
	client   *http.Client
}

func NewHTTPSMSGateway(endpoint, token string) *HTTPSMSGateway {
	return &HTTPSMSGateway{Endpoint: endpoint, Token: token, client: nudgeHTTPClient}
}

func (g HTTPSMSGateway) SendSMS(ctx context.Context, toE164, text string) error {
	body, _ := json.Marshal(map[string]string{"to": toE164, "text": text})
	//nolint:gosec // G107: endpoint is trusted deployment config, not user taint.
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, g.Endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("sms request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if g.Token != "" {
		req.Header.Set("Authorization", "Bearer "+g.Token)
	}
	resp, err := g.client.Do(req)
	if err != nil {
		return fmt.Errorf("sms send: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("sms status %d", resp.StatusCode)
	}
	return nil
}

// SMSNudgeSender delivers a CONTENT-FREE SMS nudge to a phone number. Like the
// email nudge it carries no message content — only "open the app".
type SMSNudgeSender struct{ gw SMSGateway }

func NewSMSNudgeSender(gw SMSGateway) *SMSNudgeSender { return &SMSNudgeSender{gw: gw} }

func (SMSNudgeSender) Channel() domain.Channel { return domain.ChannelSMS }

func (s SMSNudgeSender) Send(ctx context.Context, destination string, _ notifyprefs.Nudge) error {
	if s.gw == nil {
		return errors.New("sms nudge: no gateway configured")
	}
	return s.gw.SendSMS(ctx, destination, "You have new activity on WhatsApp V2. Open the app to view your messages.")
}

var (
	_ notifyprefs.NudgeSender = (*EmailNudgeSender)(nil)
	_ notifyprefs.NudgeSender = (*SMSNudgeSender)(nil)
)

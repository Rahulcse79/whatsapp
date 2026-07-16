package adapters

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/smtp"
	"sync"

	"github.com/whatsapp-v2/server/internal/auth/domain"
)

// MockSender is the dev-profile OTP driver: it logs and records codes
// instead of sending them. Config forbids it in prod (WA_OTP_CHANNEL guard).
type MockSender struct {
	mu    sync.Mutex
	codes map[string]string
	log   *slog.Logger
}

func NewMockSender(log *slog.Logger) *MockSender {
	return &MockSender{codes: map[string]string{}, log: log}
}

func (m *MockSender) Send(_ context.Context, destination, code string) error {
	m.mu.Lock()
	m.codes[destination] = code
	m.mu.Unlock()
	// Logging a code is banned in prod; this IS the mock's delivery
	// mechanism and the channel is config-blocked outside dev.
	m.log.Info("OTP code (mock sender — dev only)", "destination", destination, "code", code)
	return nil
}

func (m *MockSender) Channel() domain.Channel { return domain.ChannelMock }

// LastCode returns the most recent code sent to destination (dev/test aid).
func (m *MockSender) LastCode(destination string) string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.codes[destination]
}

// SMSSender is the production phone-OTP driver. The provider choice
// (Twilio / MSG91 / SNS) is an open P0 decision (HLD §24) — until it lands,
// this driver refuses loudly instead of pretending.
type SMSSender struct{}

func (SMSSender) Send(context.Context, string, string) error {
	return errors.New("sms provider not configured: pick and wire the provider (HLD §24 open risk); use WA_OTP_CHANNEL=email or =mock meanwhile")
}

func (SMSSender) Channel() domain.Channel { return domain.ChannelSMS }

// EmailSender delivers codes over SMTP — the offline-profile channel
// (HLD §17.5: self-hosted Stalwart/Postfix).
type EmailSender struct {
	Host     string
	Port     int
	From     string
	Username string // empty = no auth (trusted LAN relay)
	Password string
}

func (e EmailSender) Send(_ context.Context, destination, code string) error {
	msg := []byte(fmt.Sprintf(
		"From: %s\r\nTo: %s\r\nSubject: Your verification code\r\n\r\n"+
			"Your WhatsApp V2 verification code is: %s\r\nIt expires in 10 minutes.\r\n",
		e.From, destination, code))
	var a smtp.Auth
	if e.Username != "" {
		a = smtp.PlainAuth("", e.Username, e.Password, e.Host)
	}
	addr := fmt.Sprintf("%s:%d", e.Host, e.Port)
	if err := smtp.SendMail(addr, a, e.From, []string{destination}, msg); err != nil {
		return fmt.Errorf("smtp send to %s: %w", addr, err)
	}
	return nil
}

func (EmailSender) Channel() domain.Channel { return domain.ChannelEmail }

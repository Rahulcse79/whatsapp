package config

import (
	"strings"
	"testing"
	"time"
)

func TestLoad_DevDefaults(t *testing.T) {
	c, err := Load("core-api")
	if err != nil {
		t.Fatalf("Load with clean env: unexpected error: %v", err)
	}
	if c.Env != "dev" {
		t.Errorf("Env = %q, want dev", c.Env)
	}
	if c.PG.MaxConns != 8 || c.PG.MinConns != 1 {
		t.Errorf("PG pool defaults = %d/%d, want 8/1", c.PG.MaxConns, c.PG.MinConns)
	}
	if c.PG.StatementTimeout != 5*time.Second {
		t.Errorf("StatementTimeout = %v, want 5s", c.PG.StatementTimeout)
	}
	if c.Valkey.Addr != "localhost:6379" || c.NATS.URL != "nats://localhost:4222" {
		t.Errorf("infra defaults wrong: valkey=%q nats=%q", c.Valkey.Addr, c.NATS.URL)
	}
}

func TestLoad_EnvOverrides(t *testing.T) {
	t.Setenv("WA_ENV", "prod")
	t.Setenv("WA_PG_MAX_CONNS", "32")
	t.Setenv("WA_PG_STATEMENT_TIMEOUT", "250ms")
	t.Setenv("WA_VALKEY_ADDR", "valkey.data:6379")

	c, err := Load("ws-gateway")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if c.Env != "prod" || c.PG.MaxConns != 32 ||
		c.PG.StatementTimeout != 250*time.Millisecond || c.Valkey.Addr != "valkey.data:6379" {
		t.Errorf("overrides not applied: %+v", c)
	}
}

func TestLoad_CollectsAllErrors(t *testing.T) {
	t.Setenv("WA_ENV", "bogus")
	t.Setenv("WA_PG_MAX_CONNS", "not-a-number")
	t.Setenv("WA_PG_STATEMENT_TIMEOUT", "not-a-duration")

	_, err := Load("")
	if err == nil {
		t.Fatal("want joined error for 4 problems, got nil")
	}
	// errors.Join formats one problem per line; all four must be present.
	for _, want := range []string{"WA_ENV", "WA_PG_MAX_CONNS", "WA_PG_STATEMENT_TIMEOUT", "service name"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error %q missing mention of %s", err, want)
		}
	}
}

func TestLoad_MinConnsAboveMax(t *testing.T) {
	t.Setenv("WA_PG_MIN_CONNS", "9")
	if _, err := Load("core-api"); err == nil {
		t.Fatal("want error when min conns > max conns")
	}
}

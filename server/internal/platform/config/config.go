// Package config loads deployable configuration from WA_-prefixed
// environment variables (12-factor; no config in code). Every field has a
// dev-stack default so `dev` boots with zero configuration; production
// values come from Helm/SOPS-managed env.
package config

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"time"
)

// PG configures the PostgreSQL pool (via PgBouncer in production).
type PG struct {
	DSN              string
	MaxConns         int32
	MinConns         int32
	StatementTimeout time.Duration
}

// Valkey configures the cache/ephemeral tier.
type Valkey struct {
	Addr     string
	Password string
}

// NATS configures the JetStream connection.
type NATS struct {
	URL string
}

// Auth configures the auth context: secrets, token lifetimes, OTP channel.
type Auth struct {
	// PhonePepper keys the HMAC over phone numbers (threat-model T11).
	PhonePepper string
	// JWTEd25519Seed is a base64 32-byte seed; empty = ephemeral key
	// (dev only — restarts invalidate outstanding tokens).
	JWTEd25519Seed string
	AccessTTL      time.Duration
	RefreshTTL     time.Duration
	// OTPChannel: mock (dev) | sms | email (offline profile, HLD §17.5).
	OTPChannel string
}

// Config is the common configuration shared by all deployables.
type Config struct {
	// Service is the deployable name (core-api, ws-gateway, …); set by main,
	// not by env.
	Service  string
	Env      string // dev | staging | prod | offline
	LogLevel string // debug | info | warn | error
	HTTPAddr string
	GRPCAddr string

	PG     PG
	Valkey Valkey
	NATS   NATS
	Auth   Auth
}

// Load reads configuration from the environment, applying dev defaults.
// All parse failures are collected and returned as one joined error so a
// misconfigured pod reports every problem at once instead of one per restart.
func Load(service string) (*Config, error) {
	var errs []error

	c := &Config{
		Service:  service,
		Env:      getStr("WA_ENV", "dev"),
		LogLevel: getStr("WA_LOG_LEVEL", "info"),
		HTTPAddr: getStr("WA_HTTP_ADDR", ":8080"),
		GRPCAddr: getStr("WA_GRPC_ADDR", ":9090"),
		PG: PG{
			DSN:              getStr("WA_PG_DSN", "postgres://whatsapp:devpassword@localhost:5432/whatsapp?sslmode=disable"),
			MaxConns:         int32(getInt("WA_PG_MAX_CONNS", 8, &errs)),
			MinConns:         int32(getInt("WA_PG_MIN_CONNS", 1, &errs)),
			StatementTimeout: getDur("WA_PG_STATEMENT_TIMEOUT", 5*time.Second, &errs),
		},
		Valkey: Valkey{
			Addr:     getStr("WA_VALKEY_ADDR", "localhost:6379"),
			Password: os.Getenv("WA_VALKEY_PASSWORD"),
		},
		NATS: NATS{
			URL: getStr("WA_NATS_URL", "nats://localhost:4222"),
		},
		Auth: Auth{
			PhonePepper:    getStr("WA_PHONE_PEPPER", devPepper),
			JWTEd25519Seed: os.Getenv("WA_JWT_ED25519_SEED"),
			AccessTTL:      getDur("WA_ACCESS_TTL", 10*time.Minute, &errs),
			RefreshTTL:     getDur("WA_REFRESH_TTL", 90*24*time.Hour, &errs),
			OTPChannel:     getStr("WA_OTP_CHANNEL", "mock"),
		},
	}

	if service == "" {
		errs = append(errs, errors.New("config: service name must not be empty"))
	}
	switch c.Env {
	case "dev", "staging", "prod", "offline":
	default:
		errs = append(errs, fmt.Errorf("config: WA_ENV %q is not one of dev|staging|prod|offline", c.Env))
	}
	if c.PG.MinConns > c.PG.MaxConns {
		errs = append(errs, fmt.Errorf("config: WA_PG_MIN_CONNS (%d) exceeds WA_PG_MAX_CONNS (%d)", c.PG.MinConns, c.PG.MaxConns))
	}
	switch c.Auth.OTPChannel {
	case "mock", "sms", "email":
	default:
		errs = append(errs, fmt.Errorf("config: WA_OTP_CHANNEL %q is not one of mock|sms|email", c.Auth.OTPChannel))
	}
	// Production must never run on development secrets.
	if c.Env == "prod" {
		if c.Auth.PhonePepper == devPepper {
			errs = append(errs, errors.New("config: WA_PHONE_PEPPER must be set in prod"))
		}
		if c.Auth.JWTEd25519Seed == "" {
			errs = append(errs, errors.New("config: WA_JWT_ED25519_SEED must be set in prod"))
		}
		if c.Auth.OTPChannel == "mock" {
			errs = append(errs, errors.New("config: WA_OTP_CHANNEL=mock is not allowed in prod"))
		}
	}

	return c, errors.Join(errs...)
}

// devPepper is intentionally obvious so it can never be mistaken for a secret.
const devPepper = "dev-pepper-not-for-production"

func getStr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getInt(key string, def int, errs *[]error) int {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		*errs = append(*errs, fmt.Errorf("config: %s=%q is not an integer", key, v))
		return def
	}
	return n
}

func getDur(key string, def time.Duration, errs *[]error) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		*errs = append(*errs, fmt.Errorf("config: %s=%q is not a duration (e.g. 5s, 250ms)", key, v))
		return def
	}
	return d
}

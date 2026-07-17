// Command core-api is the modular-monolith deployable: auth, users/contacts,
// chat, groups, call-control + PTT, stories, and admin.
//
// LLD: Docs/05-services/core-api-lld.md
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/whatsapp-v2/server/internal/auth"
	authadapters "github.com/whatsapp-v2/server/internal/auth/adapters"
	"github.com/whatsapp-v2/server/internal/auth/domain"
	"github.com/whatsapp-v2/server/internal/devices"
	devadapters "github.com/whatsapp-v2/server/internal/devices/adapters"
	"github.com/whatsapp-v2/server/internal/keys"
	keyadapters "github.com/whatsapp-v2/server/internal/keys/adapters"
	"github.com/whatsapp-v2/server/internal/platform/config"
	"github.com/whatsapp-v2/server/internal/platform/logging"
	"github.com/whatsapp-v2/server/internal/platform/natsx"
	"github.com/whatsapp-v2/server/internal/platform/pg"
	"github.com/whatsapp-v2/server/internal/platform/ratelimit"
	"github.com/whatsapp-v2/server/internal/platform/valkey"
)

// Stamped by CI at release: -ldflags "-X main.version=… -X main.commit=…".
var (
	version = "dev"
	commit  = "none"
)

func main() {
	cfg, cfgErr := config.Load("core-api")
	log := logging.New("core-api", cfg.LogLevel)
	if cfgErr != nil {
		log.Error("configuration invalid", "err", cfgErr)
		os.Exit(1)
	}
	log.Info("starting", "version", version, "commit", commit, "env", cfg.Env)

	ctx := context.Background()

	pool, err := pg.NewPool(ctx, pg.Config{
		DSN: cfg.PG.DSN, MaxConns: cfg.PG.MaxConns, MinConns: cfg.PG.MinConns,
		StatementTimeout: cfg.PG.StatementTimeout,
	})
	if err != nil {
		log.Error("postgres connect failed", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	vk, err := valkey.New(ctx, valkey.Config{Addr: cfg.Valkey.Addr, Password: cfg.Valkey.Password})
	if err != nil {
		log.Error("valkey connect failed", "err", err)
		os.Exit(1)
	}
	defer func() { _ = vk.Close() }()

	nc, _, err := natsx.Connect(natsx.Config{URL: cfg.NATS.URL, Name: "core-api"})
	if err != nil {
		log.Error("nats connect failed", "err", err)
		os.Exit(1)
	}
	defer nc.Close()

	issuer, err := buildIssuer(cfg, log)
	if err != nil {
		log.Error("building token issuer", "err", err)
		os.Exit(1)
	}
	limiter := ratelimit.NewValkeyLimiter(vk)

	// ── auth context ──────────────────────────────────────────────────────
	authStore := authadapters.NewStore(pool)
	sender, err := buildOTPSender(cfg, log)
	if err != nil {
		log.Error("building OTP sender", "err", err)
		os.Exit(1)
	}
	authSvc := auth.NewService(auth.Deps{
		Challenges: authStore.Challenges,
		Users:      authStore.Users,
		Sessions:   authStore.Sessions,
		Registrar:  authStore.Registrar,
		Sender:     sender,
		Limiter:    limiter,
		Attempts:   authStore.Attempts,
		Issuer:     issuer,
		Log:        log,
	}, cfg.Auth.PhonePepper, cfg.Auth.RefreshTTL)

	// ── keys context ──────────────────────────────────────────────────────
	keysSvc := keys.NewService(keyadapters.NewPrekeyStore(pool), limiter)

	// ── devices context ───────────────────────────────────────────────────
	devStore := devadapters.NewStore(pool)
	devEvents := devadapters.NewNATSEvents(nc, log)
	devSvc := devices.NewService(devStore, devStore, authSvc, devEvents)

	// Chat accept + live delivery are exercised over the gateway→core-api
	// gRPC path, wired when protobuf codegen lands (T0.13). The chat service
	// and its adapters already exist and are tested.

	mux := http.NewServeMux()
	auth.Routes(mux, authSvc)
	keys.Routes(mux, keysSvc, issuer)
	devices.Routes(mux, devSvc, issuer)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := pool.Ping(r.Context()); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	httpSrv := &http.Server{Addr: cfg.HTTPAddr, Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	go func() {
		log.Info("listening", "addr", cfg.HTTPAddr)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Error("http server failed", "err", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		log.Error("shutdown", "err", err)
	}
	log.Info("stopped")
}

func buildIssuer(cfg *config.Config, log *slog.Logger) (*auth.TokenIssuer, error) {
	if cfg.Auth.JWTEd25519Seed != "" {
		return auth.NewIssuerFromSeed(cfg.Auth.JWTEd25519Seed, "v1", cfg.Auth.AccessTTL)
	}
	log.Warn("no WA_JWT_ED25519_SEED — using an ephemeral key (dev only)")
	return auth.NewEphemeralIssuer(cfg.Auth.AccessTTL)
}

func buildOTPSender(cfg *config.Config, log *slog.Logger) (auth.Sender, error) {
	switch cfg.Auth.OTPChannel {
	case "mock":
		return authadapters.NewMockSender(log), nil
	case "sms":
		return authadapters.SMSSender{}, nil
	case "email":
		return authadapters.EmailSender{
			Host:     os.Getenv("WA_SMTP_HOST"),
			Port:     587,
			From:     os.Getenv("WA_SMTP_FROM"),
			Username: os.Getenv("WA_SMTP_USER"),
			Password: os.Getenv("WA_SMTP_PASSWORD"),
		}, nil
	default:
		return nil, errors.New("unknown OTP channel " + cfg.Auth.OTPChannel)
	}
}

// ensure the domain package is referenced (compile guard for future wiring).
var _ = domain.OTPValidity

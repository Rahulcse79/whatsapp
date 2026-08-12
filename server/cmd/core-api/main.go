// Command core-api is the modular-monolith deployable: auth, users/contacts,
// chat, groups, call-control + PTT, stories, and admin.
//
// LLD: Docs/05-services/core-api-lld.md
package main

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"google.golang.org/grpc"

	"github.com/whatsapp-v2/server/internal/auth"
	authadapters "github.com/whatsapp-v2/server/internal/auth/adapters"
	"github.com/whatsapp-v2/server/internal/auth/domain"
	"github.com/whatsapp-v2/server/internal/calls"
	callsadapters "github.com/whatsapp-v2/server/internal/calls/adapters"
	"github.com/whatsapp-v2/server/internal/chat"
	chatadapters "github.com/whatsapp-v2/server/internal/chat/adapters"
	"github.com/whatsapp-v2/server/internal/contacts"
	contactsadapters "github.com/whatsapp-v2/server/internal/contacts/adapters"
	"github.com/whatsapp-v2/server/internal/devices"
	devadapters "github.com/whatsapp-v2/server/internal/devices/adapters"
	"github.com/whatsapp-v2/server/internal/groups"
	groupadapters "github.com/whatsapp-v2/server/internal/groups/adapters"
	"github.com/whatsapp-v2/server/internal/keys"
	keyadapters "github.com/whatsapp-v2/server/internal/keys/adapters"
	"github.com/whatsapp-v2/server/internal/platform/config"
	"github.com/whatsapp-v2/server/internal/platform/logging"
	"github.com/whatsapp-v2/server/internal/platform/natsx"
	"github.com/whatsapp-v2/server/internal/platform/observability"
	"github.com/whatsapp-v2/server/internal/platform/pg"
	"github.com/whatsapp-v2/server/internal/platform/ratelimit"
	"github.com/whatsapp-v2/server/internal/platform/valkey"
	rpcv1 "github.com/whatsapp-v2/server/internal/proto/gen/whatsapp/rpc/v1"
	"github.com/whatsapp-v2/server/internal/ptt"
	pttadapters "github.com/whatsapp-v2/server/internal/ptt/adapters"
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

	tel, err := observability.Init(ctx, observability.Config{
		ServiceName: "core-api", ServiceVersion: version, Env: cfg.Env, OTLPEndpoint: cfg.OTelEndpoint,
	})
	if err != nil {
		log.Error("telemetry init failed", "err", err)
		os.Exit(1)
	}
	defer func() { _ = tel.Shutdown(context.Background()) }()
	httpMetrics, err := observability.NewHTTPMetrics(tel.Meter)
	if err != nil {
		log.Error("telemetry metrics init failed", "err", err)
		os.Exit(1)
	}

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

	// ── groups context ────────────────────────────────────────────────────
	groupsSvc := groups.NewService(groupadapters.NewStore(pool), groupadapters.NewNATSEvents(nc, log))

	// ── contacts context ──────────────────────────────────────────────────
	// authSvc supplies the peppered PhoneHash so registration and hashed sync
	// key identically (contacts.Hasher). One Store backs the discovery,
	// contact-edge, favorite, and invite ports.
	contactsStore := contactsadapters.NewStore(pool)
	inviteBase := os.Getenv("WA_PUBLIC_BASE_URL")
	if inviteBase == "" {
		inviteBase = "https://wa.local"
	}
	contactsSvc := contacts.NewService(
		authSvc, contactsStore, contactsStore, contactsStore, contactsStore,
		contactsadapters.NewSyncDailyLimiter(limiter), contactsadapters.NewSearchRate(limiter),
		inviteBase, log)

	// ── calls context (call control plane; media plane is LiveKit) ────────
	// LiveKit API key/secret sign join tokens + authenticate webhooks. Unset in
	// bare dev → empty secret (like the ephemeral JWT fallback); staging/prod
	// inject a Secret.
	liveKitKey := os.Getenv("WA_LIVEKIT_API_KEY")
	liveKitSecret := os.Getenv("WA_LIVEKIT_API_SECRET")
	if liveKitKey == "" || liveKitSecret == "" {
		log.Warn("WA_LIVEKIT_API_KEY/SECRET unset — call join tokens signed with an empty secret (dev only)")
	}
	callsSvc := calls.NewService(
		calls.NewTokenMinter(liveKitKey, liveKitSecret),
		callsadapters.NewRingStore(vk),
		callsadapters.NewHistory(pool),
		callsadapters.NewSignaler(nc),
		callsadapters.NewPusher(nc),
		callsadapters.NewDevices(pool),
		log,
	)
	callsWebhook := calls.NewWebhookVerifier(liveKitKey, liveKitSecret)
	// Server-authoritative missed timeout: sweep expired rings on a ticker.
	// Transitions are idempotent (domain.Next + ZREM), so overlap across pods is
	// harmless.
	go func() {
		t := time.NewTicker(5 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if _, err := callsSvc.SweepMissed(ctx, 200); err != nil {
					log.Warn("calls: missed-ring sweep failed", "err", err)
				}
			}
		}
	}()

	// Call-history retention: purge records past the 90-day window daily
	// (FR-CALL-06). A plain DELETE, so overlap across pods is harmless.
	go func() {
		t := time.NewTicker(24 * time.Hour)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if n, err := callsSvc.PurgeHistory(ctx); err != nil {
					log.Warn("calls: history purge failed", "err", err)
				} else if n > 0 {
					log.Info("calls: purged expired call history", "count", n)
				}
			}
		}
	}()

	// ── ptt context (push-to-talk floor control) ─────────────────────────
	// Atomic Valkey-Lua fenced floor + FIFO queue; SFU publish flip is the media-
	// plane seam (NoopSFU until the LiveKit RoomService client is wired).
	pttSvc := ptt.NewService(pttadapters.NewValkeyFloorStore(vk), pttadapters.NewNoopSFU(log), pttadapters.NewSignaler(nc), log)
	pttMinter := pttTokenMinter{m: calls.NewTokenMinter(liveKitKey, liveKitSecret)}
	// Floor lapse recovery: promote queue heads for rooms whose holder stopped
	// heartbeating (~every second; heartbeat is 500 ms). Idempotent across pods.
	go func() {
		t := time.NewTicker(time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if _, err := pttSvc.SweepAll(ctx); err != nil {
					log.Warn("ptt: floor sweep failed", "err", err)
				}
			}
		}
	}()

	// ── chat context (gateway-facing gRPC surface) ────────────────────────
	chatStore := chatadapters.NewStore(pool)
	chatPub := chatadapters.NewNATSPublisher(nc)
	chatSvc := chat.NewService(chatStore, chatStore,
		chatadapters.NewDeduper(vk), chatPub, log)
	chatSvc.SetReceipts(chatStore, chatPub)

	grpcSrv := grpc.NewServer(observability.GRPCServerOption())
	rpcv1.RegisterChatServiceServer(grpcSrv, chatadapters.NewChatGRPC(chatSvc, log))
	grpcLis, err := net.Listen("tcp", cfg.GRPCAddr)
	if err != nil {
		log.Error("grpc listen failed", "addr", cfg.GRPCAddr, "err", err)
		os.Exit(1)
	}
	go func() {
		log.Info("grpc listening", "addr", cfg.GRPCAddr)
		if err := grpcSrv.Serve(grpcLis); err != nil {
			log.Error("grpc server failed", "err", err)
			os.Exit(1)
		}
	}()

	mux := http.NewServeMux()
	mux.Handle("GET /metrics", tel.MetricsHandler())
	auth.Routes(mux, authSvc)
	keys.Routes(mux, keysSvc, issuer)
	devices.Routes(mux, devSvc, issuer)
	groups.Routes(mux, groupsSvc, issuer)
	contacts.Routes(mux, contactsSvc, issuer)
	calls.Routes(mux, callsSvc, issuer, callsWebhook)
	ptt.Routes(mux, pttSvc, pttMinter, issuer)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := pool.Ping(r.Context()); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	httpSrv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           observability.WrapHTTPHandler(httpMetrics.Middleware(mux), "http.server"),
		ReadHeaderTimeout: 10 * time.Second,
	}
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
	grpcDone := make(chan struct{})
	go func() {
		grpcSrv.GracefulStop() // finishes in-flight RPCs (incl. replay streams)
		close(grpcDone)
	}()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		log.Error("shutdown", "err", err)
	}
	select {
	case <-grpcDone:
	case <-shutdownCtx.Done():
		grpcSrv.Stop() // deadline passed; cut remaining streams
	}
	log.Info("stopped")
}

// pttTokenMinter adapts the LiveKit token minter to ptt.Minter: an audio-only,
// server-muted join token (canPublish=false — the floor grant flips publish via
// the SFU, so everyone joins muted).
type pttTokenMinter struct{ m *calls.TokenMinter }

func (p pttTokenMinter) Mint(room, identity string) (string, error) {
	return p.m.Mint(calls.JoinGrant{Identity: identity, Room: room, CanPublish: false, CanSubscribe: true}, 60*time.Second, time.Now())
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

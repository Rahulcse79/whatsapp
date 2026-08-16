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

	"github.com/whatsapp-v2/server/internal/abuse"
	abuseadapters "github.com/whatsapp-v2/server/internal/abuse/adapters"
	"github.com/whatsapp-v2/server/internal/admin"
	adminadapters "github.com/whatsapp-v2/server/internal/admin/adapters"
	"github.com/whatsapp-v2/server/internal/ai"
	"github.com/whatsapp-v2/server/internal/analytics"
	analyticsadapters "github.com/whatsapp-v2/server/internal/analytics/adapters"
	"github.com/whatsapp-v2/server/internal/auth"
	authadapters "github.com/whatsapp-v2/server/internal/auth/adapters"
	"github.com/whatsapp-v2/server/internal/auth/domain"
	"github.com/whatsapp-v2/server/internal/breakout"
	breakoutadapters "github.com/whatsapp-v2/server/internal/breakout/adapters"
	"github.com/whatsapp-v2/server/internal/calls"
	callsadapters "github.com/whatsapp-v2/server/internal/calls/adapters"
	"github.com/whatsapp-v2/server/internal/channels"
	channelsadapters "github.com/whatsapp-v2/server/internal/channels/adapters"
	"github.com/whatsapp-v2/server/internal/chat"
	chatadapters "github.com/whatsapp-v2/server/internal/chat/adapters"
	"github.com/whatsapp-v2/server/internal/communities"
	communitiesadapters "github.com/whatsapp-v2/server/internal/communities/adapters"
	"github.com/whatsapp-v2/server/internal/contacts"
	contactsadapters "github.com/whatsapp-v2/server/internal/contacts/adapters"
	"github.com/whatsapp-v2/server/internal/deviceauth"
	deviceauthadapters "github.com/whatsapp-v2/server/internal/deviceauth/adapters"
	"github.com/whatsapp-v2/server/internal/devices"
	devadapters "github.com/whatsapp-v2/server/internal/devices/adapters"
	"github.com/whatsapp-v2/server/internal/ephemeral"
	ephemeraladapters "github.com/whatsapp-v2/server/internal/ephemeral/adapters"
	"github.com/whatsapp-v2/server/internal/groups"
	groupadapters "github.com/whatsapp-v2/server/internal/groups/adapters"
	"github.com/whatsapp-v2/server/internal/keys"
	keyadapters "github.com/whatsapp-v2/server/internal/keys/adapters"
	"github.com/whatsapp-v2/server/internal/platform/config"
	"github.com/whatsapp-v2/server/internal/platform/flags"
	"github.com/whatsapp-v2/server/internal/platform/logging"
	"github.com/whatsapp-v2/server/internal/platform/natsx"
	"github.com/whatsapp-v2/server/internal/platform/observability"
	"github.com/whatsapp-v2/server/internal/platform/pg"
	"github.com/whatsapp-v2/server/internal/platform/ratelimit"
	"github.com/whatsapp-v2/server/internal/platform/valkey"
	"github.com/whatsapp-v2/server/internal/polls"
	pollsadapters "github.com/whatsapp-v2/server/internal/polls/adapters"
	"github.com/whatsapp-v2/server/internal/profile"
	profileadapters "github.com/whatsapp-v2/server/internal/profile/adapters"
	rpcv1 "github.com/whatsapp-v2/server/internal/proto/gen/whatsapp/rpc/v1"
	"github.com/whatsapp-v2/server/internal/ptt"
	pttadapters "github.com/whatsapp-v2/server/internal/ptt/adapters"
	"github.com/whatsapp-v2/server/internal/stories"
	storiesadapters "github.com/whatsapp-v2/server/internal/stories/adapters"
	"github.com/whatsapp-v2/server/internal/webinar"
	webinaradapters "github.com/whatsapp-v2/server/internal/webinar/adapters"
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
	analyticsMetrics, err := analytics.NewMetrics(tel.Meter)
	if err != nil {
		log.Error("analytics metrics init failed", "err", err)
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
	// Product-analytics emitter (metadata-only; HLD §18.1). Fire-and-forget — it
	// never blocks or fails a producing request. The aggregation consumer + rollup
	// tickers are wired below.
	analyticsPub := analyticsadapters.NewPublisher(nc)

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
		Analytics:  analyticsPub, // emits signup + active (DAU/MAU) — never per-user rows
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

	// ── stories context (status posts) ───────────────────────────────────
	// Audience is the author's contacts (frozen at post time); content is E2EE
	// with client-distributed per-story keys — the server holds ciphertext refs
	// + metadata only.
	storiesSvc := stories.NewService(storiesadapters.NewStore(pool), storiesadapters.NewAudience(pool))
	pollsSvc := polls.NewService(pollsadapters.NewStore(pool))
	channelsSvc := channels.NewService(channelsadapters.NewStore(pool), channelsadapters.NewNATSBroadcaster(nc, log), channelsadapters.NewNoopGateway(), log)
	// Communities roof over groups; the announcement group is minted through the
	// groups service via a thin GroupCreator adapter (keeps communities decoupled).
	communitiesSvc := communities.NewService(communitiesadapters.NewStore(pool), announcementGroupCreator{groups: groupsSvc})
	// Webinar/live mode: role-scoped LiveKit tokens (host/speaker publish;
	// attendees subscribe-only) via the same minter as calls/ptt.
	webinarSvc := webinar.NewService(webinaradapters.NewStore(pool), webinarMinter{m: calls.NewTokenMinter(liveKitKey, liveKitSecret)})
	// Advanced live sessions (T9.03): breakout rooms + streaming egress + recording
	// consent. Reuses the calls token minter (per-room join tokens); egress rides a
	// no-op driver in dev (the real LiveKit egress API is the seam).
	breakoutSvc := breakout.NewService(breakoutadapters.NewStore(pool), breakoutMinter{m: calls.NewTokenMinter(liveKitKey, liveKitSecret)}, breakoutadapters.NoopEgress{})
	// Disappearing messages (T10.01): a coarse per-conversation timer + a relay
	// backstop purge. The authoritative timer is E2EE (client-side); this is the
	// minimal server support.
	ephemeralSvc := ephemeral.NewService(ephemeraladapters.NewStore(pool))
	// Device-auth hardening (T10.02): WebAuthn passkeys (2FA/step-up) + login-event
	// audit. rp id/origin are the web app's WebAuthn relying-party identity (dev
	// defaults to localhost); staging/prod inject them.
	webauthnRPID := envOr("WA_WEBAUTHN_RP_ID", "localhost")
	webauthnOrigin := envOr("WA_WEBAUTHN_ORIGIN", "http://localhost:5173")
	deviceAuthSvc := deviceauth.NewService(deviceauthadapters.NewStore(pool), webauthnRPID, webauthnOrigin)
	// Anti-abuse (T10.03): file user reports into the T4.01 trust-and-safety queue,
	// rate-limited. Spam/phishing detection is on-device (E2EE); block lives in the
	// profile context.
	abuseSvc := abuse.NewService(abuseadapters.NewStore(pool), limiter)
	profileSvc := profile.NewService(profileadapters.NewStore(pool))
	// Scheduled-post sweep: flip due channel posts to published and broadcast
	// them. 15 s cadence; a plain UPDATE…RETURNING so overlap across pods only
	// costs a duplicate best-effort nudge, never a double publish.
	go func() {
		t := time.NewTicker(15 * time.Second)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if n, err := channelsSvc.PublishDue(ctx); err != nil {
					log.Warn("channels: publish-due sweep failed", "err", err)
				} else if n > 0 {
					log.Info("channels: published scheduled posts", "count", n)
				}
			}
		}
	}()
	// 24 h hard-expiry purge (MinIO ILM is the media backstop). Hourly, a plain
	// DELETE, so overlap across pods is harmless.
	go func() {
		t := time.NewTicker(time.Hour)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if n, err := storiesSvc.PurgeExpired(ctx); err != nil {
					log.Warn("stories: expiry purge failed", "err", err)
				} else if n > 0 {
					log.Info("stories: purged expired stories", "count", n)
				}
			}
		}
	}()

	// Disappearing-messages backstop (T10.01): purge relay ciphertext for chats
	// past their timer. Every 5 min; a plain DELETE, so pod overlap is harmless.
	go func() {
		t := time.NewTicker(5 * time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if n, err := ephemeralSvc.PurgeExpired(ctx); err != nil {
					log.Warn("ephemeral: disappearing purge failed", "err", err)
				} else if n > 0 {
					log.Info("ephemeral: purged disappearing messages", "count", n)
				}
			}
		}
	}()

	// ── analytics (metadata-only product analytics; HLD §18.1) ────────────
	// A NATS consumer aggregates privacy-preserving events into daily PG rollups
	// (analytics_daily) and Prometheus gauges; distinct users (DAU/MAU) ride a
	// Valkey HyperLogLog sketch — no per-user rows exist. Emitters are
	// fire-and-forget; the consumer + tickers are here.
	analyticsSvc := analytics.NewService(analyticsadapters.NewRollups(pool), analyticsadapters.NewDistinct(vk), analyticsMetrics)
	analyticsConsumer := analyticsadapters.NewConsumer(nc, analyticsSvc, log)
	if err := analyticsConsumer.Start(); err != nil {
		log.Error("analytics consumer start failed", "err", err)
		os.Exit(1)
	}
	defer analyticsConsumer.Stop()
	// Condense the distinct sketch into DAU/MAU rollups + gauges each minute.
	go func() {
		t := time.NewTicker(time.Minute)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if err := analyticsSvc.RollupDistinct(ctx, time.Now()); err != nil {
					log.Warn("analytics: distinct rollup failed", "err", err)
				}
			}
		}
	}()
	// Trim rollups past the ~13-month retention window daily (idempotent DELETE).
	go func() {
		t := time.NewTicker(24 * time.Hour)
		defer t.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-t.C:
				if n, err := analyticsSvc.Purge(ctx); err != nil {
					log.Warn("analytics: retention purge failed", "err", err)
				} else if n > 0 {
					log.Info("analytics: purged old rollups", "count", n)
				}
			}
		}
	}()

	// ── feature flags + kill-switches (core-api-lld §5) ──────────────────
	// Rules in feature_flags, evaluated through a 30 s Valkey cache. Kill-
	// switches are operational circuit breakers enforced at the routing edge
	// (below), so pausing registrations / group creation / calls during an
	// incident needs no code deploy.
	flagStore := flags.NewPGStore(pool)
	flagCache := flags.NewValkeyCache(vk)
	flagsSvc := flags.NewService(flagStore, flagCache)
	// On-device AI runtime config (T11.01): exposes the AI kill-switch to clients.
	aiSvc := ai.NewService(flagsSvc)

	// ── admin plane (trust & safety + operations) ───────────────────────
	// OIDC SSO gates the admin SPA (HLD §15.6): the external IdP owns admin
	// membership and RBAC roles (viewer → agent → operator → owner). Configured
	// via WA_ADMIN_OIDC_ISSUER / _AUDIENCE / _ROLE_CLAIM plus the provider's
	// JWKS document (WA_ADMIN_OIDC_JWKS). Unset → the admin surface is not
	// mounted (offline/dev bring-up without an IdP). The HLD's edge controls —
	// separate hostname, IP allowlist, hardware-key 2FA — are enforced at Envoy.
	var adminSvc *admin.Service
	var adminFlags *admin.FlagConsole
	if iss, jwksJSON := os.Getenv("WA_ADMIN_OIDC_ISSUER"), os.Getenv("WA_ADMIN_OIDC_JWKS"); iss != "" && jwksJSON != "" {
		keySet, err := admin.NewKeySet([]byte(jwksJSON))
		if err != nil {
			log.Error("admin: invalid WA_ADMIN_OIDC_JWKS", "err", err)
			os.Exit(1)
		}
		verifier := admin.NewOIDCVerifier(iss, os.Getenv("WA_ADMIN_OIDC_AUDIENCE"), os.Getenv("WA_ADMIN_OIDC_ROLE_CLAIM"), keySet)
		adminStore := adminadapters.NewStore(pool)
		adminSvc = admin.NewService(verifier, adminStore, adminStore, adminStore)
		// Feature-flag management rides the admin plane (RBAC + audit + OIDC).
		adminFlags = admin.NewFlagConsole(flagStore, adminStore, flagCache)
		log.Info("admin plane enabled", "issuer", iss)
	} else {
		log.Warn("admin plane disabled — WA_ADMIN_OIDC_ISSUER/JWKS unset (no IdP configured)")
	}

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
	auth.Routes(mux, authSvc, deviceAuthSvc) // deviceAuthSvc audits each sign-in (T10.02)
	keys.Routes(mux, keysSvc, issuer)
	devices.Routes(mux, devSvc, issuer)
	groups.Routes(mux, groupsSvc, issuer)
	contacts.Routes(mux, contactsSvc, issuer)
	calls.Routes(mux, callsSvc, issuer, callsWebhook)
	ptt.Routes(mux, pttSvc, pttMinter, issuer)
	stories.Routes(mux, storiesSvc, issuer)
	polls.Routes(mux, pollsSvc, issuer)             // /v1/polls create/vote/close/results (T6.02)
	channels.Routes(mux, channelsSvc, issuer)       // /v1/channels broadcast: CRUD/follow/posts/react/comment (T7.01)
	communities.Routes(mux, communitiesSvc, issuer) // /v1/communities: groups-roof + roles/events/discovery (T8.01)
	webinar.Routes(mux, webinarSvc, issuer)         // /v1/webinars: live 1-to-many, waiting room, raise-hand, Q&A (T9.02)
	breakout.Routes(mux, breakoutSvc, issuer)       // /v1/live: breakout rooms + streaming egress + recording consent (T9.03)
	ephemeral.Routes(mux, ephemeralSvc, issuer)     // /v1/conversations/{id}/disappearing: per-chat timer (T10.01)
	deviceauth.Routes(mux, deviceAuthSvc, issuer)   // /v1/auth/passkeys + /v1/auth/logins: WebAuthn 2FA + login audit (T10.02)
	abuse.Routes(mux, abuseSvc, issuer)             // /v1/reports: file trust-and-safety reports into the admin queue (T10.03)
	ai.Routes(mux, aiSvc, issuer)                   // /v1/ai/config: on-device AI runtime kill-switch (T11.01)
	chat.Routes(mux, chatStore, issuer)             // POST /v1/conversations/direct (start a 1:1)
	profile.Routes(mux, profileSvc, issuer)         // /v1/me, /v1/users/{id}, /v1/blocks (T5.07)
	if adminSvc != nil {
		admin.Routes(mux, adminSvc)
		admin.FlagRoutes(mux, adminSvc, adminFlags)
	}
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := pool.Ping(r.Context()); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	handler := observability.WrapHTTPHandler(
		httpMetrics.Middleware(flagsSvc.KillSwitchMiddleware(flags.CoreAPIGuards())(mux)), "http.server")
	if cfg.Env != "prod" {
		// The web PWA dev server runs on a different origin (e.g. :5173) than the
		// API (:8080), so a browser needs CORS to call it. In prod the web app is
		// same-origin behind the ingress, so this stays off.
		handler = devCORS(handler)
	}
	httpSrv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler,
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
			Host:     cfg.Auth.SMTPHost,
			Port:     cfg.Auth.SMTPPort,
			From:     cfg.Auth.SMTPFrom,
			Username: cfg.Auth.SMTPUser,
			Password: cfg.Auth.SMTPPassword,
		}, nil
	default:
		return nil, errors.New("unknown OTP channel " + cfg.Auth.OTPChannel)
	}
}

// devCORS reflects the caller's Origin and answers preflight requests so a
// browser on a different origin (the web PWA dev server) can call the API. Only
// mounted outside prod (see wiring above); prod serves the web app same-origin.
func devCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Add("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
			w.Header().Set("Access-Control-Max-Age", "600")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// webinarMinter adapts the LiveKit token minter to webinar.Minter: a 1-hour
// join token whose publish grant is role-scoped (host/speaker publish; attendees
// subscribe-only), which is what enforces the 1-to-many webinar shape (T9.02).
type webinarMinter struct{ m *calls.TokenMinter }

func (p webinarMinter) MintJoin(room, identity string, canPublish bool) (string, error) {
	return p.m.Mint(calls.JoinGrant{Identity: identity, Room: room, CanPublish: canPublish, CanSubscribe: true}, time.Hour, time.Now())
}

// breakoutMinter adapts the LiveKit token minter to breakout.Minter: a 1-hour
// join token for a breakout/main room (participants publish + subscribe) (T9.03).
type breakoutMinter struct{ m *calls.TokenMinter }

func (p breakoutMinter) MintJoin(room, identity string, canPublish bool) (string, error) {
	return p.m.Mint(calls.JoinGrant{Identity: identity, Room: room, CanPublish: canPublish, CanSubscribe: true}, time.Hour, time.Now())
}

// envOr returns the environment variable, or a fallback default when it's unset.
func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// announcementGroupCreator adapts the groups service to communities.GroupCreator
// so a new community mints its announcement group without communities depending
// on the groups package directly (T8.01).
type announcementGroupCreator struct{ groups *groups.Service }

func (a announcementGroupCreator) CreateAnnouncementGroup(ctx context.Context, ident auth.Identity, name string) (string, error) {
	gv, err := a.groups.Create(ctx, ident, name, "Community announcements", nil)
	if err != nil {
		return "", err
	}
	return gv.ID, nil
}

// ensure the domain package is referenced (compile guard for future wiring).
var _ = domain.OTPValidity

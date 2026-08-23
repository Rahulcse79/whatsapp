// Command media-svc orchestrates uploads: presigned multipart URLs, quotas,
// completion verification, download URLs, and garbage collection against MinIO.
// Blobs never transit this service (media-svc-lld.md, HLD §9).
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/whatsapp-v2/server/internal/auth"
	"github.com/whatsapp-v2/server/internal/backups"
	backupsadapters "github.com/whatsapp-v2/server/internal/backups/adapters"
	"github.com/whatsapp-v2/server/internal/media"
	mediaadapters "github.com/whatsapp-v2/server/internal/media/adapters"
	"github.com/whatsapp-v2/server/internal/platform/config"
	"github.com/whatsapp-v2/server/internal/platform/logging"
	"github.com/whatsapp-v2/server/internal/platform/natsx"
	"github.com/whatsapp-v2/server/internal/platform/observability"
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
	cfg, cfgErr := config.Load("media-svc")
	log := logging.New("media-svc", cfg.LogLevel)
	if cfgErr != nil {
		log.Error("configuration invalid", "err", cfgErr)
		os.Exit(1)
	}
	log.Info("starting", "version", version, "commit", commit, "env", cfg.Env)

	ctx := context.Background()

	tel, err := observability.Init(ctx, observability.Config{
		ServiceName: "media-svc", ServiceVersion: version, Env: cfg.Env, OTLPEndpoint: cfg.OTelEndpoint,
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

	nc, _, err := natsx.Connect(natsx.Config{URL: cfg.NATS.URL, Name: "media-svc"})
	if err != nil {
		log.Error("nats connect failed", "err", err)
		os.Exit(1)
	}
	defer nc.Close()

	objects, err := mediaadapters.NewMinIO(
		os.Getenv("WA_MINIO_ENDPOINT"),
		os.Getenv("WA_MINIO_ACCESS_KEY"),
		os.Getenv("WA_MINIO_SECRET_KEY"),
		getenv("WA_MINIO_BUCKET", "media"),
		os.Getenv("WA_MINIO_SECURE") == "true",
	)
	if err != nil {
		log.Error("minio client failed", "err", err)
		os.Exit(1)
	}

	// QuotaService lives in core-api (the single-writer storage counter).
	coreConn, err := grpc.NewClient(cfg.CoreAPIGRPCAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Error("core-api grpc client failed", "addr", cfg.CoreAPIGRPCAddr, "err", err)
		os.Exit(1)
	}
	defer func() { _ = coreConn.Close() }()

	verifier, err := buildVerifier(cfg, log)
	if err != nil {
		log.Error("building token verifier", "err", err)
		os.Exit(1)
	}

	store := mediaadapters.NewStore(pool)
	sessions := mediaadapters.NewValkeySessions(vk)
	events := mediaadapters.NewNATSEvents(nc, log)

	// core-api's QuotaService (the single-writer storage counter) isn't wired in
	// dev/offline, so fall back to a no-op quota there; prod uses the real gRPC.
	var quota media.Quota = mediaadapters.NewQuotaClient(coreConn)
	if cfg.Env == "dev" || cfg.Env == "offline" {
		quota = mediaadapters.NewNoopQuota()
		log.Warn("using no-op media quota (dev/offline) — no storage ceiling")
	}

	svc := media.NewService(
		store,
		objects,
		sessions,
		quota,
		mediaadapters.NewRate(ratelimit.NewValkeyLimiter(vk)),
		events,
	)

	// CDN delivery (T15.04): when an edge is configured, download URLs point at
	// it with a signed token instead of at MinIO with a presigned GET. Media is
	// E2EE ciphertext, so the cache never sees plaintext. Unset = direct
	// presign, unchanged. Misconfiguring only one half is fatal rather than
	// silently minting URLs the edge would reject.
	if base, key := os.Getenv("WA_CDN_BASE_URL"), os.Getenv("WA_CDN_SIGNING_KEY"); base != "" || key != "" {
		cdn, err := mediaadapters.NewCDNDelivery(base, key)
		if err != nil {
			log.Error("CDN delivery misconfigured", "err", err)
			os.Exit(1)
		}
		svc = svc.WithDelivery(cdn)
		log.Info("media download URLs served via CDN", "base_url", base)
	} else {
		log.Info("no CDN configured — media download URLs are direct MinIO presigns")
	}

	// media.lifecycle consumer: apply inbound dereference commands (delete-for-
	// everyone with media, account purge) to the refcount (media-svc-lld §4).
	// Queue-grouped so exactly one pod handles each event; idempotent via a
	// Valkey seen-set against duplicate publishes.
	lifecycle := mediaadapters.NewLifecycleSubscriber(
		nc, media.NewLifecycleConsumer(svc, mediaadapters.NewLifecycleDeduper(vk), log), log,
	)
	unsubscribe, err := lifecycle.Start()
	if err != nil {
		log.Error("media.lifecycle subscribe failed", "err", err)
		os.Exit(1)
	}
	defer unsubscribe()

	// GC is a leader-elected singleton (K8s Lease). Until the lease is wired,
	// only the pod flagged leader sweeps, so ×2 pods don't double-run it.
	if os.Getenv("WA_MEDIA_GC_LEADER") == "true" {
		go media.NewGC(store, objects, sessions, events, log).Run(ctx, time.Hour, 500)
		log.Info("gc sweeper running (leader)")
	}

	// GIF search proxy (FR-MED-05): server-side, so the client's IP never reaches
	// the provider. Disabled in the air-gap (offline) profile or when no provider
	// key is configured — GifService then answers FEATURE_DISABLED.
	var gifProvider media.GifProvider
	if cfg.Env != "offline" {
		if key := os.Getenv("WA_GIF_TENOR_KEY"); key != "" {
			gifProvider = mediaadapters.NewTenorProvider(key)
		}
	}
	gifSvc := media.NewGifService(gifProvider, mediaadapters.NewSearchRate(ratelimit.NewValkeyLimiter(vk)))
	stickerSvc := media.NewStickerService(mediaadapters.NewStickerStore(pool))
	log.Info("gif proxy", "enabled", gifSvc.Enabled(), "profile", cfg.Env)

	// Encrypted backups (FR-SYNC-04): presigned multipart for the client-
	// encrypted archive + 1-active-backup registry. Reuses the MinIO adapter
	// (blobs go client↔MinIO directly). Size cap from env (per-profile).
	backupsSvc := backups.NewService(objects, backupsadapters.NewStore(pool), parseBytes(os.Getenv("WA_MAX_BACKUP_BYTES")))

	mux := http.NewServeMux()
	mux.Handle("GET /metrics", tel.MetricsHandler())
	media.Routes(mux, svc, verifier)
	media.GifRoutes(mux, gifSvc, verifier)
	media.StickerRoutes(mux, stickerSvc, verifier)
	backups.Routes(mux, backupsSvc, verifier)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := pool.Ping(r.Context()); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	})

	handler := observability.WrapHTTPHandler(httpMetrics.Middleware(mux), "http.server")
	if cfg.Env != "prod" {
		// The web PWA dev server + browser uploads hit media-svc cross-origin
		// (it's a separate deployable from core-api), so a browser needs CORS.
		// In prod the web app is same-origin behind the ingress, so this is off.
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
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		log.Error("shutdown", "err", err)
	}
	log.Info("stopped")
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// parseBytes reads a byte count from an env value; 0 (empty/invalid) leaves the
// backup size cap at its per-profile default.
func parseBytes(s string) int64 {
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

// buildVerifier returns the JWT verifier from the shared signing seed (media-svc
// only verifies tokens core-api minted). Dev falls back to an ephemeral key.
func buildVerifier(cfg *config.Config, log *slog.Logger) (auth.TokenVerifier, error) {
	if cfg.Auth.JWTEd25519Seed != "" {
		return auth.NewIssuerFromSeed(cfg.Auth.JWTEd25519Seed, "v1", cfg.Auth.AccessTTL)
	}
	log.Warn("no WA_JWT_ED25519_SEED — using an ephemeral key (dev only)")
	return auth.NewEphemeralIssuer(cfg.Auth.AccessTTL)
}

// devCORS reflects the caller's Origin and answers preflight so a browser on a
// different origin (the web PWA dev server + direct browser uploads) can call
// media-svc. Only mounted outside prod; prod serves the web app same-origin.
func devCORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if origin := r.Header.Get("Origin"); origin != "" {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Add("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
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

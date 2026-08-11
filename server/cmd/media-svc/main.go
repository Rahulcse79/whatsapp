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
	"syscall"
	"time"

	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/whatsapp-v2/server/internal/auth"
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
	events := mediaadapters.NewNATSEvents(nc, log)
	svc := media.NewService(
		store,
		objects,
		mediaadapters.NewValkeySessions(vk),
		mediaadapters.NewQuotaClient(coreConn),
		mediaadapters.NewRate(ratelimit.NewValkeyLimiter(vk)),
		events,
	)

	// GC is a leader-elected singleton (K8s Lease). Until the lease is wired,
	// only the pod flagged leader sweeps, so ×2 pods don't double-run it.
	if os.Getenv("WA_MEDIA_GC_LEADER") == "true" {
		go media.NewGC(store, objects, events, log).Run(ctx, time.Hour, 500)
		log.Info("gc sweeper running (leader)")
	}

	mux := http.NewServeMux()
	mux.Handle("GET /metrics", tel.MetricsHandler())
	media.Routes(mux, svc, verifier)
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

// buildVerifier returns the JWT verifier from the shared signing seed (media-svc
// only verifies tokens core-api minted). Dev falls back to an ephemeral key.
func buildVerifier(cfg *config.Config, log *slog.Logger) (auth.TokenVerifier, error) {
	if cfg.Auth.JWTEd25519Seed != "" {
		return auth.NewIssuerFromSeed(cfg.Auth.JWTEd25519Seed, "v1", cfg.Auth.AccessTTL)
	}
	log.Warn("no WA_JWT_ED25519_SEED — using an ephemeral key (dev only)")
	return auth.NewEphemeralIssuer(cfg.Auth.AccessTTL)
}

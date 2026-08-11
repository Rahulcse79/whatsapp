// Command notification-svc consumes the push.dispatch stream and delivers
// wake signals via FCM / APNs / ntfy / WebPush. Payloads never contain
// plaintext content (FR-NOTIF-01).
//
// LLD: Docs/05-services/notification-svc-lld.md
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

	"github.com/whatsapp-v2/server/internal/notify"
	notifyadapters "github.com/whatsapp-v2/server/internal/notify/adapters"
	"github.com/whatsapp-v2/server/internal/platform/config"
	"github.com/whatsapp-v2/server/internal/platform/logging"
	"github.com/whatsapp-v2/server/internal/platform/natsx"
	"github.com/whatsapp-v2/server/internal/platform/observability"
	"github.com/whatsapp-v2/server/internal/platform/pg"
)

// Stamped by CI at release: -ldflags "-X main.version=… -X main.commit=…".
var (
	version = "dev"
	commit  = "none"
)

// maxDeliveries is the redelivery ceiling before a dispatch is dead-lettered.
const maxDeliveries = 5

func main() {
	cfg, cfgErr := config.Load("notification-svc")
	log := logging.New("notification-svc", cfg.LogLevel)
	if cfgErr != nil {
		log.Error("configuration invalid", "err", cfgErr)
		os.Exit(1)
	}
	log.Info("starting", "version", version, "commit", commit, "env", cfg.Env)

	ctx := context.Background()

	tel, err := observability.Init(ctx, observability.Config{
		ServiceName: "notification-svc", ServiceVersion: version, Env: cfg.Env, OTLPEndpoint: cfg.OTelEndpoint,
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

	nc, js, err := natsx.Connect(natsx.Config{URL: cfg.NATS.URL, Name: "notification-svc"})
	if err != nil {
		log.Error("nats connect failed", "err", err)
		os.Exit(1)
	}
	defer nc.Close()

	if err := notifyadapters.EnsurePushStream(ctx, js); err != nil {
		log.Error("ensuring PUSH stream", "err", err)
		os.Exit(1)
	}

	drivers := buildDrivers(log)
	svc := notify.NewService(notifyadapters.NewTokenStore(pool), drivers, maxDeliveries, log)
	consumer := notifyadapters.NewConsumer(js, svc, notifyadapters.NewDLQPublisher(js), maxDeliveries+1, log)

	stopConsume, err := consumer.Run(ctx)
	if err != nil {
		log.Error("starting consumer", "err", err)
		os.Exit(1)
	}

	// Liveness/readiness + Prometheus /metrics on a small HTTP surface.
	mux := http.NewServeMux()
	mux.Handle("GET /metrics", tel.MetricsHandler())
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
	stopConsume() // stop pulling; in-flight messages settle
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		log.Error("shutdown", "err", err)
	}
	log.Info("stopped")
}

// buildDrivers registers the push drivers this deployment supports. ntfy is
// always on (the self-hosted/offline profile — HLD §17.5); FCM activates when
// a project + access-token source is configured. APNs/WebPush are implemented
// but not registered by default (unregistered providers ack-drop rather than
// DLQ-spam); wire them when their credentials land.
func buildDrivers(log *slog.Logger) []notify.PushDriver {
	drivers := []notify.PushDriver{notifyadapters.NewNtfyDriver()}
	if proj := os.Getenv("WA_FCM_PROJECT"); proj != "" {
		drivers = append(drivers, notifyadapters.NewFCMDriver(proj, staticFCMToken))
		log.Info("fcm driver enabled", "project", proj)
	}
	return drivers
}

// staticFCMToken reads a pre-minted OAuth token from the environment. Real
// deployments inject a refreshing service-account token source here.
func staticFCMToken(context.Context) (string, error) {
	t := os.Getenv("WA_FCM_ACCESS_TOKEN")
	if t == "" {
		return "", errors.New("WA_FCM_ACCESS_TOKEN not set")
	}
	return t, nil
}

// Command ws-gateway is the stateless WebSocket tier: connection registry,
// session resume, frame routing (~20k connections per pod).
//
// LLD: Docs/05-services/ws-gateway-lld.md
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
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
	"github.com/whatsapp-v2/server/internal/gateway"
	gwadapters "github.com/whatsapp-v2/server/internal/gateway/adapters"
	"github.com/whatsapp-v2/server/internal/platform/config"
	"github.com/whatsapp-v2/server/internal/platform/logging"
	"github.com/whatsapp-v2/server/internal/platform/natsx"
	"github.com/whatsapp-v2/server/internal/platform/valkey"
)

// Stamped by CI at release: -ldflags "-X main.version=… -X main.commit=…".
var (
	version = "dev"
	commit  = "none"
)

func main() {
	cfg, err := config.Load("ws-gateway")
	log := logging.New("ws-gateway", cfg.LogLevel)
	if err != nil {
		log.Error("configuration invalid", "err", err)
		os.Exit(1)
	}
	log.Info("starting", "version", version, "commit", commit, "env", cfg.Env)

	ctx := context.Background()
	vk, err := valkey.New(ctx, valkey.Config{Addr: cfg.Valkey.Addr, Password: cfg.Valkey.Password})
	if err != nil {
		log.Error("valkey connect failed", "err", err)
		os.Exit(1)
	}
	defer func() { _ = vk.Close() }()

	nc, _, err := natsx.Connect(natsx.Config{URL: cfg.NATS.URL, Name: "ws-gateway"})
	if err != nil {
		log.Error("nats connect failed", "err", err)
		os.Exit(1)
	}
	defer nc.Close()

	verifier, err := buildVerifier(cfg, log)
	if err != nil {
		log.Error("building token verifier", "err", err)
		os.Exit(1)
	}

	// gRPC to core-api: in-cluster plaintext for now; mTLS arrives with the
	// K8s deploy (T0.22). NewClient dials lazily — core-api being down shows
	// up as per-RPC Unavailable (retried once), never as a boot failure.
	coreConn, err := grpc.NewClient(cfg.CoreAPIGRPCAddr,
		grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		log.Error("core-api grpc client failed", "addr", cfg.CoreAPIGRPCAddr, "err", err)
		os.Exit(1)
	}
	defer func() { _ = coreConn.Close() }()

	srv := gateway.NewServer(gateway.Config{
		Verifier: verifier,
		Routes:   gwadapters.NewValkeyRouteStore(vk),
		Delivery: gwadapters.NewNATSDeliverySource(nc),
		Chat:     gwadapters.NewGRPCChatClient(coreConn),
		Resume:   gwadapters.NewValkeyResumeStore(vk),
		PodID:    podID(),
		RouteTTL: 90 * time.Second,
		Log:      log,
		// TODO(T0.11): Authorizer via core-api gRPC session check (revocation).
	})

	mux := http.NewServeMux()
	mux.HandleFunc("/v1/ws", srv.Handle)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	mux.HandleFunc("GET /readyz", func(w http.ResponseWriter, r *http.Request) {
		if err := vk.Ping(r.Context()).Err(); err != nil {
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

	// Graceful shutdown: stop accepting, drain connections (clients reconnect
	// with jittered backoff), then exit (ws-gateway-lld §5).
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	log.Info("draining", "connections", srv.Registry().Count())

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	srv.Registry().CloseAll(gateway.CloseDrain, "server draining")
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		log.Error("shutdown", "err", err)
	}
	log.Info("stopped")
}

// buildVerifier returns the JWT verifier. In production the shared signing
// seed is provided; in dev an ephemeral key is generated (tokens then only
// validate within this process — fine until core-api is wired end to end).
//
// TODO: give the gateway a public-key-only verifier so it never holds signing
// material (it only needs to verify).
func buildVerifier(cfg *config.Config, log *slog.Logger) (auth.TokenVerifier, error) {
	if cfg.Auth.JWTEd25519Seed != "" {
		return auth.NewIssuerFromSeed(cfg.Auth.JWTEd25519Seed, "v1", cfg.Auth.AccessTTL)
	}
	log.Warn("no WA_JWT_ED25519_SEED — using an ephemeral key (dev only)")
	return auth.NewEphemeralIssuer(cfg.Auth.AccessTTL)
}

// podID identifies this gateway instance in the routing table.
func podID() string {
	host, _ := os.Hostname()
	var b [4]byte
	_, _ = rand.Read(b[:])
	return host + "-" + hex.EncodeToString(b[:])
}

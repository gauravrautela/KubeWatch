// Command dashboard serves the read-only KubeWatch dashboard API (and SPA assets).
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/gauravrautela/kubewatch/internal/dashboardapi"
	"github.com/gauravrautela/kubewatch/internal/storage"
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	dsn := os.Getenv("CLICKHOUSE_DSN")
	addr := envOr("LISTEN_ADDR", ":8081")
	spaDir := os.Getenv("SPA_DIR")
	certFile := os.Getenv("TLS_CERT_FILE")
	keyFile := os.Getenv("TLS_KEY_FILE")

	store, err := storage.New(dsn)
	if err != nil {
		log.Fatalf("storage: %v", err)
	}
	defer store.Close()

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	if err := store.Ping(ctx); err != nil {
		log.Fatalf("clickhouse ping: %v", err)
	}

	srv := &http.Server{Addr: addr, Handler: dashboardapi.NewRouter(store, spaDir)}
	srvErr := make(chan error, 1)
	go func() {
		if certFile != "" && keyFile != "" {
			srvErr <- srv.ListenAndServeTLS(certFile, keyFile)
		} else {
			log.Printf("WARNING: dashboard serving cleartext HTTP on %s — front it with an authenticating TLS ingress/proxy in production (audit data is sensitive)", addr)
			srvErr <- srv.ListenAndServe()
		}
	}()
	log.Printf("dashboard listening on %s", addr)

	select {
	case err := <-srvErr:
		if err != nil && err != http.ErrServerClosed {
			log.Fatalf("server: %v", err)
		}
	case <-ctx.Done():
	}

	shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
	_ = srv.Shutdown(shutCtx)
	shutCancel()
}

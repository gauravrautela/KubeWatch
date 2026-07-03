// Command hub receives change events from agents and stores them in ClickHouse.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gauravrautela/kubewatch/internal/ingest"
	"github.com/gauravrautela/kubewatch/internal/storage"
)

func parseTokens(s string) ingest.StaticAuth {
	m := ingest.StaticAuth{}
	for _, pair := range strings.Split(s, ",") {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		if k, v, ok := strings.Cut(pair, "="); ok {
			m[strings.TrimSpace(k)] = strings.TrimSpace(v)
		}
	}
	return m
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	dsn := os.Getenv("CLICKHOUSE_DSN")
	addr := envOr("LISTEN_ADDR", ":8080")
	auth := parseTokens(os.Getenv("AGENT_TOKENS"))
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
	if err := store.Migrate(ctx); err != nil {
		log.Fatalf("migrate: %v", err)
	}

	batcher := ingest.NewBatcher(store, 500, 2*time.Second)

	batcherCtx, batcherCancel := context.WithCancel(context.Background())
	batcherDone := make(chan struct{})
	go func() {
		batcher.Run(batcherCtx)
		close(batcherDone)
	}()

	// Periodically surface the drop counter so silent data loss is visible
	// in logs/monitoring rather than only queryable via code.
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if n := batcher.Dropped(); n > 0 {
					log.Printf("hub: %d events dropped so far", n)
				}
			}
		}
	}()

	mux := http.NewServeMux()
	mux.Handle("/v1/events", ingest.NewHandler(auth, batcher))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	srv := &http.Server{Addr: addr, Handler: mux}
	srvErr := make(chan error, 1)
	go func() {
		if certFile != "" && keyFile != "" {
			srvErr <- srv.ListenAndServeTLS(certFile, keyFile)
		} else {
			log.Printf("WARNING: hub serving cleartext HTTP on %s — terminate TLS at an ingress/proxy in production (bearer tokens must not travel unencrypted)", addr)
			srvErr <- srv.ListenAndServe()
		}
	}()
	log.Printf("hub listening on %s", addr)

	select {
	case err := <-srvErr:
		if err != nil && err != http.ErrServerClosed {
			batcherCancel()
			log.Fatalf("server: %v", err)
		}
	case <-ctx.Done():
	}

	// 1. Stop accepting and let in-flight handlers finish (all batcher.Add calls land).
	shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
	_ = srv.Shutdown(shutCtx)
	shutCancel()
	// 2. Now stop the batcher so its final drain captures everything enqueued above.
	batcherCancel()
	// 3. Wait for the final flush before the deferred store.Close() runs.
	<-batcherDone
}

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
	go batcher.Run(ctx)

	mux := http.NewServeMux()
	mux.Handle("/v1/events", ingest.NewHandler(auth, batcher))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	srv := &http.Server{Addr: addr, Handler: mux}
	go func() {
		<-ctx.Done()
		sh, c := context.WithTimeout(context.Background(), 5*time.Second)
		defer c()
		_ = srv.Shutdown(sh)
	}()

	log.Printf("hub listening on %s", addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("server: %v", err)
	}
}

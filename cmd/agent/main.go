// Command agent hosts the validating webhook and forwards changes to the hub.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/gauravrautela/kubewatch/internal/buffer"
	"github.com/gauravrautela/kubewatch/internal/forward"
	"github.com/gauravrautela/kubewatch/internal/webhook"
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// parseLevel maps a LOG_LEVEL env value (case-insensitive) to a slog.Level,
// defaulting to info for empty or unrecognized values.
func parseLevel(s string) slog.Level {
	switch strings.ToLower(s) {
	case "debug":
		return slog.LevelDebug
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}

func main() {
	logLevelStr := envOr("LOG_LEVEL", "info")
	level := parseLevel(logLevelStr)
	handler := slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: level})
	slog.SetDefault(slog.New(handler))

	addr := envOr("WEBHOOK_ADDR", ":8443")
	hubURL := os.Getenv("HUB_URL") // e.g. https://hub.internal/v1/events
	token := os.Getenv("CLUSTER_TOKEN")
	certFile := os.Getenv("TLS_CERT_FILE")
	keyFile := os.Getenv("TLS_KEY_FILE")

	if hubURL == "" || token == "" {
		slog.Error("agent requires HUB_URL and CLUSTER_TOKEN to be set")
		os.Exit(1)
	}

	buf := buffer.New(10000)
	client := forward.New(hubURL, token)

	var forwarded atomic.Int64

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	mux := http.NewServeMux()
	h := webhook.NewHandler(buf.Add)
	mux.Handle("/webhook", h)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	// Heartbeat: always log flow every 60s so operators can confirm the
	// pipeline is alive, not just when something drops.
	go func() {
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				slog.Info("agent heartbeat", "received", h.Received(), "forwarded", forwarded.Load(), "dropped", buf.Dropped())
			}
		}
	}()

	// Periodic flush loop: drain the buffer and forward batches to the hub.
	flushCtx, flushCancel := context.WithCancel(context.Background())
	flushDone := make(chan struct{})
	go func() {
		defer close(flushDone)
		ticker := time.NewTicker(2 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-flushCtx.Done():
				if events := buf.Drain(); len(events) > 0 {
					if err := client.Send(context.Background(), events); err != nil {
						slog.Warn("forward failed, dropping events", "count", len(events), "err", err)
					} else {
						forwarded.Add(int64(len(events)))
					}
				}
				return
			case <-ticker.C:
				events := buf.Drain()
				if len(events) == 0 {
					continue
				}
				if err := client.Send(flushCtx, events); err != nil {
					slog.Warn("forward failed, dropping events", "count", len(events), "err", err)
				} else {
					forwarded.Add(int64(len(events)))
				}
			}
		}
	}()

	srv := &http.Server{Addr: addr, Handler: mux}
	srv.ErrorLog = slog.NewLogLogger(slog.Default().Handler(), slog.LevelError)
	srvErr := make(chan error, 1)
	go func() { srvErr <- srv.ListenAndServeTLS(certFile, keyFile) }()

	slog.Info("agent starting",
		"webhook_addr", addr,
		"hub_url", hubURL,
		"buffer_max", 10000,
		"flush_interval", "2s",
		"tls_cert_set", certFile != "",
		"tls_key_set", keyFile != "",
		"log_level", logLevelStr,
	)
	slog.Info("agent webhook listening", "addr", addr)

	select {
	case err := <-srvErr:
		if err != nil && err != http.ErrServerClosed {
			flushCancel()
			slog.Error("server error", "err", err)
			os.Exit(1)
		}
	case <-ctx.Done():
	}

	shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
	_ = srv.Shutdown(shutCtx)
	shutCancel()
	flushCancel()
	<-flushDone
}

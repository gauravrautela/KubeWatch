// Command agent hosts the validating webhook and forwards changes to the hub.
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
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

func main() {
	addr := envOr("WEBHOOK_ADDR", ":8443")
	hubURL := os.Getenv("HUB_URL") // e.g. https://hub.internal/v1/events
	token := os.Getenv("CLUSTER_TOKEN")
	certFile := os.Getenv("TLS_CERT_FILE")
	keyFile := os.Getenv("TLS_KEY_FILE")

	buf := buffer.New(10000)
	client := forward.New(hubURL, token)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

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
						log.Printf("agent: final forward failed, dropping %d events: %v", len(events), err)
					}
				}
				return
			case <-ticker.C:
				events := buf.Drain()
				if len(events) == 0 {
					continue
				}
				if err := client.Send(flushCtx, events); err != nil {
					log.Printf("agent: forward failed, dropping %d events: %v", len(events), err)
				}
			}
		}
	}()

	mux := http.NewServeMux()
	mux.Handle("/webhook", webhook.NewHandler(buf.Add))
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusOK) })

	srv := &http.Server{Addr: addr, Handler: mux}
	srvErr := make(chan error, 1)
	go func() { srvErr <- srv.ListenAndServeTLS(certFile, keyFile) }()
	log.Printf("agent webhook listening on %s", addr)

	select {
	case err := <-srvErr:
		if err != nil && err != http.ErrServerClosed {
			flushCancel()
			log.Fatalf("server: %v", err)
		}
	case <-ctx.Done():
	}

	shutCtx, shutCancel := context.WithTimeout(context.Background(), 5*time.Second)
	_ = srv.Shutdown(shutCtx)
	shutCancel()
	flushCancel()
	<-flushDone
}

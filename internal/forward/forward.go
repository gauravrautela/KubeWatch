// Package forward posts batches of change events from the agent to the hub.
package forward

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gauravrautela/kubewatch/internal/event"
)

// Client posts batches to the hub ingest API with a per-cluster bearer token.
type Client struct {
	url     string
	token   string
	http    *http.Client
	retries int
	backoff time.Duration
}

// New constructs a forwarding Client targeting the hub ingest URL.
func New(url, token string) *Client {
	return &Client{
		url:     url,
		token:   token,
		http:    &http.Client{Timeout: 10 * time.Second},
		retries: 3,
		backoff: 200 * time.Millisecond,
	}
}

// Send posts the events as one batch, retrying with linear backoff. It returns
// an error only after all retries fail; the caller decides whether to drop
// (metered) or requeue.
func (c *Client) Send(ctx context.Context, events []event.ChangeEvent) error {
	if len(events) == 0 {
		return nil
	}
	body, err := json.Marshal(event.Batch{Events: events})
	if err != nil {
		return err
	}
	var lastErr error
	for attempt := 0; attempt < c.retries; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(time.Duration(attempt) * c.backoff):
			}
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url, bytes.NewReader(body))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Authorization", "Bearer "+c.token)
		resp, err := c.http.Do(req)
		if err != nil {
			lastErr = err
			if attempt != c.retries-1 {
				slog.Debug("forward: attempt failed, will retry", "attempt", attempt+1, "of", c.retries, "err", lastErr)
			}
			continue
		}
		resp.Body.Close()
		if resp.StatusCode >= 200 && resp.StatusCode < 300 {
			return nil
		}
		lastErr = fmt.Errorf("hub returned %d", resp.StatusCode)
		if attempt != c.retries-1 {
			slog.Debug("forward: attempt failed, will retry", "attempt", attempt+1, "of", c.retries, "err", lastErr)
		}
	}
	return lastErr
}

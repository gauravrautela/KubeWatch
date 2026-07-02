package forward

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/gauravrautela/kubewatch/internal/event"
)

func TestSendPostsBatchWithToken(t *testing.T) {
	var gotToken string
	var gotCount int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotToken = r.Header.Get("Authorization")
		var b event.Batch
		json.NewDecoder(r.Body).Decode(&b)
		gotCount = len(b.Events)
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	c := New(srv.URL, "secret")
	err := c.Send(context.Background(), []event.ChangeEvent{{EventID: "1"}, {EventID: "2"}})
	if err != nil {
		t.Fatal(err)
	}
	if gotToken != "Bearer secret" {
		t.Fatalf("want bearer token, got %q", gotToken)
	}
	if gotCount != 2 {
		t.Fatalf("want 2 events, got %d", gotCount)
	}
}

func TestSendRetriesThenSucceeds(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) < 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()

	c := New(srv.URL, "secret")
	if err := c.Send(context.Background(), []event.ChangeEvent{{EventID: "1"}}); err != nil {
		t.Fatalf("expected eventual success, got %v", err)
	}
	if attempts.Load() < 2 {
		t.Fatalf("expected a retry, got %d attempts", attempts.Load())
	}
}

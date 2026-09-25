package main

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestEnvInt(t *testing.T) {
	cases := map[string]int{"": 500, "250": 250, "0": 500, "-3": 500, "abc": 500}
	for in, want := range cases {
		t.Setenv("BATCH_SIZE", in)
		if got := envInt("BATCH_SIZE", 500); got != want {
			t.Errorf("envInt(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestEnvDuration(t *testing.T) {
	cases := map[string]time.Duration{"": 2 * time.Second, "500ms": 500 * time.Millisecond, "0s": 2 * time.Second, "-1s": 2 * time.Second, "soon": 2 * time.Second}
	for in, want := range cases {
		t.Setenv("FLUSH_INTERVAL", in)
		if got := envDuration("FLUSH_INTERVAL", 2*time.Second); got != want {
			t.Errorf("envDuration(%q) = %s, want %s", in, got, want)
		}
	}
}

type fakePinger struct{ err error }

func (f fakePinger) Ping(context.Context) error { return f.err }

func TestReadyz(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want int
	}{
		{"clickhouse up", nil, http.StatusOK},
		{"clickhouse down", errors.New("dial tcp: refused"), http.StatusServiceUnavailable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			readyzHandler(fakePinger{tc.err}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/readyz", nil))
			if rec.Code != tc.want {
				t.Fatalf("status = %d, want %d", rec.Code, tc.want)
			}
		})
	}
}

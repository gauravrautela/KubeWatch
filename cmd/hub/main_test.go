package main

import (
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

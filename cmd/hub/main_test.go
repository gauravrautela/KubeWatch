package main

import (
	"os"
	"testing"
)

func TestListenAddr(t *testing.T) {
	cases := []struct {
		name  string
		set   bool
		value string
		want  string
	}{
		{"unset", false, "", ":8097"},
		{"empty", true, "", ":8097"},
		{"explicit 8080", true, ":8080", ":8080"},
		{"explicit other", true, ":18097", ":18097"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// t.Setenv restores the original value when the subtest ends.
			t.Setenv("LISTEN_ADDR", c.value)
			if !c.set {
				os.Unsetenv("LISTEN_ADDR")
			}
			if got := listenAddr(); got != c.want {
				t.Errorf("listenAddr() with LISTEN_ADDR %q (set=%v) = %q, want %q", c.value, c.set, got, c.want)
			}
		})
	}
}

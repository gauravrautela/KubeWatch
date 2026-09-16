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
		{name: "unset", want: ":8765"},
		{name: "empty", set: true, value: "", want: ":8765"},
		{name: "set", set: true, value: ":8080", want: ":8080"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("LISTEN_ADDR", tc.value) // restores the original value afterwards
			if !tc.set {
				os.Unsetenv("LISTEN_ADDR")
			}
			if got := listenAddr(); got != tc.want {
				t.Errorf("listenAddr() = %q, want %q", got, tc.want)
			}
		})
	}
}

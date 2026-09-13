package main

import (
	"os"
	"testing"
)

func TestListenAddr(t *testing.T) {
	cases := []struct {
		name  string
		unset bool
		value string
		want  string
	}{
		{name: "unset", unset: true, want: ":8976"},
		{name: "empty", value: "", want: ":8976"},
		{name: "set", value: ":9000", want: ":9000"},
		{name: "malformed passes through", value: "not-an-addr", want: "not-an-addr"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// Setenv restores the original value afterwards, the unset case included.
			t.Setenv("LISTEN_ADDR", c.value)
			if c.unset {
				os.Unsetenv("LISTEN_ADDR")
			}
			if got := listenAddr(); got != c.want {
				t.Errorf("listenAddr() = %q, want %q", got, c.want)
			}
		})
	}
}

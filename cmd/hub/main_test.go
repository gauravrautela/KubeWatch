package main

import "testing"

func TestEnvInt(t *testing.T) {
	cases := map[string]int{"": 500, "abc": 500, "0": 500, "-3": 500, "1000": 1000}
	for in, want := range cases {
		t.Setenv("BATCH_SIZE", in)
		if got := envInt("BATCH_SIZE", 500); got != want {
			t.Errorf("envInt(%q) = %d, want %d", in, got, want)
		}
	}
}

package version

import "testing"

func TestString(t *testing.T) {
	if String() == "" {
		t.Fatal("version string must not be empty")
	}
}

func TestUserAgent(t *testing.T) {
	old := Version
	defer func() { Version = old }()
	Version = "v1.2.3"
	if got, want := UserAgent(), "kubewatch/v1.2.3"; got != want {
		t.Fatalf("UserAgent() = %q, want %q", got, want)
	}
}

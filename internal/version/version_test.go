package version

import "testing"

func TestString(t *testing.T) {
	if String() == "" {
		t.Fatal("version string must not be empty")
	}
}

func TestStringWithCommit(t *testing.T) {
	oldV, oldC := Version, Commit
	t.Cleanup(func() { Version, Commit = oldV, oldC })

	Version, Commit = "1.4.0", ""
	if got := String(); got != "1.4.0" {
		t.Fatalf("String() = %q, want %q", got, "1.4.0")
	}
	Version, Commit = "1.4.0", "a1b2c3d"
	if got := String(); got != "1.4.0 (a1b2c3d)" {
		t.Fatalf("String() = %q, want %q", got, "1.4.0 (a1b2c3d)")
	}
}

package version

import "testing"

func TestString(t *testing.T) {
	if String() == "" {
		t.Fatal("version string must not be empty")
	}
}

func TestIsDev(t *testing.T) {
	old := Version
	defer func() { Version = old }()
	for _, c := range []struct {
		v    string
		want bool
	}{{"dev", true}, {"", true}, {"v1.2.3", false}} {
		Version = c.v
		if got := IsDev(); got != c.want {
			t.Errorf("IsDev() with %q = %v, want %v", c.v, got, c.want)
		}
	}
}

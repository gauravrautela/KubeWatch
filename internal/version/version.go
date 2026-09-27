// Package version exposes the build version of KubeWatch binaries.
package version

// Version is overridable at build time with -ldflags "-X ...".
var Version = "dev"

// Commit is the short Git commit the binary was built from, set at build
// time with -ldflags "-X github.com/gauravrautela/kubewatch/internal/version.Commit=<sha>".
// Empty when unknown.
var Commit = ""

// String returns the current version string, with the commit in
// parentheses when it is known: "1.4.0 (a1b2c3d)".
func String() string {
	if Commit == "" {
		return Version
	}
	return Version + " (" + Commit + ")"
}

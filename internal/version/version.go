// Package version exposes the build version of KubeWatch binaries.
package version

// Version is overridable at build time with -ldflags "-X ...".
var Version = "dev"

// String returns the current version string.
func String() string { return Version }

// UserAgent is the User-Agent KubeWatch sends on outbound HTTP calls,
// e.g. "kubewatch/v1.2.3".
func UserAgent() string { return "kubewatch/" + String() }

// Package version exposes build metadata injected at link time.
package version

// These are overridden via -ldflags "-X github.com/evilgenius79/fable-tailscale/internal/version.Version=...".
var (
	Version = "dev"
	Commit  = "unknown"
	Date    = "unknown"
)

// String returns a human readable version string.
func String() string {
	return Version + " (" + Commit + ", " + Date + ")"
}

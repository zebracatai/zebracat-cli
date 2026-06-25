// Package version holds the CLI version, injected at build time via -ldflags.
package version

var (
	// Version is the semantic version of the CLI. Overridden by goreleaser.
	Version = "0.1.0"
	// Commit is the git commit the binary was built from.
	Commit = "dev"
	// Date is the build date (RFC3339).
	Date = "unknown"
)

//go:build !testreleases

package update

// releasesHost is where a shipped binary reads releases from: always GitHub.
func releasesHost() string { return "https://github.com" }

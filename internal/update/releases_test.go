//go:build !testreleases

package update

import "testing"

// A shipped binary ignores ZEBRACAT_RELEASES_URL: the environment can't point
// `zebracat update` at another host.
func TestReleaseBuildIgnoresTheReleasesEnv(t *testing.T) {
	t.Setenv("ZEBRACAT_RELEASES_URL", "http://127.0.0.1:1/evil")
	if got := releasesHost(); got != "https://github.com" {
		t.Fatalf("releasesHost() = %q, want https://github.com", got)
	}
}

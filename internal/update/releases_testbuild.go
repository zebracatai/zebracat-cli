//go:build testreleases

package update

import (
	"os"
	"strings"
)

// releasesHost, in a `go build -tags testreleases` binary only, honours
// ZEBRACAT_RELEASES_URL so a manual smoke test can point `zebracat update` at a
// local release server. Release builds never carry this tag.
func releasesHost() string {
	if u := os.Getenv("ZEBRACAT_RELEASES_URL"); u != "" {
		return strings.TrimRight(u, "/")
	}
	return "https://github.com"
}

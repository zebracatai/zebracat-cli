package cmd

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/zebracatai/zebracat-cli/internal/attach"
	"github.com/zebracatai/zebracat-cli/internal/clierr"
)

// launchImages are the --image pictures the conversation starts with.
var launchImages []attach.Image

// openImages checks each --image path[:kind] (PNG, JPEG or WebP, up to 12 MB).
func openImages(specs []string) ([]attach.Image, error) {
	var out []attach.Image
	for _, spec := range specs {
		path, kind := spec, "other"
		if i := strings.LastIndex(spec, ":"); i > 1 { // "photo.png:person" (a Windows "C:\" drive is not a kind)
			k := spec[i+1:]
			for _, known := range attach.Kinds {
				if k == known {
					path, kind = spec[:i], k
				}
			}
		}
		im, err := attach.Open(path)
		if err != nil {
			return nil, clierr.Usage("--image: %v", err)
		}
		im.Kind = kind
		out = append(out, im)
	}
	return out, nil
}

// loginCmd is `zebracat login`: the same as `zebracat auth login`.
var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Log in (same as `zebracat auth login`)",
	RunE:  func(cmd *cobra.Command, args []string) error { return apiKeyLogin() },
}

func init() {
	loginCmd.Hidden = true // the same as `auth login`, kept for old scripts (G8)
	rootCmd.AddCommand(createCmd, loginCmd)
}

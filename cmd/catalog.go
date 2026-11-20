package cmd

import (
	"github.com/spf13/cobra"

	"github.com/zebracatai/zebracat-cli/internal/videos"
)

// ---- catalogues -------------------------------------------------------------

var modelsCmd = &cobra.Command{Use: "models", Short: "Video qualities and the models behind them"}
var modelsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List qualities, their models and credits per 5 s",
	RunE:  func(cmd *cobra.Command, args []string) error { return getAndEmit(videos.Base + "/video/models") },
}

var presetsCmd = &cobra.Command{Use: "presets", Short: "Video presets (street interview, podcast, UGC ad, …)"}
var presetsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List presets for `video create --preset`",
	RunE:  func(cmd *cobra.Command, args []string) error { return getAndEmit(videos.Base + "/video/presets") },
}

func init() {
	modelsCmd.AddCommand(modelsListCmd)
	presetsCmd.AddCommand(presetsListCmd)
	rootCmd.AddCommand(modelsCmd, presetsCmd)
}

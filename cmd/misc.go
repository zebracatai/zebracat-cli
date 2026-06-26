package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/zebracatai/zebracat-cli/internal/clierr"
	"github.com/zebracatai/zebracat-cli/internal/config"
	"github.com/zebracatai/zebracat-cli/internal/ui"
	"github.com/zebracatai/zebracat-cli/internal/update"
	"github.com/zebracatai/zebracat-cli/internal/version"
)

// ---- config ----
var configCmd = &cobra.Command{Use: "config", Short: "Read and write CLI settings"}

var configListCmd = &cobra.Command{
	Use:   "list",
	Short: "Show current settings",
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := config.LoadSettings()
		if err != nil {
			return clierr.API("%v", err)
		}
		out := map[string]any{
			"base_url": config.ResolveBaseURL(s),
			"mcp_url":  config.ResolveMCPURL(s),
			"output":   config.ResolveOutput(s),
		}
		return emit(out, func() {
			ui.KV([][2]string{{"base_url", out["base_url"].(string)}, {"mcp_url", out["mcp_url"].(string)}, {"output", out["output"].(string)}})
		})
	},
}

var configSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "Set a setting (base_url | mcp_url | output)",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := config.LoadSettings()
		if err != nil {
			return clierr.API("%v", err)
		}
		switch args[0] {
		case "base_url":
			s.BaseURL = args[1]
		case "mcp_url":
			s.MCPURL = args[1]
		case "output":
			if args[1] != "json" && args[1] != "human" {
				return clierr.Usage("output must be json or human")
			}
			s.Output = args[1]
		default:
			return clierr.Usage("unknown key %q (base_url|mcp_url|output)", args[0])
		}
		if err := config.SaveSettings(s); err != nil {
			return clierr.API("%v", err)
		}
		ui.Success("Set %s = %s", args[0], args[1])
		return emit(map[string]any{args[0]: args[1]}, func() {})
	},
}

// ---- version ----
var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print the CLI version",
	RunE: func(cmd *cobra.Command, args []string) error {
		out := map[string]any{"version": version.Version, "commit": version.Commit, "date": version.Date}
		return emit(out, func() {
			fmt.Print(ui.Banner())
			fmt.Printf("zebracat %s (%s, %s)\n", version.Version, version.Commit, version.Date)
		})
	},
}

// ---- update ----
var flagUpdateCheck bool

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update the CLI to the latest release",
	Long: `Download the latest release and replace this binary in place.

Use --check to only report whether an update is available, without installing.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := ctxTimeout(60 * time.Second)
		defer cancel()

		latest, err := update.Latest(ctx)
		if err != nil {
			return clierr.API("could not check for updates: %v", err)
		}
		current := "v" + version.Version

		if !update.Newer(current, latest) {
			out := map[string]any{"current": current, "latest": latest, "updated": false}
			return emit(out, func() { ui.Success("You're on the latest version (%s).", current) })
		}
		if flagUpdateCheck {
			out := map[string]any{"current": current, "latest": latest, "updated": false, "update_available": true}
			return emit(out, func() {
				ui.Warn("A newer version is available: %s (you have %s).", latest, current)
				ui.Info("Install it with:  zebracat update")
			})
		}

		sp := ui.StartSpinner(fmt.Sprintf("Updating to %s…", latest))
		err = update.Apply(ctx, latest)
		sp.Stop()
		if err != nil {
			return clierr.API("%v", err)
		}
		out := map[string]any{"current": current, "latest": latest, "updated": true}
		return emit(out, func() { ui.Success("Updated %s → %s. Run `zebracat version` to confirm.", current, latest) })
	},
}

func init() {
	configCmd.AddCommand(configListCmd, configSetCmd)
	updateCmd.Flags().BoolVar(&flagUpdateCheck, "check", false, "Only check for an update; don't install it")
	rootCmd.AddCommand(configCmd, versionCmd, updateCmd)
}

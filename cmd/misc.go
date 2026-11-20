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
var configCmd = &cobra.Command{Use: "config", Short: "Read and write CLI settings", Example: "  zebracat config list\n  zebracat config set output human"}

var configListCmd = &cobra.Command{
	Use:     "list",
	Short:   "Show current settings",
	Example: "  zebracat config list --json",
	Args:    cobra.NoArgs,
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
	Example: `  zebracat config set output human
  zebracat config set base_url https://bc-dev.zebracat.ai`,
	Args: cobra.ExactArgs(2),
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
		return emit(map[string]any{args[0]: args[1]}, func() { ui.Success("Set %s = %s", args[0], args[1]) })
	},
}

// ---- version ----
var versionCmd = &cobra.Command{
	Use:     "version",
	Short:   "Print the CLI version, commit and install path",
	Example: "  zebracat version --json\n  zebracat --version     # one line",
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		out := map[string]any{"version": version.Version, "commit": version.Commit, "date": version.Date,
			"path": update.CurrentPath(), "install_method": update.Method()}
		return emit(out, func() {
			fmt.Print(ui.Banner())
			fmt.Printf("zebracat %s (%s, %s)\n%s (%s)\n", version.Version, version.Commit, version.Date, update.CurrentPath(), update.Method())
		})
	},
}

// ---- update ----
var flagUpdateCheck, flagUpdateNoRelaunch bool

var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Update the CLI to the latest release",
	Long: `Download the latest release and replace this binary in place.

Use --check to only report whether an update is available, without installing.
On a terminal the new version starts right away (the conversation, saying
"Updated from …"); --no-relaunch, --json and scripts just update. An npm or
"go install" copy is left to its package manager: the command prints the line
to run instead.

The launch check (once a day, 1.5 s at most, never blocking) is off with
ZEBRACAT_NO_UPDATE_CHECK=1 and in CI.`,
	Example: `  zebracat update --check --json
  zebracat update`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		ctx, cancel := ctxTimeout(60 * time.Second)
		defer cancel()

		latest, err := update.Latest(ctx)
		if err != nil {
			return clierr.API("could not check for updates: %v", err)
		}
		current := "v" + version.Version

		if !update.Newer(current, latest) {
			out := map[string]any{"current": current, "latest": latest, "updated": false, "update_available": false}
			return emit(out, func() { ui.Success("You're on the latest version (%s).", current) })
		}
		if flagUpdateCheck {
			out := map[string]any{"current": current, "latest": latest, "updated": false, "update_available": true}
			return emit(out, func() {
				ui.Warn("A newer version is available: %s (you have %s).", latest, current)
				ui.Info("Install it with:  zebracat update")
			})
		}

		if m := update.Method(); m != "binary" {
			cmdline := update.UpgradeCommand()
			out := map[string]any{"current": current, "latest": latest, "updated": false, "update_available": true,
				"method": m, "command": cmdline}
			return emit(out, func() {
				ui.Warn("%s is available (you have %s). This copy was installed with %s.", latest, current, m)
				ui.Info("Update it with:  %s", cmdline)
			})
		}

		sp := ui.StartSpinner(fmt.Sprintf("Updating to %s…", latest))
		path, err := applyUpdate(ctx, latest)
		sp.Stop()
		if err != nil {
			return clierr.API("%v", err)
		}
		relaunch := !flagUpdateNoRelaunch && !flagJSON && interactive()
		out := map[string]any{"current": current, "latest": latest, "updated": true, "update_available": false, "path": path, "relaunched": relaunch}
		if !relaunch {
			return emit(out, func() {
				ui.Success("Updated %s → %s", current, latest)
				ui.Info("Binary: %s", path)
			})
		}
		// On a terminal the new version opens straight away: the conversation,
		// with "Updated from …" in its welcome box.
		ui.Success("Updated %s → %s — starting it…", current, latest)
		if err := relaunchBinary(path, current, nil); err != nil {
			ui.Info("Start it with: zebracat (%v)", err)
		}
		return nil
	},
}

// applyUpdate and relaunchBinary are swapped in tests.
var (
	applyUpdate    = update.Apply
	relaunchBinary = update.Relaunch
)

func init() {
	configCmd.AddCommand(configListCmd, configSetCmd)
	updateCmd.Flags().BoolVar(&flagUpdateCheck, "check", false, "Only check for an update; don't install it")
	updateCmd.Flags().BoolVar(&flagUpdateNoRelaunch, "no-relaunch", false, "Don't start the new version after updating")
	rootCmd.AddCommand(configCmd, versionCmd, updateCmd)
}

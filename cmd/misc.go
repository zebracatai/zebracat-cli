package cmd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/spf13/cobra"

	"github.com/zebracatai/zebracat-cli/internal/clierr"
	"github.com/zebracatai/zebracat-cli/internal/config"
	"github.com/zebracatai/zebracat-cli/internal/ui"
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
var updateCmd = &cobra.Command{
	Use:   "update",
	Short: "Check for a newer release",
	RunE: func(cmd *cobra.Command, args []string) error {
		c := &http.Client{Timeout: 15 * time.Second}
		resp, err := c.Get("https://api.github.com/repos/zebracatai/zebracat-cli/releases/latest")
		if err != nil {
			return clierr.API("could not check for updates: %v", err)
		}
		defer resp.Body.Close()
		var rel struct {
			TagName string `json:"tag_name"`
		}
		_ = json.NewDecoder(resp.Body).Decode(&rel)
		latest := rel.TagName
		current := "v" + version.Version
		out := map[string]any{"current": current, "latest": latest}
		return emit(out, func() {
			if latest == "" {
				ui.Warn("No published releases found yet.")
				return
			}
			if latest == current {
				ui.Success("You're on the latest version (%s).", current)
				return
			}
			ui.Warn("A newer version is available: %s (you have %s)", latest, current)
			ui.Info("Update with:  curl -fsSL https://static.zebracat.ai/cli/install.sh | bash")
		})
	},
}

func init() {
	configCmd.AddCommand(configListCmd, configSetCmd)
	rootCmd.AddCommand(configCmd, versionCmd, updateCmd)
}

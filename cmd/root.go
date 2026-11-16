// Package cmd wires up the Zebracat CLI commands (cobra).
package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/zebracatai/zebracat-cli/internal/client"
	"github.com/zebracatai/zebracat-cli/internal/clierr"
	"github.com/zebracatai/zebracat-cli/internal/config"
	"github.com/zebracatai/zebracat-cli/internal/ui"
	"github.com/zebracatai/zebracat-cli/internal/update"
	"github.com/zebracatai/zebracat-cli/internal/version"
)

var (
	flagHuman   bool
	flagJSON    bool
	flagAPIKey  string
	flagBaseURL string
	flagQuiet   bool
)

var rootCmd = &cobra.Command{
	Use:   "zebracat",
	Short: "Zebracat — AI video generation from your terminal",
	Long: ui.Banner() + `
Create AI videos, manage projects, and drive Zebracat from scripts and agents.

Auth: run "zebracat auth login" (uses your plan credits) or set ZEBRACAT_API_KEY
(pay-as-you-go). Output is JSON by default; add --human for readable tables.`,
	SilenceUsage:  true,
	SilenceErrors: true,
	Version:       version.Version,
}

// Execute runs the root command and maps errors to stable exit codes.
func Execute() {
	if err := rootCmd.Execute(); err != nil {
		var ce *clierr.Error
		if errors.As(err, &ce) {
			ui.PrintErrorEnvelope(ce.Envelope())
			os.Exit(ce.Exit)
		}
		ui.PrintError("error", err.Error(), "")
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&flagHuman, "human", false, "Force human-readable output (default when run in a terminal)")
	rootCmd.PersistentFlags().BoolVar(&flagJSON, "json", false, "Force JSON output (default when piped or scripted)")
	rootCmd.PersistentFlags().StringVar(&flagAPIKey, "api-key", "", "API key (overrides ZEBRACAT_API_KEY / stored credentials)")
	rootCmd.PersistentFlags().StringVar(&flagBaseURL, "base-url", "", "Override the API base URL")
	rootCmd.PersistentFlags().BoolVarP(&flagQuiet, "quiet", "q", false, "Suppress non-essential stderr output")
	rootCmd.SetVersionTemplate(ui.Banner() + "zebracat {{.Version}}\n")
	rootCmd.PersistentPreRun = func(cmd *cobra.Command, args []string) { startUpdateCheck(cmd) }
	rootCmd.PersistentPostRun = func(cmd *cobra.Command, args []string) { finishUpdateCheck() }
}

// updateNotice waits for the launch update check (see startUpdateCheck).
var updateNotice func() string

// startUpdateCheck starts the launch update check in the background: GitHub's
// /releases/latest redirect, cached a day under ~/.zebracat/update.json, with a
// 1.5 s cap. It is off with ZEBRACAT_NO_UPDATE_CHECK=1, in CI, for --quiet,
// when stderr is not a terminal, and for commands that shouldn't nag.
func startUpdateCheck(cmd *cobra.Command) {
	updateNotice = nil
	switch cmd.Name() {
	case "zebracat", "chat", "create", "update", "version", "completion", "help", "__complete", "__completeNoDesc":
		return // the TUI shows its own notice; these commands shouldn't nag
	}
	if flagQuiet || !ui.IsTTY(os.Stderr) {
		return
	}
	updateNotice = update.Check("v" + version.Version)
}

// finishUpdateCheck prints the notice (one line on stderr) once the command is
// done. It never changes stdout or the exit code.
func finishUpdateCheck() {
	if updateNotice == nil {
		return
	}
	if n := updateNotice(); n != "" {
		fmt.Fprintln(os.Stderr, "↑ "+n)
	}
	updateNotice = nil
}

// settings loads on-disk settings honoring the --base-url override.
func settings() (*config.Settings, error) {
	s, err := config.LoadSettings()
	if err != nil {
		return nil, clierr.API("could not read config: %v", err)
	}
	if flagBaseURL != "" {
		s.BaseURL = flagBaseURL
	}
	return s, nil
}

// newClient builds an API client from settings + stored credentials.
func newClient() (*client.Client, error) {
	s, err := settings()
	if err != nil {
		return nil, err
	}
	creds, err := config.LoadCredentials()
	if err != nil {
		return nil, clierr.API("could not read credentials: %v", err)
	}
	return client.New(config.ResolveBaseURL(s), creds, flagAPIKey), nil
}

// humanMode reports whether to render themed human output. Explicit flags/env/
// config win; otherwise we default to human in an interactive terminal and JSON
// when piped or scripted (so agents and pipelines still get clean JSON).
func humanMode() bool {
	switch {
	case flagJSON:
		return false
	case flagHuman:
		return true
	}
	if v := os.Getenv("ZEBRACAT_OUTPUT"); v != "" {
		return v == "human"
	}
	if s, _ := config.LoadSettings(); s != nil && s.Output != "" {
		return s.Output == "human"
	}
	return ui.IsTTY(os.Stdout)
}

// emit prints v as JSON, or themed human output when in human mode. If a bespoke
// human renderer is provided it's used; otherwise a generic themed renderer runs.
func emit(v any, human func()) error {
	if humanMode() {
		if human != nil {
			human()
		} else {
			ui.Auto(v)
		}
		return nil
	}
	return ui.PrintJSON(v)
}

// ctxTimeout returns a context with the given timeout (0 = no timeout).
func ctxTimeout(d time.Duration) (context.Context, context.CancelFunc) {
	if d <= 0 {
		return context.WithCancel(context.Background())
	}
	return context.WithTimeout(context.Background(), d)
}

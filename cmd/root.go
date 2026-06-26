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
			ui.PrintError(ce.Code, ce.Message, ce.Hint)
			os.Exit(ce.Exit)
		}
		ui.PrintError("error", err.Error(), "")
		os.Exit(1)
	}
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&flagHuman, "human", false, "Human-readable output instead of JSON")
	rootCmd.PersistentFlags().StringVar(&flagAPIKey, "api-key", "", "API key (overrides ZEBRACAT_API_KEY / stored credentials)")
	rootCmd.PersistentFlags().StringVar(&flagBaseURL, "base-url", "", "Override the API base URL")
	rootCmd.PersistentFlags().BoolVarP(&flagQuiet, "quiet", "q", false, "Suppress non-essential stderr output")
	rootCmd.SetVersionTemplate(ui.Banner() + "zebracat {{.Version}}\n")
	rootCmd.PersistentPreRun = func(cmd *cobra.Command, args []string) { maybeNotifyUpdate(cmd) }
}

// maybeNotifyUpdate prints a one-line "update available" notice to stderr for
// interactive commands. It's cached (≤1 network check/day) and never touches
// stdout, so JSON output and pipelines stay clean.
func maybeNotifyUpdate(cmd *cobra.Command) {
	switch cmd.Name() {
	case "zebracat", "update", "version", "completion", "help", "__complete", "__completeNoDesc":
		return // the TUI shows its own notice; these commands shouldn't nag
	}
	if flagQuiet || !ui.IsTTY(os.Stderr) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	latest := update.LatestCached(ctx)
	if latest != "" && update.Newer("v"+version.Version, latest) {
		fmt.Fprintf(os.Stderr, "↑ zebracat %s is available (you have v%s) — run `zebracat update`\n", latest, version.Version)
	}
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

// humanMode reports whether to render human output.
func humanMode() bool {
	if flagHuman {
		return true
	}
	s, _ := config.LoadSettings()
	return config.ResolveOutput(s) == "human"
}

// emit prints v as JSON, or calls human() when in human mode (human may be nil).
func emit(v any, human func()) error {
	if humanMode() && human != nil {
		human()
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

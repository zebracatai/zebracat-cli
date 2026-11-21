// Package cmd wires up the Zebracat CLI commands (cobra).
package cmd

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
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
	flagNoColor bool
)

var rootCmd = &cobra.Command{
	Use:   "zebracat",
	Short: "Zebracat — AI video generation from your terminal",
	Long: ui.Banner() + `
Create AI videos, manage projects, and drive Zebracat from scripts and agents.
Run zebracat with no arguments to open the conversation.

Auth: an API key, billed pay-as-you-go from your API balance. Set
ZEBRACAT_API_KEY, or run "zebracat auth login" once to save one.

Output: tables for people in a terminal, JSON when piped or scripted. Force one
with --human or --json. "zebracat commands --json" lists every command for agents.`,
	SilenceUsage:  true,
	SilenceErrors: true,
	Version:       version.Version,
}

// Execute runs the root command and maps errors to stable exit codes.
func Execute() {
	prepareCommands()
	if err := rootCmd.Execute(); err != nil {
		ce := asCLIError(err)
		printError(ce)
		os.Exit(ce.Exit)
	}
}

// asCLIError turns any error into the envelope; one that is not already a
// *clierr.Error is an API/network failure (exit 1).
func asCLIError(err error) *clierr.Error {
	var ce *clierr.Error
	if errors.As(err, &ce) {
		return ce
	}
	return clierr.API("%v", err)
}

// printError writes the error to stderr: "✗ message" + hint for a person at a
// terminal, the JSON envelope for --json, pipes, scripts and agents.
func printError(ce *clierr.Error) {
	if humanMode() && ui.IsTTY(os.Stderr) {
		ui.PrintHumanError(ce)
		return
	}
	ui.PrintErrorEnvelope(ce.Envelope())
}

// prepare runs once before the first Execute: cobra's own errors (unknown
// flag or command, a missing argument, a bad value) become usage errors
// (exit 2, usage_error), and the help groups are assigned (help.go).
var prepare sync.Once

// pureGroups are the commands that only hold subcommands (`zebracat video`).
var pureGroups = map[*cobra.Command]bool{}

func prepareCommands() {
	prepare.Do(func() {
		rootCmd.InitDefaultHelpCmd()
		rootCmd.InitDefaultCompletionCmd()
		rootCmd.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
			e := clierr.Usage("%v", err)
			e.Hint = "Run `" + c.CommandPath() + " --help` for its flags."
			return e
		})
		walkCommands(rootCmd, func(c *cobra.Command) {
			if c.HasSubCommands() && !c.Runnable() {
				pureGroups[c] = true
				// A group (`zebracat video`): no argument shows its help; an
				// unknown subcommand is a usage error, not help with exit 0.
				c.RunE = func(cmd *cobra.Command, args []string) error {
					if len(args) == 0 {
						return cmd.Help()
					}
					return unknownCommand(cmd, args[0])
				}
				return
			}
			if c.Args != nil {
				check := c.Args
				c.Args = func(cmd *cobra.Command, args []string) error {
					if err := check(cmd, args); err != nil {
						if !cmd.HasParent() && len(args) > 0 {
							return unknownCommand(cmd, args[0])
						}
						e := clierr.Usage("%s: %v", cmd.CommandPath(), err)
						e.Hint = "Usage: " + cmd.UseLine()
						return e
					}
					return nil
				}
			}
		})
		assignHelpGroups()
	})
}

// unknownCommand is the usage error for `zebracat <parent> <name>` where
// name is not a subcommand, with cobra's "did you mean" suggestions.
func unknownCommand(parent *cobra.Command, name string) error {
	e := clierr.Usage("unknown command %q for %q", name, parent.CommandPath())
	e.Hint = "Run `" + parent.CommandPath() + " --help` to see its commands."
	if parent.SuggestionsMinimumDistance <= 0 {
		parent.SuggestionsMinimumDistance = 2 // cobra's own default for `did you mean`
	}
	if s := parent.SuggestionsFor(name); len(s) > 0 {
		e.Hint = "Did you mean `" + parent.CommandPath() + " " + s[0] + "`? " + e.Hint
	}
	return e
}

// walkCommands calls fn for c and every command under it.
func walkCommands(c *cobra.Command, fn func(*cobra.Command)) {
	fn(c)
	for _, sub := range c.Commands() {
		walkCommands(sub, fn)
	}
}

func init() {
	rootCmd.PersistentFlags().BoolVar(&flagHuman, "human", false, "Force human-readable output (default when run in a terminal)")
	rootCmd.PersistentFlags().BoolVar(&flagJSON, "json", false, "Force JSON output (default when piped or scripted)")
	rootCmd.PersistentFlags().StringVar(&flagAPIKey, "api-key", "", "API key for this run (prefer ZEBRACAT_API_KEY: a flag stays in shell history and ps)")
	rootCmd.PersistentFlags().StringVar(&flagBaseURL, "base-url", "", "Override the API base URL")
	rootCmd.PersistentFlags().BoolVarP(&flagQuiet, "quiet", "q", false, "Suppress non-essential stderr output")
	rootCmd.PersistentFlags().BoolVar(&flagNoColor, "no-color", false, "No colours (also NO_COLOR=1 or TERM=dumb)")
	rootCmd.SetVersionTemplate("zebracat {{.Version}}\n")
	rootCmd.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		if flagNoColor {
			ui.DisableColor()
		}
		warnAPIKeyFlag()
		startUpdateCheck(cmd)
	}
	rootCmd.PersistentPostRun = func(cmd *cobra.Command, args []string) { finishUpdateCheck() }
}

// warnAPIKeyFlag says once, on a terminal, that a key passed with --api-key
// is visible in shell history and to other users through ps.
func warnAPIKeyFlag() {
	if flagAPIKey == "" || flagQuiet || !ui.IsTTY(os.Stdin) || !ui.IsTTY(os.Stderr) {
		return
	}
	ui.Warn("--api-key stays in your shell history and shows in `ps`. Prefer ZEBRACAT_API_KEY, or `zebracat auth login` (it reads the key hidden).")
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
	case "zebracat", "chat", "create", "update", "version", "doctor", "commands", "completion", "help", "__complete", "__completeNoDesc":
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
		scrubControl(v) // server text never drives the terminal
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

package cmd

import (
	"github.com/spf13/cobra"

	"github.com/zebracatai/zebracat-cli/internal/client"
	"github.com/zebracatai/zebracat-cli/internal/clierr"
	"github.com/zebracatai/zebracat-cli/internal/config"
	"github.com/zebracatai/zebracat-cli/internal/tui"
	"github.com/zebracatai/zebracat-cli/internal/update"
)

// launchShell runs the interactive conversation (a test swaps it to prove the
// non-terminal paths never open it). After an in-app update it starts the new
// binary in its place.
var launchShell = func(c *client.Client, baseURL, first string) error {
	res, err := tui.Run(c, baseURL, tui.Launch{First: first, JustUpdated: update.TakeUpdatedFrom(), Images: launchImages})
	if err != nil {
		return err
	}
	if res.Relaunch != "" {
		return relaunchBinary(res.Relaunch, res.From, nil)
	}
	return nil
}

// shellAllowed: the conversation needs a terminal on stdin and stdout and human
// output; --json, a pipe or CI never get a prompt.
var shellAllowed = func() bool { return interactive() }

// runInteractive launches the conversation, first being the opening message.
func runInteractive(first string) error {
	if !shellAllowed() {
		e := clierr.Usage("the interactive conversation needs a terminal")
		e.Code = "needs_terminal"
		e.Hint = "For scripts and agents: `zebracat create \"<idea>\" --json` (writes a plan, never prompts), or `zebracat --help`."
		return e
	}
	c, err := newClient()
	if err != nil {
		return err
	}
	s, err := settings()
	if err != nil {
		return err
	}
	return launchShell(c, config.ResolveBaseURL(s), first)
}

var chatCmd = &cobra.Command{
	Use:   "chat",
	Short: "Open the interactive Zebracat conversation",
	Long:  "Open the interactive Zebracat conversation — the same thing you get by running `zebracat` with no arguments.",
	Example: `  zebracat chat
  zebracat          # the same`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error { return runInteractive("") },
}

func init() {
	rootCmd.AddCommand(chatCmd)
	// Bare `zebracat` (no subcommand) opens the conversation.
	rootCmd.Args = cobra.NoArgs
	rootCmd.RunE = func(cmd *cobra.Command, args []string) error { return runInteractive("") }
}

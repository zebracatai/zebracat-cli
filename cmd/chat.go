package cmd

import (
	"github.com/spf13/cobra"

	"github.com/zebracatai/zebracat-cli/internal/config"
	"github.com/zebracatai/zebracat-cli/internal/tui"
)

// runInteractive launches the branded interactive shell (the TUI).
func runInteractive() error {
	c, err := newClient()
	if err != nil {
		return err
	}
	s, err := settings()
	if err != nil {
		return err
	}
	_, err = tui.New(c, config.ResolveBaseURL(s)).Run()
	return err
}

var chatCmd = &cobra.Command{
	Use:   "chat",
	Short: "Open the interactive Zebracat shell",
	Long:  "Open the interactive Zebracat shell — the same thing you get by running `zebracat` with no arguments.",
	Args:  cobra.NoArgs,
	RunE:  func(cmd *cobra.Command, args []string) error { return runInteractive() },
}

func init() {
	rootCmd.AddCommand(chatCmd)
	// Bare `zebracat` (no subcommand) opens the interactive shell.
	rootCmd.Args = cobra.NoArgs
	rootCmd.RunE = func(cmd *cobra.Command, args []string) error { return runInteractive() }
}

package cmd

import (
	"bufio"
	"fmt"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/zebracatai/zebracat-cli/internal/auth"
	"github.com/zebracatai/zebracat-cli/internal/clierr"
	"github.com/zebracatai/zebracat-cli/internal/config"
	"github.com/zebracatai/zebracat-cli/internal/ui"
)

var flagDevice bool

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Log in, log out, and check authentication",
}

var authLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Log in with your Zebracat account (OAuth)",
	Long: `Log in via your browser. Usage is billed from your plan credits.

On a headless/remote machine use --device to get a URL + code to paste back.
For CI, skip login entirely and set ZEBRACAT_API_KEY (pay-as-you-go).`,
	RunE: func(cmd *cobra.Command, args []string) error {
		s, err := settings()
		if err != nil {
			return err
		}
		ctx, cancel := ctxTimeout(5 * time.Minute)
		defer cancel()

		prompt := func(u string) {
			ui.Info("Opening your browser to sign in. If it doesn't open, visit:")
			fmt.Fprintln(os.Stderr, "  "+u)
		}
		readCode := func() (string, error) {
			fmt.Fprint(os.Stderr, "Paste the code here: ")
			line, err := bufio.NewReader(os.Stdin).ReadString('\n')
			return line, err
		}

		sp := ui.StartSpinner("Waiting for sign-in…")
		creds, err := auth.Login(ctx, config.ResolveBaseURL(s), flagDevice, prompt, readCode)
		sp.Stop()
		if err != nil {
			return err
		}
		if err := config.SaveCredentials(creds); err != nil {
			return clierr.API("could not save credentials: %v", err)
		}
		ui.Success("Logged in. Credentials saved to ~/.zebracat/credentials.json")
		return emit(map[string]any{"status": "logged_in", "auth": "oauth"}, func() {})
	},
}

var authLogoutCmd = &cobra.Command{
	Use:   "logout",
	Short: "Remove stored credentials",
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := config.ClearCredentials(); err != nil {
			return clierr.API("could not clear credentials: %v", err)
		}
		ui.Success("Logged out.")
		return emit(map[string]any{"status": "logged_out"}, func() {})
	},
}

var authStatusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show the current authentication state",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		mode := c.AuthMode()
		out := map[string]any{"authenticated": c.IsAuthenticated(), "auth": mode}
		return emit(out, func() {
			if c.IsAuthenticated() {
				ui.Success("Authenticated (%s)", mode)
			} else {
				ui.Warn("Not authenticated. Run `zebracat auth login` or set ZEBRACAT_API_KEY.")
			}
		})
	},
}

var authWhoamiCmd = &cobra.Command{
	Use:   "whoami",
	Short: "Show the logged-in account and balances",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		ctx, cancel := ctxTimeout(60 * time.Second)
		defer cancel()
		var acct map[string]any
		if _, err := c.Do(ctx, "GET", "/api/public/account", nil, &acct); err != nil {
			return err
		}
		return emit(acct, func() {
			pairs := [][2]string{{"Email", fmt.Sprint(acct["email"])}, {"Plan", fmt.Sprint(acct["plan"])}}
			if ac, ok := acct["account_credit"].(map[string]any); ok {
				pairs = append(pairs, [2]string{"Plan credit left", fmt.Sprint(ac["remaining"])})
			}
			pairs = append(pairs, [2]string{"API $ balance", fmt.Sprint(acct["api_dollar_balance"])})
			ui.KV(pairs)
		})
	},
}

func init() {
	authLoginCmd.Flags().BoolVar(&flagDevice, "device", false, "Use device flow (paste a code) instead of opening a browser")
	authCmd.AddCommand(authLoginCmd, authLogoutCmd, authStatusCmd, authWhoamiCmd)
	rootCmd.AddCommand(authCmd)
}

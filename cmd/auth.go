package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/zebracatai/zebracat-cli/internal/client"
	"github.com/zebracatai/zebracat-cli/internal/clierr"
	"github.com/zebracatai/zebracat-cli/internal/config"
	"github.com/zebracatai/zebracat-cli/internal/ui"
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Log in, log out, and check authentication",
}

var authLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Log in with your Zebracat API key",
	Long: `Log in with your Zebracat API key. The CLI uses the public API, which is
billed pay-as-you-go from your API dollar balance. Create a key at
https://studio.zebracat.ai → API Keys.

  zebracat auth login                # paste your API key (or pass --api-key)
  echo "$KEY" | zebracat auth login  # read the key from stdin (CI-friendly)

For non-interactive use you can also just set ZEBRACAT_API_KEY and skip login.`,
	RunE: func(cmd *cobra.Command, args []string) error { return apiKeyLogin() },
}

// apiKeyLogin stores an API key (from --api-key, stdin, or an interactive
// prompt) and verifies it against /account.
func apiKeyLogin() error {
	key := strings.TrimSpace(flagAPIKey)
	if key == "" {
		if ui.IsTTY(os.Stdin) {
			fmt.Fprint(os.Stderr, "Enter your Zebracat API key (https://studio.zebracat.ai → API Keys): ")
		}
		line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
		key = strings.TrimSpace(line)
	}
	if key == "" {
		return clierr.Usage("no API key provided")
	}

	creds, _ := config.LoadCredentials()
	if creds == nil {
		creds = &config.Credentials{}
	}
	creds.APIKey = key
	if err := config.SaveCredentials(creds); err != nil {
		return clierr.API("could not save credentials: %v", err)
	}

	s, _ := settings()
	c := client.New(config.ResolveBaseURL(s), creds, "")
	ctx, cancel := ctxTimeout(30 * time.Second)
	defer cancel()
	var acct map[string]any
	if _, err := c.Do(ctx, "GET", "/api/v1/public/account", nil, &acct); err != nil {
		return clierr.Auth("that API key didn't work: %v", err)
	}
	ui.Success("Logged in as %v. Key saved to ~/.zebracat/credentials.json", acct["email"])
	return emit(map[string]any{"status": "logged_in", "auth": "api_key", "email": acct["email"]}, func() {})
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
		if _, err := c.Do(ctx, "GET", "/api/v1/public/account", nil, &acct); err != nil {
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
	authCmd.AddCommand(authLoginCmd, authLogoutCmd, authStatusCmd, authWhoamiCmd)
	rootCmd.AddCommand(authCmd)
}

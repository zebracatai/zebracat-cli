package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/zebracatai/zebracat-cli/internal/auth"
	"github.com/zebracatai/zebracat-cli/internal/client"
	"github.com/zebracatai/zebracat-cli/internal/clierr"
	"github.com/zebracatai/zebracat-cli/internal/config"
	"github.com/zebracatai/zebracat-cli/internal/ui"
)

var (
	flagDevice bool
	flagOAuth  bool
)

var authCmd = &cobra.Command{
	Use:   "auth",
	Short: "Log in, log out, and check authentication",
}

var authLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Save your API key (or use --oauth to sign in via browser)",
	Long: `By default, log in with your Zebracat API key — the public API uses key auth,
billed pay-as-you-go from your API dollar balance. Create a key at
https://studio.zebracat.ai → API Keys.

  zebracat auth login                # paste your API key (or pass --api-key)
  echo "$KEY" | zebracat auth login  # read the key from stdin (CI-friendly)
  zebracat auth login --oauth        # browser sign-in, billed from plan credits

For non-interactive use you can also just set ZEBRACAT_API_KEY and skip login.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if flagOAuth {
			return oauthLogin()
		}
		return apiKeyLogin()
	},
}

// apiKeyLogin stores an API key (from --api-key, stdin, or an interactive
// prompt) and verifies it against /account.
func apiKeyLogin() error {
	key := strings.TrimSpace(flagAPIKey)
	if key == "" {
		if ui.IsTTY(os.Stdin) {
			fmt.Fprint(os.Stderr, "Paste your Zebracat API key (https://studio.zebracat.ai → API Keys): ")
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
	creds.AccessToken, creds.RefreshToken, creds.ClientID = "", "", "" // drop any stale OAuth session
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

// oauthLogin runs the browser/device OAuth flow (opt-in, plan-credit billing).
func oauthLogin() error {
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
	authLoginCmd.Flags().BoolVar(&flagOAuth, "oauth", false, "Sign in via browser (plan-credit billing) instead of an API key")
	authLoginCmd.Flags().BoolVar(&flagDevice, "device", false, "With --oauth: paste a code instead of opening a browser")
	authCmd.AddCommand(authLoginCmd, authLogoutCmd, authStatusCmd, authWhoamiCmd)
	rootCmd.AddCommand(authCmd)
}

package cmd

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/spf13/cobra"

	"github.com/zebracatai/zebracat-cli/internal/client"
	"github.com/zebracatai/zebracat-cli/internal/clierr"
	"github.com/zebracatai/zebracat-cli/internal/config"
	"github.com/zebracatai/zebracat-cli/internal/ui"
)

var authCmd = &cobra.Command{
	Use:     "auth",
	Short:   "Log in, log out, and check authentication",
	Example: "  zebracat auth login\n  zebracat auth whoami",
}

var authLoginCmd = &cobra.Command{
	Use:   "login",
	Short: "Log in with your Zebracat API key",
	Long: `Log in with your Zebracat API key. The CLI uses the public API, which is
billed pay-as-you-go from your API dollar balance. Create a key at
https://studio.zebracat.ai → API Keys.

The key is checked first and saved only when it works: a wrong key never
replaces the one you have. On a terminal the key you paste is not shown.

For scripts and agents, set ZEBRACAT_API_KEY and skip login, or pipe the key
in. Avoid --api-key: it stays in your shell history and shows in ps.`,
	Example: `  zebracat auth login                  # paste your API key (hidden)
  echo "$KEY" | zebracat auth login    # read the key from stdin (CI)
  export ZEBRACAT_API_KEY=...          # or skip login entirely`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error { return apiKeyLogin() },
}

// readKey reads the API key: hidden from a terminal, else one line of stdin.
var readKey = func() (string, error) {
	if ui.IsTTY(os.Stdin) {
		fmt.Fprint(os.Stderr, "Enter your Zebracat API key (https://studio.zebracat.ai → API Keys): ")
		b, err := term.ReadPassword(os.Stdin.Fd())
		fmt.Fprintln(os.Stderr)
		return strings.TrimSpace(string(b)), err
	}
	line, err := bufio.NewReader(stdin).ReadString('\n')
	if err != nil && line == "" {
		return "", nil
	}
	return strings.TrimSpace(line), nil
}

// apiKeyLogin checks an API key (from --api-key, stdin, or a hidden prompt)
// against /account and only then saves it. A key that fails the check is
// never written, so the stored key (if any) stays byte for byte.
func apiKeyLogin() error {
	key := strings.TrimSpace(flagAPIKey)
	if key == "" {
		k, err := readKey()
		if err != nil {
			return clierr.Usage("could not read the API key: %v", err)
		}
		key = k
	}
	if key == "" {
		e := clierr.Usage("no API key provided")
		e.Hint = "Paste the key at the prompt, pipe it in (echo \"$KEY\" | zebracat auth login), or set ZEBRACAT_API_KEY."
		return e
	}

	// Check this key and only this key: the flag slot wins over
	// ZEBRACAT_API_KEY and the stored key.
	s, err := settings()
	if err != nil {
		return err
	}
	c := client.New(config.ResolveBaseURL(s), &config.Credentials{}, key)
	ctx, cancel := ctxTimeout(30 * time.Second)
	defer cancel()
	var acct map[string]any
	if _, err := c.Do(ctx, "GET", "/api/v1/public/account", nil, &acct); err != nil {
		ce := asCLIError(err)
		if ce.Exit == clierr.CodeAuth {
			ce.Message = "that API key was rejected: " + ce.Message
		} else {
			ce.Message = "could not check the API key: " + ce.Message
		}
		ce.Hint = strings.TrimSpace(ce.Hint + " Nothing was saved; your stored key (if any) is unchanged.")
		return ce
	}

	creds, err := config.LoadCredentials()
	if err != nil || creds == nil {
		creds = &config.Credentials{}
	}
	creds.APIKey = key
	if err := config.SaveCredentials(creds); err != nil {
		return clierr.API("could not save credentials: %v", err)
	}
	out := map[string]any{"status": "logged_in", "auth": "api_key", "email": acct["email"]}
	return emit(out, func() {
		ui.Success("Logged in as %v. Key saved to ~/.zebracat/credentials.json", acct["email"])
		if envAPIKeySet() {
			ui.Warn("ZEBRACAT_API_KEY is set in your environment and is used before the saved key.")
		}
	})
}

// envAPIKeySet reports whether ZEBRACAT_API_KEY overrides the saved key.
func envAPIKeySet() bool { return strings.TrimSpace(os.Getenv("ZEBRACAT_API_KEY")) != "" }

var authLogoutCmd = &cobra.Command{
	Use:     "logout",
	Short:   "Remove stored credentials",
	Example: "  zebracat auth logout",
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := config.ClearCredentials(); err != nil {
			return clierr.API("could not clear credentials: %v", err)
		}
		return emit(map[string]any{"status": "logged_out"}, func() {
			ui.Success("Logged out.")
			if envAPIKeySet() {
				ui.Warn("ZEBRACAT_API_KEY is still set in your environment; unset it to stop using that key.")
			}
		})
	},
}

var authStatusCmd = &cobra.Command{
	Use:     "status",
	Short:   "Show whether a key is set (no network call; whoami checks it)",
	Example: "  zebracat auth status --json",
	Args:    cobra.NoArgs,
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
	Use:     "whoami",
	Short:   "Show the logged-in account and balances",
	Example: "  zebracat auth whoami\n  zebracat auth whoami --json | jq .email",
	Args:    cobra.NoArgs,
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

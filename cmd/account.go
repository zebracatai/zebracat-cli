package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/zebracatai/zebracat-cli/internal/ui"
)

var accountCmd = &cobra.Command{Use: "account", Short: "Account, balances and pricing"}

var accountShowCmd = &cobra.Command{
	Use:   "show",
	Short: "Show your account, plan and credit balances",
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
				pairs = append(pairs, [2]string{"Plan credit", fmt.Sprintf("%v / %v left", ac["remaining"], ac["limit"])})
			}
			pairs = append(pairs, [2]string{"API $ balance", fmt.Sprint(acct["api_dollar_balance"])})
			ui.Heading("Account")
			ui.KV(pairs)
		})
	},
}

var accountPricingCmd = &cobra.Command{
	Use:   "pricing",
	Short: "Show pay-as-you-go pricing",
	RunE:  func(cmd *cobra.Command, args []string) error { return getAndEmit("/api/v1/public/pricing") },
}

var accountUsageCmd = &cobra.Command{
	Use:   "usage",
	Short: "Show API usage summary",
	RunE:  func(cmd *cobra.Command, args []string) error { return getAndEmit("/api/v1/public/credit/usage") },
}

func init() {
	accountCmd.AddCommand(accountShowCmd, accountPricingCmd, accountUsageCmd)
	rootCmd.AddCommand(accountCmd)
}

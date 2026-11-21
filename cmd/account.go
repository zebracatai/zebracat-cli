package cmd

import (
	"fmt"
	"net/url"
	"strconv"
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
	RunE: func(cmd *cobra.Command, args []string) error {
		out, err := getJSON("/api/v1/public/pricing")
		if err != nil {
			return err
		}
		return emit(out, func() { printPricing(out) })
	},
}

var (
	usageDetail bool
	usagePage   int
	usageSize   int
)

var accountUsageCmd = &cobra.Command{
	Use:   "usage [--detail]",
	Short: "Show API usage: per key, or every charge with --detail",
	Example: `  zebracat account usage
  zebracat account usage --detail --page 2`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if !usageDetail {
			return getAndEmit("/api/v1/public/credit/usage")
		}
		q := url.Values{"page": {strconv.Itoa(usagePage)}, "page_size": {strconv.Itoa(usageSize)}}
		return getAndEmit(withQuery("/api/v1/public/credit/usage/detail", q))
	},
}

func init() {
	uf := accountUsageCmd.Flags()
	uf.BoolVar(&usageDetail, "detail", false, "Every charge, newest first, one page at a time")
	uf.IntVar(&usagePage, "page", 1, "Page (with --detail)")
	uf.IntVar(&usageSize, "page-size", 50, "Rows per page (with --detail)")
	accountCmd.AddCommand(accountShowCmd, accountPricingCmd, accountUsageCmd)
	rootCmd.AddCommand(accountCmd)
}

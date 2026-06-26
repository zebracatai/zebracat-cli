package cmd

import (
	"time"

	"github.com/spf13/cobra"

	"github.com/zebracatai/zebracat-cli/internal/clierr"
	"github.com/zebracatai/zebracat-cli/internal/ui"
)

var (
	whURL    string
	whName   string
	whEvents []string
)

var webhookCmd = &cobra.Command{Use: "webhook", Short: "Manage webhooks"}

var webhookListCmd = &cobra.Command{
	Use:   "list",
	Short: "List webhooks",
	RunE:  func(cmd *cobra.Command, args []string) error { return getAndEmit("/api/v1/public/webhooks") },
}

var webhookCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a webhook",
	Long:  "Events: video.completed, video.failed, project.created, project.updated, credit.low (omit --events for all).",
	RunE: func(cmd *cobra.Command, args []string) error {
		if whURL == "" {
			return clierr.Usage("--url is required")
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		body := map[string]any{"url": whURL}
		if whName != "" {
			body["name"] = whName
		}
		if len(whEvents) > 0 {
			body["events"] = whEvents
		}
		ctx, cancel := ctxTimeout(60 * time.Second)
		defer cancel()
		var out any
		if _, err := c.Do(ctx, "POST", "/api/v1/public/webhooks", body, &out); err != nil {
			return err
		}
		return emit(out, func() { ui.Success("Webhook created") })
	},
}

var webhookDeleteCmd = &cobra.Command{
	Use:   "delete <id>",
	Short: "Delete a webhook",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		ctx, cancel := ctxTimeout(60 * time.Second)
		defer cancel()
		if _, err := c.Do(ctx, "DELETE", "/api/v1/public/webhooks/"+args[0], nil, nil); err != nil {
			return err
		}
		ui.Success("Webhook %s deleted", args[0])
		return emit(map[string]any{"status": "deleted", "id": args[0]}, func() {})
	},
}

func init() {
	webhookCreateCmd.Flags().StringVar(&whURL, "url", "", "Endpoint URL to receive events")
	webhookCreateCmd.Flags().StringVar(&whName, "name", "", "Optional label")
	webhookCreateCmd.Flags().StringSliceVar(&whEvents, "events", nil, "Event types (comma-separated)")
	webhookCmd.AddCommand(webhookListCmd, webhookCreateCmd, webhookDeleteCmd)
	rootCmd.AddCommand(webhookCmd)
}

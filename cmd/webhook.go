package cmd

import (
	"strings"

	"github.com/spf13/cobra"

	"github.com/zebracatai/zebracat-cli/internal/clierr"
	"github.com/zebracatai/zebracat-cli/internal/ui"
)

var (
	whURL    string
	whName   string
	whEvents []string
	whActive string
	whRotate bool
)

// whEventsHelp lists the events the server sends (its schema's enum).
const whEventsHelp = `Events: video.plan_ready, video.needs_input, video.generating, video.completed,
video.failed, character_swap.completed, character_swap.failed, and
studio.job.completed / studio.job.failed for clone and translate jobs. Omit
--events for all of them. The URL must be https; every call is signed with the
webhook's secret (see the Webhooks guide).`

var webhookCmd = &cobra.Command{Use: "webhook", Short: "Signed callbacks when your videos change"}

var webhookListCmd = &cobra.Command{
	Use:     "list",
	Short:   "List webhooks",
	Example: `  zebracat webhook list`,
	Args:    cobra.NoArgs,
	RunE:    func(cmd *cobra.Command, args []string) error { return getAndEmit(pub + "/webhooks") },
}

var webhookGetCmd = &cobra.Command{
	Use:     "get <id>",
	Short:   "Show one webhook",
	Example: `  zebracat webhook get 12`,
	Args:    cobra.ExactArgs(1),
	RunE:    func(cmd *cobra.Command, args []string) error { return getAndEmit(pub + "/webhooks/" + esc(args[0])) },
}

var webhookCreateCmd = &cobra.Command{
	Use:     "create --url <https URL>",
	Short:   "Create a webhook",
	Long:    "Create a webhook.\n\n" + whEventsHelp,
	Example: `  zebracat webhook create --url https://example.com/hooks/zebracat --events video.completed,video.failed`,
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if whURL == "" {
			return clierr.Usage("--url is required")
		}
		if err := checkHTTPS("--url", whURL); err != nil {
			return err
		}
		body := map[string]any{"url": whURL}
		if whName != "" {
			body["name"] = whName
		}
		if len(whEvents) > 0 {
			body["events"] = whEvents
		}
		out, err := sendJSON("POST", pub+"/webhooks", body)
		if err != nil {
			return err
		}
		return emit(out, func() { ui.Success("Webhook created"); ui.Auto(out) })
	},
}

var webhookUpdateCmd = &cobra.Command{
	Use:   "update <id> [--url] [--name] [--events] [--active true|false] [--rotate-secret]",
	Short: "Change a webhook (URL, name, events, on/off, new secret)",
	Long:  "Change a webhook. Only the flags you pass change.\n\n" + whEventsHelp,
	Example: `  zebracat webhook update 12 --events video.completed
  zebracat webhook update 12 --active false
  zebracat webhook update 12 --rotate-secret`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		body := map[string]any{}
		if whURL != "" {
			if err := checkHTTPS("--url", whURL); err != nil {
				return err
			}
			body["url"] = whURL
		}
		if whName != "" {
			body["name"] = whName
		}
		if len(whEvents) > 0 {
			body["events"] = whEvents
		}
		switch strings.ToLower(whActive) {
		case "":
		case "true", "yes", "on":
			body["is_active"] = true
		case "false", "no", "off":
			body["is_active"] = false
		default:
			return clierr.Usage("--active takes true or false, got %q", whActive)
		}
		if whRotate {
			body["rotate_secret"] = true
		}
		if len(body) == 0 {
			return clierr.Usage("nothing to change: pass --url, --name, --events, --active or --rotate-secret")
		}
		out, err := sendJSON("PATCH", pub+"/webhooks/"+esc(args[0]), body)
		if err != nil {
			return err
		}
		return emit(out, func() { ui.Success("Webhook %s updated", args[0]) })
	},
}

var webhookTestCmd = &cobra.Command{
	Use:   "test <id>",
	Short: "Send a signed sample event to a webhook",
	Long: `Send a signed sample event to the webhook's URL now and print what your
endpoint answered. Nothing is made or charged.`,
	Example: `  zebracat webhook test 12`,
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return postAndEmit(pub+"/webhooks/"+esc(args[0])+"/test", map[string]any{})
	},
}

var webhookDeleteCmd = &cobra.Command{
	Use:     "delete <id>",
	Short:   "Delete a webhook",
	Example: `  zebracat webhook delete 12`,
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return deleteAndEmit(pub+"/webhooks/"+esc(args[0]), args[0])
	},
}

// checkHTTPS: webhook endpoints are https only.
func checkHTTPS(flag, v string) error {
	if err := checkWebhookURL(v); err != nil {
		e := clierr.Usage("%s must be an https URL, got %q", flag, v)
		e.Code = "invalid_webhook_url"
		return e
	}
	return nil
}

func init() {
	for _, c := range []*cobra.Command{webhookCreateCmd, webhookUpdateCmd} {
		c.Flags().StringVar(&whURL, "url", "", "The https endpoint that receives events")
		c.Flags().StringVar(&whName, "name", "", "A label")
		c.Flags().StringSliceVar(&whEvents, "events", nil, "Event types, comma-separated (default: all)")
	}
	webhookUpdateCmd.Flags().StringVar(&whActive, "active", "", "Turn it on (true) or off (false)")
	webhookUpdateCmd.Flags().BoolVar(&whRotate, "rotate-secret", false, "Make a new signing secret (the answer shows it)")
	webhookCmd.AddCommand(webhookListCmd, webhookGetCmd, webhookCreateCmd, webhookUpdateCmd, webhookTestCmd, webhookDeleteCmd)
	rootCmd.AddCommand(webhookCmd)
}

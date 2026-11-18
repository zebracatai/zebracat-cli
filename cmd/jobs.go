package cmd

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/zebracatai/zebracat-cli/internal/client"
	"github.com/zebracatai/zebracat-cli/internal/clierr"
	"github.com/zebracatai/zebracat-cli/internal/ui"
	"github.com/zebracatai/zebracat-cli/internal/videos"
)

// The `zebracat studio …` commands (script, hooks, voice, image, clip, job) were
// removed with the dashboard's Studio (owner 2026-11-18). What is left here is
// the job polling Character Swap uses, and the video catalogues.

var jobDone = map[string]bool{
	"completed": true, "done": true, "succeeded": true, "failed": true, "error": true, "cancelled": true,
}

// waitJob polls a job until it finishes, honouring Retry-After on a 429.
func waitJob(c *client.Client, path string, timeout time.Duration) (map[string]any, error) {
	sp := ui.StartSpinner("Working…")
	defer sp.Stop()
	deadline := clock.Now().Add(timeout)
	var last map[string]any
	for {
		out, err := getMap(c, path)
		sleep := pollInterval
		if err != nil {
			var ce *clierr.Error
			if !(errors.As(err, &ce) && ce.Status == 429) {
				return nil, err
			}
			if ce.RetryAfter > 0 {
				sleep = ce.RetryAfter
			}
		} else {
			last = out
			if s, _ := out["status"].(string); jobDone[s] {
				return out, nil
			}
		}
		if !clock.Now().Add(sleep).Before(deadline) {
			return last, clierr.Timeout("timed out after %s waiting for %s", timeout, path)
		}
		clock.Sleep(sleep)
	}
}

// printJobHuman prints a job's id and status, its quote, and how to follow it.
func printJobHuman(out map[string]any, follow string) {
	pairs := [][2]string{}
	for _, k := range []string{"job_id", "tool", "kind", "status"} {
		if v, ok := out[k]; ok {
			pairs = append(pairs, [2]string{k, fmt.Sprint(v)})
		}
	}
	ui.KV(pairs)
	if q, ok := out["quote"].(map[string]any); ok {
		ui.Info("Quote: %s", videos.QuoteLine(q))
	}
	if s, _ := out["status"].(string); !jobDone[s] && out["job_id"] != nil && follow != "" {
		ui.Info("Follow it: %s %v --wait", follow, out["job_id"])
	}
}

// ---- catalogues -------------------------------------------------------------

var modelsCmd = &cobra.Command{Use: "models", Short: "Video qualities and the models behind them"}
var modelsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List qualities, their models and credits per 5 s",
	RunE:  func(cmd *cobra.Command, args []string) error { return getAndEmit(videos.Base + "/video/models") },
}

var presetsCmd = &cobra.Command{Use: "presets", Short: "Video presets (street interview, podcast, UGC ad, …)"}
var presetsListCmd = &cobra.Command{
	Use:   "list",
	Short: "List presets for `video create --preset`",
	RunE:  func(cmd *cobra.Command, args []string) error { return getAndEmit(videos.Base + "/video/presets") },
}

func init() {
	modelsCmd.AddCommand(modelsListCmd)
	presetsCmd.AddCommand(presetsListCmd)
	rootCmd.AddCommand(modelsCmd, presetsCmd)
}

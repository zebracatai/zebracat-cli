package cmd

import (
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/spf13/cobra"

	"github.com/zebracatai/zebracat-cli/internal/client"
	"github.com/zebracatai/zebracat-cli/internal/clierr"
	"github.com/zebracatai/zebracat-cli/internal/ui"
	"github.com/zebracatai/zebracat-cli/internal/videos"
)

var (
	swVideo     string
	swCharacter string
	swPerson    string
	swConsent   bool
	swDryRun    bool
	swYes       bool
	swWait      bool
	swIdemKey   string
	swTimeout   time.Duration
	swJobWait   bool
)

const swapBase = videos.Base + "/character-swap"

// swapJobPath is a swap job's route.
func swapJobPath(jobID string) string { return swapBase + "/" + url.PathEscape(jobID) }

// swapJobDone are the statuses a swap job stops at.
var swapJobDone = map[string]bool{
	"completed": true, "done": true, "succeeded": true, "failed": true, "error": true, "cancelled": true,
}

// requireSwap answers a clear error when the server's OpenAPI lists no
// Character Swap routes (instead of a 404 from the analysis). A schema that
// can't be read is not proof of absence: the call goes ahead.
func requireSwap(c *client.Client, cmd *cobra.Command) error {
	if c.LacksPath(cmd.Context(), "/character-swap") {
		return clierr.APIHint("Character Swap is on the dashboard at https://studio.zebracat.ai",
			"%s has no Character Swap API", c.BaseURL)
	}
	return nil
}

var swapCmd = &cobra.Command{
	Use:   "swap --video <url> --character <image-url> --consent",
	Short: "Character swap: put a face into a clip (paid per second; --dry-run for the quote)",
	Long: `Put the face from --character into the person in --video, made like every
video: the result is a project you can open in the editor and export.

  zebracat swap --video https://…/clip.mp4 --character https://…/face.jpg --consent --dry-run
  zebracat swap --video https://…/clip.mp4 --character https://…/face.jpg --consent --yes --wait

The clip is analysed first (people in it, length, price). --person picks who is
swapped (default: the main person). --consent states that you have the right to
use this face; the swap is refused without it. On a terminal the quote is shown
and you are asked; scripts must pass --yes. Paid plans only on the dashboard;
API keys pay from the API balance.

Read a swap job later with ` + "`zebracat swap status <job_id> [--wait]`" + `.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := checkURL("--video", swVideo); err != nil {
			return err
		}
		if err := checkURL("--character", swCharacter); err != nil {
			return err
		}
		if !swConsent {
			e := clierr.Usage("pass --consent to confirm you have the right to use this face")
			e.Code = "consent_required"
			return e
		}
		if !swDryRun && !swYes && !interactive() {
			e := clierr.Usage("a swap spends credits: pass --yes to confirm (or --dry-run for the quote)")
			e.Code = "confirmation_required"
			return e
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		if err := requireSwap(c, cmd); err != nil {
			return err
		}
		ctx, cancel := ctxTimeout(5 * time.Minute)
		defer cancel()
		var analysis map[string]any
		if _, err := c.Do(ctx, "POST", swapBase+"/analyze", map[string]any{"video_url": swVideo}, &analysis); err != nil {
			return err
		}
		person := swPerson
		if person == "" {
			person = mainPerson(analysis)
		}
		if person == "" {
			return clierr.API("no one to swap was found in the video")
		}
		body := map[string]any{
			"analysis_id":         analysis["analysis_id"],
			"person":              person,
			"character_image_url": swCharacter,
			"consent":             true,
		}
		quote, _ := analysis["quote"].(map[string]any)
		if credits, ok := quote["credits"].(float64); ok {
			body["quoted_credits"] = int(credits)
		}
		if swDryRun {
			body["dry_run"] = true
		} else if !swYes {
			if !confirm(fmt.Sprintf("Swap %v s of video for %v credits?", quote["seconds"], quote["credits"])) {
				out := map[string]any{"analysis": analysis, "swapped": false}
				return emit(out, func() { ui.Info("Not swapped. Nothing was charged.") })
			}
		}
		key := swIdemKey
		if key == "" {
			key = newIdempotencyKey()
		}
		var out map[string]any
		if err := postIdempotent(c, swapBase, body, key, swWait, swTimeout, &out); err != nil {
			return err
		}
		if out == nil {
			out = map[string]any{}
		}
		out["idempotency_key"] = key
		out["analysis_id"] = analysis["analysis_id"]
		out["person"] = person
		if dry, _ := out["dry_run"].(bool); dry {
			return emit(out, func() { printDryRunHuman(out) })
		}
		jobID, _ := out["job_id"].(string)
		if swWait && jobID != "" {
			final, err := waitSwapJob(c, swapJobPath(jobID), swTimeout)
			if err != nil {
				return err
			}
			final["idempotency_key"] = key
			out = final
		}
		return emit(out, func() { printSwapHuman(out) })
	},
}

// mainPerson is the analysed person marked is_main, else the first selectable one.
func mainPerson(analysis map[string]any) string {
	var first string
	for _, p := range asList(analysis["people"]) {
		pm, _ := p.(map[string]any)
		id, _ := pm["id"].(string)
		if id == "" || pm["selectable"] == false {
			continue
		}
		if pm["is_main"] == true {
			return id
		}
		if first == "" {
			first = id
		}
	}
	return first
}

var swapStatusCmd = &cobra.Command{
	Use:   "status <job_id>",
	Short: "Read a swap job (optionally --wait)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		if err := requireSwap(c, cmd); err != nil {
			return err
		}
		var out map[string]any
		if swJobWait {
			out, err = waitSwapJob(c, swapJobPath(args[0]), swTimeout)
		} else {
			out, err = getMap(c, swapJobPath(args[0]))
		}
		if err != nil {
			return err
		}
		return emit(out, func() { printSwapHuman(out) })
	},
}

// waitSwapJob polls a job until it finishes, honouring Retry-After on a 429.
func waitSwapJob(c *client.Client, path string, timeout time.Duration) (map[string]any, error) {
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
			if s, _ := out["status"].(string); swapJobDone[s] {
				return out, nil
			}
		}
		if !clock.Now().Add(sleep).Before(deadline) {
			return last, clierr.Timeout("timed out after %s waiting for %s", timeout, path)
		}
		clock.Sleep(sleep)
	}
}

func printSwapHuman(out map[string]any) {
	pairs := [][2]string{}
	for _, k := range []string{"job_id", "status"} {
		if v, ok := out[k]; ok {
			pairs = append(pairs, [2]string{k, fmt.Sprint(v)})
		}
	}
	ui.KV(pairs)
	if q, ok := out["quote"].(map[string]any); ok {
		ui.Info("Quote: %s", videos.QuoteLine(q))
	}
	res, _ := out["result"].(map[string]any)
	if u, _ := res["video_url"].(string); u != "" {
		ui.Link("▶ Video:", u)
	}
	if out["project_id"] != nil {
		ui.Link("✎ Open in studio:", videos.EditorURL(out))
	}
	if s, _ := out["status"].(string); !swapJobDone[s] && out["job_id"] != nil {
		ui.Info("Follow it: zebracat swap status %v --wait", out["job_id"])
	}
}

func init() {
	f := swapCmd.Flags()
	f.StringVar(&swVideo, "video", "", "The clip's URL (required)")
	f.StringVar(&swCharacter, "character", "", "The face image URL (required)")
	f.StringVar(&swPerson, "person", "", "Who to swap: a person id from the analysis (default: the main person)")
	f.BoolVar(&swConsent, "consent", false, "Confirm you have the right to use this face (required)")
	f.BoolVar(&swDryRun, "dry-run", false, "Validate and quote only: nothing is generated, held or charged")
	f.BoolVar(&swYes, "yes", false, "Don't ask; required when not on a terminal")
	f.BoolVar(&swWait, "wait", false, "Block until the swap is finished")
	f.StringVar(&swIdemKey, "idempotency-key", "", "Idempotency-Key header (default: a new UUID, printed in the output)")
	f.DurationVar(&swTimeout, "timeout", 30*time.Minute, "Max time to wait with --wait")
	sf := swapStatusCmd.Flags()
	sf.BoolVar(&swJobWait, "wait", false, "Block until the swap is finished")
	sf.DurationVar(&swTimeout, "timeout", 30*time.Minute, "Max time to wait with --wait")
	swapCmd.AddCommand(swapStatusCmd)
	rootCmd.AddCommand(swapCmd)
}

package cmd

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

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
)

const swapBase = videos.Base + "/character-swap"

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
API keys pay from the API balance.`,
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
			stTimeout = swTimeout
			final, err := waitStudioJob(c, studioJobPath("swap", jobID), swTimeout)
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

func printSwapHuman(out map[string]any) {
	printStudioJobHuman(out)
	res, _ := out["result"].(map[string]any)
	if u, _ := res["video_url"].(string); u != "" {
		ui.Link("▶ Video:", u)
	}
	if out["project_id"] != nil {
		ui.Link("✎ Open in studio:", videos.EditorURL(out))
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
	rootCmd.AddCommand(swapCmd)
}

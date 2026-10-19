package cmd

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/zebracatai/zebracat-cli/internal/client"
	"github.com/zebracatai/zebracat-cli/internal/clierr"
	"github.com/zebracatai/zebracat-cli/internal/ui"
	"github.com/zebracatai/zebracat-cli/internal/videos"
)

var (
	stSeconds  int
	stLanguage string
	stWait     bool
	stTimeout  time.Duration
	stScript   string
	stPlatform string
	stText     string
	stVoice    string
	stDryRun   bool
	stOut      string
	stModel    string
	stAspect   string
	stCount    int
	stImage    string
	stIdemKey  string
)

const studioBase = videos.Base + "/studio"

// studioJobPaths maps `studio job <tool>` names to their job routes.
var studioJobPaths = map[string]string{
	"script": "scripts", "scripts": "scripts",
	"voice": "voices", "voices": "voices",
	"image": "media", "images": "media", "clip": "media", "clips": "media", "media": "media",
}

var studioJobDone = map[string]bool{
	"completed": true, "done": true, "succeeded": true, "failed": true, "error": true, "cancelled": true,
}

var studioCmd = &cobra.Command{
	Use:   "studio",
	Short: "Studio tools: script, hooks, voice-over, images, clips",
	Long: `The dashboard's Studio tools, one call each.

  zebracat studio script "3 tips for better sleep" --seconds 30 --wait
  zebracat studio hooks --script script.txt
  zebracat studio voice --text script.txt --voice <id> --dry-run
  zebracat studio image "a neon city at dusk" --aspect vertical --count 2
  zebracat studio clip "a cat surfing a wave" --seconds 5 --dry-run
  zebracat studio job script <job_id>

Script, hooks and images cost no credits (daily allowances apply). Voices and
clips are paid: --dry-run answers the quote and spends nothing.`,
}

var studioScriptCmd = &cobra.Command{
	Use:   "script <idea>",
	Short: "Write a video script from an idea (free, daily allowance)",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		out, err := studioScript(c, strings.Join(args, " "), stSeconds, stLanguage, stWait)
		if err != nil {
			return err
		}
		return emit(out, func() { printStudioJobHuman(out) })
	},
}

func studioScript(c *client.Client, idea string, seconds int, language string, wait bool) (map[string]any, error) {
	body := map[string]any{"idea": strings.TrimSpace(idea)}
	if seconds > 0 {
		body["seconds"] = seconds
	}
	if language != "" {
		body["language"] = language
	}
	return startStudioJob(c, "scripts", body, wait)
}

var studioHooksCmd = &cobra.Command{
	Use:   "hooks --script <file|->",
	Short: "Opening hooks for your script (free, daily allowance)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if stScript == "" {
			return clierr.Usage("--script <file|-> is required")
		}
		text, err := readTextArg("--script", stScript)
		if err != nil {
			return err
		}
		body := map[string]any{"script": text}
		if stPlatform != "" {
			body["platform"] = stPlatform
		}
		if stLanguage != "" {
			body["language"] = stLanguage
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		ctx, cancel := ctxTimeout(3 * time.Minute)
		defer cancel()
		var out map[string]any
		if _, err := c.Do(ctx, "POST", studioBase+"/hooks", body, &out); err != nil {
			return err
		}
		return emit(out, func() {
			ui.Heading("Hooks")
			for i, h := range asList(out["hooks"]) {
				if hm, ok := h.(map[string]any); ok {
					fmt.Printf("  %d. %v  %s\n", i+1, hm["line"], ui.Dim(fmt.Sprintf("(%v)", hm["type"])))
				}
			}
		})
	},
}

var studioVoiceCmd = &cobra.Command{
	Use:   "voice --text <file|-> --voice <id>",
	Short: "Generate a voice-over (paid; --dry-run for the quote)",
	RunE: func(cmd *cobra.Command, args []string) error {
		if stText == "" || stVoice == "" {
			return clierr.Usage("--text <file|-> and --voice <id> are required (voices: `zebracat voice list`)")
		}
		text, err := readTextArg("--text", stText)
		if err != nil {
			return err
		}
		body := map[string]any{"script": text, "voice_id": stVoice}
		if stLanguage != "" {
			body["language"] = stLanguage
		}
		if stDryRun {
			body["dry_run"] = true
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		out, err := startStudioJob(c, "voices", body, stWait || stOut != "")
		if err != nil {
			return err
		}
		if dry, _ := out["dry_run"].(bool); dry {
			return emit(out, func() { printDryRunHuman(out) })
		}
		if stOut != "" {
			res, _ := out["result"].(map[string]any)
			audio, _ := res["audio_url"].(string)
			if audio == "" {
				return clierr.API("the voice-over finished without an audio_url (status: %v)", out["status"])
			}
			if err := downloadFile(audio, stOut); err != nil {
				return err
			}
			out["saved"] = stOut
			ui.Success("Saved %s", stOut)
		}
		return emit(out, func() { printStudioJobHuman(out) })
	},
}

var studioImageCmd = &cobra.Command{
	Use:   "image <prompt>",
	Short: "Generate images (0 credits, daily allowance)",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		out, err := studioImage(c, strings.Join(args, " "), stModel, stAspect, stCount, stWait)
		if err != nil {
			return err
		}
		return emit(out, func() { printStudioJobHuman(out) })
	},
}

func studioImage(c *client.Client, prompt, model, aspect string, count int, wait bool) (map[string]any, error) {
	body := map[string]any{"prompt": strings.TrimSpace(prompt)}
	if model != "" {
		body["model"] = model
	}
	if aspect != "" {
		if !client.CheckEnum("studio_image.aspect", aspect) {
			return nil, enumUsage("--aspect", aspect, "studio_image.aspect")
		}
		body["aspect"] = aspect
	}
	if count > 0 {
		body["count"] = count
	}
	return startStudioJob(c, "images", body, wait)
}

var studioClipCmd = &cobra.Command{
	Use:   "clip <prompt> --seconds N",
	Short: "Generate a video clip (paid per started 5 s; --dry-run for the quote)",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if stSeconds <= 0 {
			return clierr.Usage("--seconds is required")
		}
		body := map[string]any{"prompt": strings.Join(args, " "), "seconds": stSeconds}
		if stModel != "" {
			body["model"] = stModel
		}
		if stAspect != "" {
			body["aspect"] = stAspect
		}
		if stImage != "" {
			if err := checkURL("--image", stImage); err != nil {
				return err
			}
			body["image_url"] = stImage
		}
		if stDryRun {
			body["dry_run"] = true
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		out, err := startStudioJob(c, "clips", body, stWait)
		if err != nil {
			return err
		}
		if dry, _ := out["dry_run"].(bool); dry {
			return emit(out, func() { printDryRunHuman(out) })
		}
		return emit(out, func() { printStudioJobHuman(out) })
	},
}

var studioJobCmd = &cobra.Command{
	Use:   "job <script|voice|image|clip> <job_id>",
	Short: "Read a Studio job (optionally --wait)",
	Args:  cobra.ExactArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		route, ok := studioJobPaths[args[0]]
		if !ok {
			return clierr.Usage("unknown tool %q (script|voice|image|clip)", args[0])
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		path := studioBase + "/" + route + "/" + url.PathEscape(args[1])
		var out map[string]any
		if stWait {
			out, err = waitStudioJob(c, path, stTimeout)
		} else {
			out, err = getMap(c, path)
		}
		if err != nil {
			return err
		}
		return emit(out, func() { printStudioJobHuman(out) })
	},
}

// startStudioJob POSTs a Studio tool with an Idempotency-Key. A dry run or a
// synchronous answer comes back as is; with wait it polls the job's self link.
func startStudioJob(c *client.Client, tool string, body map[string]any, wait bool) (map[string]any, error) {
	key := stIdemKey
	if key == "" {
		key = newIdempotencyKey()
	}
	var out map[string]any
	if err := postIdempotent(c, studioBase+"/"+tool, body, key, wait, stTimeout, &out); err != nil {
		return nil, err
	}
	if out == nil {
		out = map[string]any{}
	}
	out["idempotency_key"] = key
	jobID, _ := out["job_id"].(string)
	if dry, _ := out["dry_run"].(bool); dry || !wait || jobID == "" {
		return out, nil
	}
	self := ""
	if l, ok := out["links"].(map[string]any); ok {
		self, _ = l["self"].(string)
	}
	if self == "" {
		route := studioJobPaths[tool]
		self = studioBase + "/" + route + "/" + url.PathEscape(jobID)
	}
	final, err := waitStudioJob(c, self, stTimeout)
	if final != nil {
		final["idempotency_key"] = key
	}
	return final, err
}

// waitStudioJob polls a job until it finishes, honouring Retry-After on a 429.
func waitStudioJob(c *client.Client, path string, timeout time.Duration) (map[string]any, error) {
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
			if s, _ := out["status"].(string); studioJobDone[s] {
				return out, nil
			}
		}
		if !clock.Now().Add(sleep).Before(deadline) {
			return last, clierr.Timeout("timed out after %s waiting for %s", timeout, path)
		}
		clock.Sleep(sleep)
	}
}

func printStudioJobHuman(out map[string]any) {
	pairs := [][2]string{}
	for _, k := range []string{"job_id", "tool", "status"} {
		if v, ok := out[k]; ok {
			pairs = append(pairs, [2]string{k, fmt.Sprint(v)})
		}
	}
	ui.KV(pairs)
	res, _ := out["result"].(map[string]any)
	if t, _ := res["narration_text"].(string); t != "" {
		ui.Heading(fmt.Sprint(res["title"]))
		fmt.Println(t)
	}
	if a, _ := res["audio_url"].(string); a != "" {
		ui.Link("♪ Audio:", a)
	}
	for _, u := range asList(res["urls"]) {
		ui.Link("▣", fmt.Sprint(u))
	}
	if q, ok := out["quote"].(map[string]any); ok {
		ui.Info("Quote: %s", videos.QuoteLine(q))
	}
	if s, _ := out["status"].(string); !studioJobDone[s] && out["job_id"] != nil {
		ui.Info("Follow it: zebracat studio job %v %v --wait", out["tool"], out["job_id"])
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
	studioScriptCmd.Flags().IntVar(&stSeconds, "seconds", 0, "Target length in seconds")
	studioScriptCmd.Flags().StringVar(&stLanguage, "language", "", "Language")
	studioScriptCmd.Flags().BoolVar(&stWait, "wait", false, "Block until the script is written")

	studioHooksCmd.Flags().StringVar(&stScript, "script", "", "Your script: a file, or - for stdin (required)")
	studioHooksCmd.Flags().StringVar(&stPlatform, "platform", "", "Platform (default tiktok)")
	studioHooksCmd.Flags().StringVar(&stLanguage, "language", "", "Language")

	vf := studioVoiceCmd.Flags()
	vf.StringVar(&stText, "text", "", "The words: a file, or - for stdin (required)")
	vf.StringVar(&stVoice, "voice", "", "Voice ID (required; see `zebracat voice list`)")
	vf.StringVar(&stLanguage, "language", "", "Language")
	vf.BoolVar(&stDryRun, "dry-run", false, "Answer the quote; spend nothing")
	vf.BoolVar(&stWait, "wait", false, "Block until the audio is ready")
	vf.StringVarP(&stOut, "output", "o", "", "Save the audio to this file (implies --wait)")

	imf := studioImageCmd.Flags()
	imf.StringVar(&stModel, "model", "", "Image model (see the Studio media catalogue)")
	imf.StringVar(&stAspect, "aspect", "", "Aspect: "+client.EnumHelp("studio_image.aspect"))
	imf.IntVar(&stCount, "count", 0, "How many images")
	imf.BoolVar(&stWait, "wait", false, "Block until the images are ready")

	clf := studioClipCmd.Flags()
	clf.IntVar(&stSeconds, "seconds", 0, "Clip length in seconds (required)")
	clf.StringVar(&stModel, "model", "", "Video model (see `zebracat models list`)")
	clf.StringVar(&stAspect, "aspect", "", "Aspect")
	clf.StringVar(&stImage, "image", "", "Start from this image URL")
	clf.BoolVar(&stDryRun, "dry-run", false, "Answer the quote; spend nothing")
	clf.BoolVar(&stWait, "wait", false, "Block until the clip is ready")

	studioJobCmd.Flags().BoolVar(&stWait, "wait", false, "Block until the job finishes")

	for _, c := range []*cobra.Command{studioScriptCmd, studioHooksCmd, studioVoiceCmd, studioImageCmd, studioClipCmd, studioJobCmd} {
		c.Flags().DurationVar(&stTimeout, "timeout", 15*time.Minute, "Max time to wait with --wait")
		if c != studioJobCmd && c != studioHooksCmd {
			c.Flags().StringVar(&stIdemKey, "idempotency-key", "", "Idempotency-Key header (default: a new UUID)")
		}
	}
	studioCmd.AddCommand(studioScriptCmd, studioHooksCmd, studioVoiceCmd, studioImageCmd, studioClipCmd, studioJobCmd)
	modelsCmd.AddCommand(modelsListCmd)
	presetsCmd.AddCommand(presetsListCmd)
	rootCmd.AddCommand(studioCmd, modelsCmd, presetsCmd)
}

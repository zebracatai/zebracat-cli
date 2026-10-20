package cmd

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/zebracatai/zebracat-cli/internal/client"
	"github.com/zebracatai/zebracat-cli/internal/clierr"
	"github.com/zebracatai/zebracat-cli/internal/dash"
	"github.com/zebracatai/zebracat-cli/internal/ui"
	"github.com/zebracatai/zebracat-cli/internal/videos"
)

var (
	vWait      bool
	vTimeout   time.Duration
	vLimit     int
	vStatus    string
	vURL       string
	vTo        string
	vNoCaption bool
	vRender    bool
	vOut       string
)

var videoCmd = &cobra.Command{Use: "video", Short: "Create, plan, generate, track and download videos"}

var videoListCmd = &cobra.Command{
	Use:   "list",
	Short: "List your videos",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		path := fmt.Sprintf("/api/v1/public/projects?limit=%d", vLimit)
		if vStatus != "" {
			path += "&status=" + vStatus
		}
		ctx, cancel := ctxTimeout(60 * time.Second)
		defer cancel()
		var out map[string]any
		if _, err := c.Do(ctx, "GET", path, nil, &out); err != nil {
			return err
		}
		return emit(out, func() { printProjectsDashboard(out) })
	},
}

var videoGetCmd = &cobra.Command{
	Use:   "get <task_id>",
	Short: "Get a video's status and details",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		ctx, cancel := ctxTimeout(60 * time.Second)
		defer cancel()
		out, err := videos.Fetch(ctx, c, args[0])
		if err != nil {
			return err
		}
		return emit(out, func() { printStatusHuman(out) })
	},
}

var videoStatusCmd = &cobra.Command{
	Use:   "status <task_id>",
	Short: "Get a video's phase/status (optionally --wait)",
	Long: `Get a video's phase and status. With --wait, block until it needs you
(plan_ready, needs_input) or is finished (completed, failed, cancelled).
Director v2 tasks report a phase; v1 task ids keep the v1 status.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		if vWait {
			out, err := waitTask(c, args[0], vTimeout, false)
			if err != nil {
				return err
			}
			return emit(out, func() { printStatusHuman(out) })
		}
		ctx, cancel := ctxTimeout(60 * time.Second)
		defer cancel()
		out, err := videos.Fetch(ctx, c, args[0])
		if err != nil {
			return err
		}
		return emit(out, func() { printStatusHuman(out) })
	},
}

var videoCancelCmd = &cobra.Command{
	Use:   "cancel <task_id>",
	Short: "Cancel a video before it generates",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		ctx, cancel := ctxTimeout(60 * time.Second)
		defer cancel()
		var out map[string]any
		_, err = c.Do(ctx, "POST", videos.Path(args[0], "cancel"), nil, &out)
		var ce *clierr.Error
		if errors.As(err, &ce) && ce.Status == 404 { // a v1 task id
			out = nil
			_, err = c.Do(ctx, "POST", "/api/v1/public/video/"+url.PathEscape(args[0])+"/cancel", nil, &out)
		}
		if err != nil {
			return err
		}
		return emit(out, func() { ui.Success("Cancelled %s", args[0]) })
	},
}

var videoDownloadCmd = &cobra.Command{
	Use:   "download <task_id>",
	Short: "Download a finished video (or print its URL)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		ctx, cancel := ctxTimeout(5 * time.Minute)
		defer cancel()
		st, err := videos.Fetch(ctx, c, args[0])
		if err != nil {
			return err
		}
		mp4 := renderedMP4(st)
		if mp4 == "" {
			// Not a rendered file yet (e.g. an editable draft) — send them to studio.
			if s := videos.EditorURL(st); s != "" {
				return emit(map[string]any{"task_id": args[0], "studio_url": s, "status": st["status"], "phase": st["phase"]}, func() {
					ui.Info("No rendered MP4 (%s) — open it in the studio instead:", videos.PhaseOrStatus(st))
					ui.Link("✎", s)
				})
			}
			return clierr.API("video is not ready (%s)", videos.PhaseOrStatus(st))
		}
		if vOut == "" {
			return emit(map[string]any{"task_id": args[0], "video_url": mp4}, func() {
				fmt.Println(mp4)
			})
		}
		if err := downloadFile(mp4, vOut); err != nil {
			return err
		}
		ui.Success("Saved %s", vOut)
		return emit(map[string]any{"task_id": args[0], "saved": vOut}, func() {})
	},
}

var videoTranslateCmd = &cobra.Command{
	Use:   "translate",
	Short: "Translate/dub an existing video into another language",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		if vURL == "" || vTo == "" {
			return clierr.Usage("--url and --to are required")
		}
		payload := map[string]any{
			"video_url":       vURL,
			"target_language": toLangCode(vTo),
			"enable_caption":  !vNoCaption,
			"should_render":   vRender,
		}
		ctx, cancel := ctxTimeout(2 * time.Minute)
		defer cancel()
		var created map[string]any
		if _, err := c.Do(ctx, "POST", "/api/v1/public/video/translate", payload, &created); err != nil {
			return err
		}
		taskID, _ := created["task_id"].(string)
		if !vWait || taskID == "" {
			return emit(created, func() { submittedHuman(created, taskID) })
		}
		final, err := waitTask(c, taskID, vTimeout, true)
		if err != nil {
			return err
		}
		return emit(final, func() { printStatusHuman(final) })
	},
}

// --- helpers ---------------------------------------------------------------

// waitTask polls a task with a spinner on a terminal; untilDone skips plan_ready.
func waitTask(c *client.Client, taskID string, timeout time.Duration, untilDone bool) (map[string]any, error) {
	label := "Working… (a plan takes about a minute; a video a few)"
	sp := ui.StartSpinner(label)
	defer sp.Stop()
	out, err := videos.Wait(c, taskID, videos.WaitOptions{Timeout: timeout, UntilDone: untilDone, Clock: clock, Interval: pollInterval})
	if err != nil && out != nil {
		// A timeout still prints the last state on stdout for scripts.
		_ = emit(out, func() {})
	}
	return out, err
}

func printStatusHuman(m map[string]any) {
	pairs := [][2]string{{"task_id", fmt.Sprint(m["task_id"])}}
	if p := videos.Phase(m); p != "" {
		pairs = append(pairs, [2]string{"phase", phaseLabel(p)})
	}
	pairs = append(pairs, [2]string{"status", fmt.Sprint(m["status"])})
	if t, ok := m["video_type"]; ok {
		pairs = append(pairs, [2]string{"type", fmt.Sprint(t)})
	}
	switch e := m["error"].(type) {
	case string:
		if e != "" {
			pairs = append(pairs, [2]string{"error", e})
		}
	case map[string]any:
		pairs = append(pairs, [2]string{"error", fmt.Sprintf("%v (%v)", e["message"], e["code"])})
	}
	ui.KV(pairs)
	if qid, text, opts := videos.Question(m); qid != "" {
		ui.Heading("Question")
		fmt.Printf("  %s\n", text)
		for _, o := range opts {
			fmt.Printf("    • %s\n", o)
		}
		ui.Info("Answer with: zebracat video answer %v --question %s --answer \"…\"", m["task_id"], qid)
	}
	if lines := videos.SummaryLines(m); len(lines) > 0 && videos.Phase(m) == "plan_ready" {
		ui.Heading("Plan")
		for _, l := range lines {
			fmt.Println("  " + l)
		}
	}
	if q, ok := m["quote"].(map[string]any); ok {
		ui.Info("Quote: %s", videos.QuoteLine(q))
	}
	if videos.Phase(m) == "plan_ready" {
		ui.Info("Next: zebracat plan show %v · zebracat video generate %v", m["task_id"], m["task_id"])
	}
	if mp4 := renderedMP4(m); mp4 != "" {
		ui.Link("▶ Video:", mp4)
	}
	if s := videos.EditorURL(m); s != "" {
		ui.Link("✎ Open in studio:", s)
	}
}

// phaseLabel is a friendly phase name for people.
func phaseLabel(p string) string {
	switch p {
	case "planning":
		return "planning — writing the plan"
	case "needs_input":
		return "needs_input — the plan has a question for you"
	case "plan_ready":
		return "plan_ready — review, then generate"
	case "generating":
		return "generating — making the clips"
	case "rendering":
		return "rendering — exporting the MP4"
	}
	return p
}

// renderedMP4 returns the result URL only when it's an actual playable file
// (never the raw project video.json).
func renderedMP4(m map[string]any) string {
	u, _ := m["video_url"].(string)
	if strings.HasSuffix(u, ".mp4") {
		return u
	}
	return ""
}

// langCodes maps friendly language names to the 2-letter codes /video/translate
// expects. Anything already a code passes through.
var langCodes = map[string]string{
	"english": "en", "hindi": "hi", "portuguese": "pt", "chinese": "zh", "mandarin": "zh",
	"spanish": "es", "french": "fr", "german": "de", "japanese": "ja", "arabic": "ar",
	"russian": "ru", "korean": "ko", "indonesian": "id", "italian": "it", "dutch": "nl",
	"turkish": "tr", "polish": "pl", "swedish": "sv", "malay": "ms", "romanian": "ro",
	"ukrainian": "uk", "greek": "el", "czech": "cs", "danish": "da", "finnish": "fi",
	"bulgarian": "bg", "croatian": "hr", "slovak": "sk", "tamil": "ta",
}

func toLangCode(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	if c, ok := langCodes[s]; ok {
		return c
	}
	return s // assume it's already a 2-letter code
}

// submittedHuman prints the friendly "submitted" lines with a studio link.
func submittedHuman(created map[string]any, taskID string) {
	ui.Success("Submitted. task_id=%s", taskID)
	if s := videos.EditorURL(created); s != "" {
		ui.Link("✎ Track in studio:", s)
	}
}

// printProjectsDashboard renders the project list like a dashboard: a coloured
// status badge, a friendly type, when it was made, and a studio link (or the
// failure reason) — no task IDs, JSON or other technical noise.
func printProjectsDashboard(out map[string]any) {
	vids, _ := out["videos"].([]any)
	ui.Heading("Your videos")
	if len(vids) == 0 {
		ui.Info(`Nothing here yet — make one with: zebracat video create --prompt "..."`)
		return
	}
	fmt.Println()
	for _, v := range vids {
		m, _ := v.(map[string]any)
		status := fmt.Sprint(m["status"])
		badge := colorByKind(dash.Kind(status))(fmt.Sprintf("%s %-10s", dash.Icon(status), dash.Label(status)))
		kind := fmt.Sprintf("%-17s", dash.HumanType(fmt.Sprint(m["video_type"])))
		when := fmt.Sprintf("%-9s", dash.RelTime(fmt.Sprint(m["created_at"])))
		tail := ui.Dim(dash.StudioURL(m["project_id"]))
		if dash.Kind(status) == "failed" {
			if e, _ := m["error"].(string); e != "" {
				tail = ui.Red(truncate(e, 56))
			}
		}
		fmt.Printf("  %s  %s %s %s\n", badge, kind, when, tail)
	}
	fmt.Println()
	ui.Info("%v total", out["total"])
}

func colorByKind(kind string) func(string) string {
	switch kind {
	case "ok":
		return ui.Green
	case "working":
		return ui.Yellow
	case "failed":
		return ui.Red
	default:
		return ui.Dim
	}
}

func truncate(s string, n int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	if len(s) <= n {
		return s
	}
	return s[:n-1] + "…"
}

func downloadFile(url, dest string) error {
	resp, err := http.Get(url) //nolint:gosec // user-provided result URL
	if err != nil {
		return clierr.API("download failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return clierr.API("download failed (%d)", resp.StatusCode)
	}
	f, err := os.Create(dest)
	if err != nil {
		return clierr.Usage("cannot write %s: %v", dest, err)
	}
	defer f.Close()
	if _, err := io.Copy(f, resp.Body); err != nil {
		return clierr.API("download interrupted: %v", err)
	}
	return nil
}

func init() {
	videoListCmd.Flags().IntVar(&vLimit, "limit", 20, "Max rows")
	videoListCmd.Flags().StringVar(&vStatus, "status", "", "Filter by status")

	videoStatusCmd.Flags().BoolVar(&vWait, "wait", false, "Block until plan_ready, needs_input or finished")
	videoStatusCmd.Flags().DurationVar(&vTimeout, "timeout", 30*time.Minute, "Max time to wait with --wait")

	videoDownloadCmd.Flags().StringVarP(&vOut, "output", "o", "", "Save to this file instead of printing the URL")

	tf := videoTranslateCmd.Flags()
	tf.StringVar(&vURL, "url", "", "URL of the source video")
	tf.StringVar(&vTo, "to", "", "Target language (e.g. spanish or es)")
	tf.BoolVar(&vNoCaption, "no-caption", false, "Disable captions")
	tf.BoolVar(&vRender, "render", false, "Render a final MP4")
	tf.BoolVar(&vWait, "wait", false, "Block until done")
	tf.DurationVar(&vTimeout, "timeout", 30*time.Minute, "Max time to wait with --wait")

	videoCmd.AddCommand(videoCreateCmd, videoListCmd, videoGetCmd, videoStatusCmd, videoCancelCmd, videoDownloadCmd, videoTranslateCmd)
	rootCmd.AddCommand(videoCmd)
}

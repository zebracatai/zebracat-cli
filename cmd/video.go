package cmd

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
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
	vForce     bool
	vYes       bool
	vDryRun    bool
	vIdemKey   string
	vWebhook   string
	vLangs     bool
)

var videoCmd = &cobra.Command{Use: "video", Short: "Make, track, download, clone and translate videos"}

var videoListCmd = &cobra.Command{
	Use:   "list",
	Short: "List your videos",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		q := url.Values{"limit": {fmt.Sprint(vLimit)}}
		if vStatus != "" {
			q.Set("status", vStatus)
		}
		path := withQuery("/api/v1/public/projects", q)
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
Videos report a phase; task ids of the old v1 calls keep their v1 status.`,
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
			_, err = c.Do(ctx, "POST", videos.Base+"/video/"+url.PathEscape(args[0])+"/cancel", nil, &out)
		}
		if err != nil {
			return err
		}
		return emit(out, func() { ui.Success("Cancelled %s", args[0]) })
	},
}

var videoDownloadCmd = &cobra.Command{
	Use:   "download <task_id> [-o file]",
	Short: "Download a finished video (or print its URL)",
	Long: `Download a finished video's MP4. Without -o it prints the URL. With -o the
file is written to a temporary name and renamed when complete, so a broken
download never leaves half a file; an existing file is kept unless --force.
Pass -o . (or a folder) to save it as <task_id>.mp4 there.

JSON answer: {task_id, path, bytes} (or {task_id, video_url} without -o).`,
	Example: `  zebracat video download 6f1c0d2e -o video.mp4
  zebracat video download 6f1c0d2e -o .`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		id := args[0]
		mp4, st, err := resolveDownload(c, id)
		if err != nil {
			return err
		}
		if mp4 == "" {
			// Not a rendered file yet (e.g. an editable draft) — send them to the editor.
			if s := videos.EditorURL(st); s != "" {
				return emit(map[string]any{"task_id": id, "studio_url": s, "status": st["status"], "phase": st["phase"]}, func() {
					ui.Info("No rendered MP4 (%s) — open it in the editor instead:", videos.PhaseOrStatus(st))
					ui.Link("✎", s)
				})
			}
			e := clierr.API("video is not ready (%s)", videos.PhaseOrStatus(st))
			e.Code = "not_ready"
			return e
		}
		if vOut == "" {
			return emit(map[string]any{"task_id": id, "video_url": mp4}, func() { fmt.Println(mp4) })
		}
		dest := vOut
		if fi, err := os.Stat(dest); err == nil && fi.IsDir() {
			dest = filepath.Join(dest, id+".mp4")
		}
		if _, err := os.Stat(dest); err == nil && !vForce {
			e := clierr.Usage("%s exists: pass --force to overwrite it", dest)
			e.Code = "file_exists"
			return e
		}
		n, err := downloadFile(mp4, dest)
		if err != nil {
			return err
		}
		return emit(map[string]any{"task_id": id, "path": dest, "bytes": n}, func() { ui.Success("Saved %s (%d bytes)", dest, n) })
	},
}

// resolveDownload finds the MP4: the /videos/{id}/download route (a redirect
// to the file), else the old /video/{id}/download, else the task's video_url.
// The redirect is read, not followed, so the API key never goes to the file
// host. st is the task when the route could not say (nil otherwise).
func resolveDownload(c *client.Client, id string) (string, map[string]any, error) {
	for _, p := range []string{videos.Path(id, "download"), videos.Base + "/video/" + url.PathEscape(id) + "/download"} {
		loc, err := redirectTarget(c, p)
		if loc != "" {
			return loc, nil, nil
		}
		var ce *clierr.Error
		if errors.As(err, &ce) && (ce.Status == 404 || ce.Status == 405) {
			continue // this server has no such route (or not for this task): try the next
		}
		break // 409 not ready, or an answer without a redirect: read the task
	}
	ctx, cancel := ctxTimeout(60 * time.Second)
	defer cancel()
	st, err := videos.Fetch(ctx, c, id)
	if err != nil {
		return "", nil, err
	}
	return renderedMP4(st), st, nil
}

// redirectTarget GETs path without following a redirect and returns its
// Location ("" when the answer is not a redirect).
func redirectTarget(c *client.Client, path string) (string, error) {
	nc := *c
	hc := *c.HTTP
	var loc string
	hc.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		loc = req.URL.String()
		return http.ErrUseLastResponse
	}
	nc.HTTP = &hc
	ctx, cancel := ctxTimeout(60 * time.Second)
	defer cancel()
	_, err := nc.Do(ctx, "GET", path, nil, nil)
	if loc != "" {
		return loc, nil
	}
	return "", err
}

var videoTranslateCmd = &cobra.Command{
	Use:   "translate --url <video> --to <language>",
	Short: "Translate and dub a video into another language (spends credits)",
	Long: `Translate and dub a video (a link or a local file, uploaded first) into
another language. --to takes a code (es) or a name (spanish); --languages lists
them. A translation spends credits: on a terminal you are asked first; scripts
and agents pass --yes. --dry-run answers the price and spends nothing.`,
	Example: `  zebracat video translate --languages
  zebracat video translate --url clip.mp4 --to spanish --yes --wait`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if vLangs {
			return listLanguages(true)
		}
		if vURL == "" || vTo == "" {
			return clierr.Usage("--url and --to are required (--languages lists the languages)")
		}
		if err := checkMedia("--url", vURL); err != nil {
			return err
		}
		if err := checkWebhookURL(vWebhook); err != nil {
			return err
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		lang, err := translateCode(c, vTo)
		if err != nil {
			return err
		}
		if !vDryRun {
			ok, err := confirmSpend(vYes, "translate", func() (string, error) {
				return fmt.Sprintf("Translate this video into %s? It spends credits.", lang), nil
			})
			if err != nil {
				return err
			}
			if !ok {
				return emit(map[string]any{"translated": false}, func() { ui.Info("Not translated. Nothing was charged.") })
			}
		}
		src, err := mediaURL(c, "--url", vURL, "")
		if err != nil {
			return err
		}
		payload := map[string]any{
			"video_url":       src,
			"target_language": lang,
			"enable_caption":  !vNoCaption,
			"should_render":   vRender,
		}
		if vDryRun {
			payload["dry_run"] = true
		}
		if vWebhook != "" {
			payload["webhook_url"] = vWebhook
		}
		key := vIdemKey
		if key == "" {
			key = newIdempotencyKey()
		}
		var created map[string]any
		if err := postIdempotent(c, videos.Base+"/video/translate", payload, key, vWait, vTimeout, &created); err != nil {
			return err
		}
		if created == nil {
			created = map[string]any{}
		}
		created["idempotency_key"] = key
		if dry, _ := created["dry_run"].(bool); dry {
			return emit(created, func() { printDryRunHuman(created) })
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
	stopOnInterrupt := onInterrupt(func() {
		sp.Stop()
		fmt.Fprintf(os.Stderr, "\nStopped waiting. The video keeps going on Zebracat; follow it with: zebracat video status %s --wait\n", taskID)
		os.Exit(130)
	})
	out, err := videos.Wait(c, taskID, videos.WaitOptions{Timeout: timeout, UntilDone: untilDone, Clock: clock, Interval: pollInterval})
	stopOnInterrupt()
	sp.Stop() // before anything is printed, so JSON never starts on the spinner's line
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

// translateCode turns --to into the code /video/translate takes: a code or a
// language name from the server's own list (GET /video/translate/languages).
// When the list can't be read the value is sent as given and the server answers.
func translateCode(c *client.Client, to string) (string, error) {
	want := strings.ToLower(strings.TrimSpace(to))
	ctx, cancel := ctxTimeout(30 * time.Second)
	defer cancel()
	var out struct {
		Languages []struct {
			Code string `json:"code"`
			Name string `json:"name"`
		} `json:"languages"`
	}
	if _, err := c.Do(ctx, "GET", videos.Base+"/video/translate/languages", nil, &out); err != nil || len(out.Languages) == 0 {
		return want, nil
	}
	var names []string
	for _, l := range out.Languages {
		if strings.EqualFold(l.Code, want) || strings.EqualFold(l.Name, want) {
			return l.Code, nil
		}
		names = append(names, l.Code+" ("+l.Name+")")
	}
	e := clierr.Usage("--to %q is not a language videos can be translated into", to)
	e.Code = "invalid_choice"
	e.Hint = "One of: " + strings.Join(names, ", ")
	return "", e
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

// downloadTimeout bounds one MP4 download.
var downloadTimeout = 30 * time.Minute

// downloadFile writes the MP4 at u to dest through a temporary file in the
// same folder, renamed only when every byte (Content-Length) arrived. A cut
// connection removes the temporary file and leaves dest untouched.
func downloadFile(u, dest string) (int64, error) {
	ctx, cancel := ctxTimeout(downloadTimeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, "GET", u, nil)
	if err != nil {
		return 0, clierr.API("bad video URL: %v", err)
	}
	req.Header.Set("User-Agent", client.UserAgent())
	resp, err := http.DefaultClient.Do(req) // the file host: no API key
	if err != nil {
		return 0, clierr.Network("download failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		e := clierr.API("download failed (HTTP %d)", resp.StatusCode)
		e.Status = resp.StatusCode
		return 0, e
	}
	tmp, err := os.CreateTemp(filepath.Dir(dest), "."+filepath.Base(dest)+".part-*")
	if err != nil {
		return 0, clierr.Usage("cannot write next to %s: %v", dest, err)
	}
	n, err := io.Copy(tmp, resp.Body)
	cerr := tmp.Close()
	if err == nil && resp.ContentLength >= 0 && n != resp.ContentLength {
		err = fmt.Errorf("got %d of %d bytes", n, resp.ContentLength)
	}
	if err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(tmp.Name())
		return 0, clierr.Network("download interrupted: %v (nothing was saved)", err)
	}
	if err := os.Rename(tmp.Name(), dest); err != nil {
		os.Remove(tmp.Name())
		return 0, clierr.Usage("cannot write %s: %v", dest, err)
	}
	return n, nil
}

// onInterrupt runs fn on Ctrl-C until the returned stop is called.
func onInterrupt(fn func()) (stop func()) {
	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt)
	done := make(chan struct{})
	go func() {
		select {
		case <-ch:
			fn()
		case <-done:
		}
	}()
	return func() { signal.Stop(ch); close(done) }
}

func init() {
	videoListCmd.Flags().IntVar(&vLimit, "limit", 20, "Max rows")
	videoListCmd.Flags().StringVar(&vStatus, "status", "", "Filter by status")

	videoStatusCmd.Flags().BoolVar(&vWait, "wait", false, "Block until plan_ready, needs_input or finished")
	videoStatusCmd.Flags().DurationVar(&vTimeout, "timeout", 30*time.Minute, "Max time to wait with --wait")

	videoDownloadCmd.Flags().StringVarP(&vOut, "output", "o", "", "Save to this file (or folder) instead of printing the URL")
	videoDownloadCmd.Flags().BoolVar(&vForce, "force", false, "Overwrite the file if it exists")

	tf := videoTranslateCmd.Flags()
	tf.StringVar(&vURL, "url", "", "The video: a link or a local file")
	tf.StringVar(&vTo, "to", "", "Target language: a code (es) or a name (spanish)")
	tf.BoolVar(&vLangs, "languages", false, "List the languages a video can be translated into")
	tf.BoolVar(&vYes, "yes", false, "Don't ask; required when not on a terminal")
	tf.BoolVar(&vDryRun, "dry-run", false, "Answer the price; nothing is created or charged")
	tf.StringVar(&vIdemKey, "idempotency-key", "", "Idempotency-Key header (default: a new UUID, printed in the output)")
	tf.StringVar(&vWebhook, "webhook-url", "", "An https URL to call when this video finishes (signed like account webhooks)")
	tf.BoolVar(&vNoCaption, "no-caption", false, "Disable captions")
	tf.BoolVar(&vRender, "render", false, "Render a final MP4")
	tf.BoolVar(&vWait, "wait", false, "Block until done")
	tf.DurationVar(&vTimeout, "timeout", 30*time.Minute, "Max time to wait with --wait")

	videoCmd.AddCommand(videoCreateCmd, videoListCmd, videoGetCmd, videoStatusCmd, videoCancelCmd, videoDownloadCmd, videoTranslateCmd)
	rootCmd.AddCommand(videoCmd)
}

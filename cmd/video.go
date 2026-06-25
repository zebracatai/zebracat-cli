package cmd

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/zebracatai/zebracat-cli/internal/client"
	"github.com/zebracatai/zebracat-cli/internal/clierr"
	"github.com/zebracatai/zebracat-cli/internal/ui"
)

var (
	vFrom        string
	vPrompt      string
	vScript      string
	vURL         string
	vAudioURL    string
	vType        string
	vDuration    int
	vLanguage    string
	vVoice       string
	vStyle       int
	vAspect      string
	vMood        string
	vPromptStyle string
	vRender      bool
	vWait        bool
	vTimeout     time.Duration
	vLimit       int
	vStatus      string
	vTo          string
	vNoCaption   bool
	vOut         string
)

var videoCmd = &cobra.Command{Use: "video", Short: "Create, track and download videos"}

var videoCreateCmd = &cobra.Command{
	Use:   "create",
	Short: "Create a video (agentic by default — the AI picks the best settings)",
	Long: `Create a video from an idea, script, blog URL, or audio file.

Examples:
  zebracat video create --prompt "30s explainer on compound interest" --wait
  zebracat video create --from idea --prompt "top 3 productivity tips" --type ai_video --duration 30
  zebracat video create --from blog --url https://example.com/post --render --wait
  zebracat video create --from script --script "Line one. Line two." --voice 21m00Tcm4TlvDq8ikWAM`,
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		path, payload, err := buildCreate()
		if err != nil {
			return err
		}
		ctx, cancel := ctxTimeout(2 * time.Minute)
		defer cancel()

		var created map[string]any
		if _, err := c.Do(ctx, "POST", path, payload, &created); err != nil {
			return err
		}
		taskID, _ := created["task_id"].(string)
		if !vWait || taskID == "" {
			return emit(created, func() {
				ui.Success("Submitted. task_id=%s", taskID)
			})
		}
		final, err := pollStatus(c, taskID, vTimeout)
		if err != nil {
			return err
		}
		return emit(final, func() { printStatusHuman(final) })
	},
}

func buildCreate() (string, map[string]any, error) {
	text := strings.TrimSpace(vPrompt)
	common := map[string]any{}
	if vLanguage != "" {
		common["language"] = vLanguage
	}
	if vDuration != 0 {
		common["duration"] = vDuration
	}
	if vVoice != "" {
		common["voice_id"] = vVoice
	}
	if vAspect != "" {
		common["aspect_ratio"] = vAspect
	}
	if vMood != "" {
		common["mood"] = vMood
	}
	if vStyle != 0 {
		common["style_id"] = vStyle
	}
	if vPromptStyle != "" {
		common["prompt_style"] = vPromptStyle
	}
	common["should_render"] = vRender

	switch vFrom {
	case "agentic", "":
		if text == "" {
			return "", nil, clierr.Usage("--prompt is required")
		}
		p := map[string]any{"prompt": text, "should_render": vRender}
		for _, k := range []string{"duration", "language", "mood", "aspect_ratio", "voice_id"} {
			if v, ok := common[k]; ok {
				p[k] = v
			}
		}
		if vType != "" {
			p["video_type"] = vType
		}
		return "/api/public/video/agentic", p, nil
	case "idea":
		if text == "" {
			return "", nil, clierr.Usage("--prompt (the idea) is required")
		}
		common["idea"] = text
		return "/api/public/video/idea", common, nil
	case "script":
		s := vScript
		if s == "" {
			s = text
		}
		if s == "" {
			return "", nil, clierr.Usage("--script or --prompt is required")
		}
		common["script"] = s
		return "/api/public/video/script", common, nil
	case "blog":
		u := vURL
		if u == "" {
			u = text
		}
		if u == "" {
			return "", nil, clierr.Usage("--url is required")
		}
		common["url"] = u
		return "/api/public/video/blog", common, nil
	case "audio":
		a := vAudioURL
		if a == "" {
			a = text
		}
		if a == "" {
			return "", nil, clierr.Usage("--audio-url is required")
		}
		common["audio_url"] = a
		return "/api/public/video/audio", common, nil
	default:
		return "", nil, clierr.Usage("unknown --from %q (use idea|script|blog|audio|agentic)", vFrom)
	}
}

var videoListCmd = &cobra.Command{
	Use:   "list",
	Short: "List your videos",
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		path := fmt.Sprintf("/api/public/projects?limit=%d", vLimit)
		if vStatus != "" {
			path += "&status=" + vStatus
		}
		ctx, cancel := ctxTimeout(60 * time.Second)
		defer cancel()
		var out map[string]any
		if _, err := c.Do(ctx, "GET", path, nil, &out); err != nil {
			return err
		}
		return emit(out, func() {
			rows := [][]string{}
			if vids, ok := out["videos"].([]any); ok {
				for _, v := range vids {
					m, _ := v.(map[string]any)
					rows = append(rows, []string{
						fmt.Sprint(m["task_id"]), fmt.Sprint(m["video_type"]),
						fmt.Sprint(m["status"]), fmt.Sprint(m["created_at"]),
					})
				}
			}
			ui.Table([]string{"task_id", "type", "status", "created"}, rows)
		})
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
		out, err := fetchStatus(c, ctx, args[0])
		if err != nil {
			return err
		}
		return emit(out, func() { printStatusHuman(out) })
	},
}

var videoStatusCmd = &cobra.Command{
	Use:   "status <task_id>",
	Short: "Get just the status of a video (optionally --wait for it to finish)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		if vWait {
			out, err := pollStatus(c, args[0], vTimeout)
			if err != nil {
				return err
			}
			return emit(out, func() { printStatusHuman(out) })
		}
		ctx, cancel := ctxTimeout(60 * time.Second)
		defer cancel()
		out, err := fetchStatus(c, ctx, args[0])
		if err != nil {
			return err
		}
		return emit(out, func() { printStatusHuman(out) })
	},
}

var videoCancelCmd = &cobra.Command{
	Use:   "cancel <task_id>",
	Short: "Cancel a queued video",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		ctx, cancel := ctxTimeout(60 * time.Second)
		defer cancel()
		var out map[string]any
		if _, err := c.Do(ctx, "POST", "/api/public/video/"+args[0]+"/cancel", nil, &out); err != nil {
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
		st, err := fetchStatus(c, ctx, args[0])
		if err != nil {
			return err
		}
		urlStr, _ := st["video_url"].(string)
		if urlStr == "" {
			return clierr.API("video is not ready (status: %v)", st["status"])
		}
		if vOut == "" {
			return emit(map[string]any{"task_id": args[0], "video_url": urlStr}, func() {
				fmt.Println(urlStr)
			})
		}
		if err := downloadFile(urlStr, vOut); err != nil {
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
			"target_language": vTo,
			"enable_caption":  !vNoCaption,
			"should_render":   vRender,
		}
		ctx, cancel := ctxTimeout(2 * time.Minute)
		defer cancel()
		var created map[string]any
		if _, err := c.Do(ctx, "POST", "/api/public/video/translate", payload, &created); err != nil {
			return err
		}
		taskID, _ := created["task_id"].(string)
		if !vWait || taskID == "" {
			return emit(created, func() { ui.Success("Submitted. task_id=%s", taskID) })
		}
		final, err := pollStatus(c, taskID, vTimeout)
		if err != nil {
			return err
		}
		return emit(final, func() { printStatusHuman(final) })
	},
}

// --- helpers ---------------------------------------------------------------

var terminalStatuses = map[string]bool{
	"completed": true, "failed": true, "render_failed": true,
	"avatar_render_failed": true, "cancelled": true,
}

func fetchStatus(c *client.Client, ctx context.Context, taskID string) (map[string]any, error) {
	var out map[string]any
	if _, err := c.Do(ctx, "GET", "/api/public/video/status?task_id="+taskID, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// pollStatus blocks until the task reaches a terminal status or timeout elapses.
func pollStatus(c *client.Client, taskID string, timeout time.Duration) (map[string]any, error) {
	deadline := time.Now().Add(timeout)
	sp := ui.StartSpinner("Rendering… (this can take a few minutes)")
	defer sp.Stop()
	for {
		ctx, cancel := ctxTimeout(60 * time.Second)
		out, err := fetchStatus(c, ctx, taskID)
		cancel()
		if err != nil {
			return nil, err
		}
		if st, _ := out["status"].(string); terminalStatuses[st] {
			return out, nil
		}
		if time.Now().After(deadline) {
			return out, clierr.Timeout("timed out after %s; last status: %v (task still running — poll later with `zebracat video status %s`)", timeout, out["status"], taskID)
		}
		time.Sleep(5 * time.Second)
	}
}

func printStatusHuman(m map[string]any) {
	pairs := [][2]string{
		{"task_id", fmt.Sprint(m["task_id"])},
		{"status", fmt.Sprint(m["status"])},
		{"type", fmt.Sprint(m["video_type"])},
	}
	if u, ok := m["video_url"].(string); ok && u != "" {
		pairs = append(pairs, [2]string{"video_url", u})
	}
	if e, ok := m["error"].(string); ok && e != "" {
		pairs = append(pairs, [2]string{"error", e})
	}
	ui.KV(pairs)
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
	f := videoCreateCmd.Flags()
	f.StringVar(&vFrom, "from", "agentic", "Input type: idea|script|blog|audio|agentic")
	f.StringVar(&vPrompt, "prompt", "", "The idea / text input (universal)")
	f.StringVar(&vScript, "script", "", "Full voice-over script (for --from script)")
	f.StringVar(&vURL, "url", "", "Blog/article URL (for --from blog)")
	f.StringVar(&vAudioURL, "audio-url", "", "Audio file URL (for --from audio)")
	f.StringVar(&vType, "type", "", "Video type: ai_video|moving_ai_images|ai_avatar|stock_footage|brainrot")
	f.IntVar(&vDuration, "duration", 0, "Duration in seconds: 15|30|60|120|180")
	f.StringVar(&vLanguage, "language", "", "Voice-over language (e.g. english)")
	f.StringVar(&vVoice, "voice", "", "Voice ID (see `zebracat voice list`)")
	f.IntVar(&vStyle, "style", 0, "Visual style ID (see `zebracat style list`)")
	f.StringVar(&vAspect, "aspect", "", "Aspect ratio: vertical|square|horizontal")
	f.StringVar(&vMood, "mood", "", "Mood (e.g. energetic, inspiring)")
	f.StringVar(&vPromptStyle, "prompt-style", "", "Narration style (see `zebracat video prompt-styles`)")
	f.BoolVar(&vRender, "render", false, "Render a final MP4 (otherwise saved as an editable project)")
	f.BoolVar(&vWait, "wait", false, "Block until the video finishes")
	f.DurationVar(&vTimeout, "timeout", 30*time.Minute, "Max time to wait with --wait")

	videoListCmd.Flags().IntVar(&vLimit, "limit", 20, "Max rows")
	videoListCmd.Flags().StringVar(&vStatus, "status", "", "Filter by status")

	videoStatusCmd.Flags().BoolVar(&vWait, "wait", false, "Block until the video finishes")
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

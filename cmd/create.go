package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/zebracatai/zebracat-cli/internal/attach"
	"github.com/zebracatai/zebracat-cli/internal/brief"
	"github.com/zebracatai/zebracat-cli/internal/clierr"
	"github.com/zebracatai/zebracat-cli/internal/ui"
	"github.com/zebracatai/zebracat-cli/internal/videos"
)

var (
	crImages   []string
	crScript   string
	crYes      bool
	crNoRender bool
	crTimeout  time.Duration
)

var createCmd = &cobra.Command{
	Use:   "create [idea, script or link]",
	Short: "Make a video by talking to it: plan, change, then generate",
	Long: `Make a video the way the studio's chat does.

In a terminal this opens the conversation: Zebracat writes a plan from your idea,
script or link (a TikTok/YouTube/Reel link can be cloned or used as the topic),
shows it with its price, takes changes in plain words, and generates only when
you press Ctrl+G and say y.

Scripts and agents (no terminal, --json or --yes) never get a prompt: create
writes the plan, waits for it and prints it with its quote as JSON. Add --yes to
also generate it and wait for the finished video.

  zebracat create "a 30-second ad for my coffee brand"
  zebracat create https://www.tiktok.com/@someone/video/7300000000000000000
  zebracat create --script script.txt --json
  zebracat create "why cats knock things over" --yes --json   # plan AND generate (spends credits)
  zebracat create "a launch video for this mug" --image mug.png:product --json

In the conversation, paste a picture with Ctrl+V or drag a picture file in.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		text := strings.TrimSpace(strings.Join(args, " "))
		kind := brief.KindIdea
		if crScript != "" {
			s, err := readTextArg("--script", crScript)
			if err != nil {
				return err
			}
			text, kind = s, brief.KindScript
		}
		imgs, err := openImages(crImages)
		if err != nil {
			return err
		}
		launchImages = imgs
		if !crYes && shellAllowed() {
			first := text
			if kind == brief.KindScript {
				first = "/script " + crScript
			}
			return runInteractive(first)
		}
		if text == "" {
			return clierr.Usage("say what the video is about: zebracat create \"<idea>\" (or --script <file>, or a link)")
		}
		return createPlan(text, kind, imgs)
	},
}

// createPlan is the non-interactive create: a plan (and, with --yes, the video).
func createPlan(text string, kind brief.Kind, imgs []attach.Image) error {
	c, err := newClient()
	if err != nil {
		return err
	}
	body := brief.Body(text, kind, !crNoRender)
	if len(imgs) > 0 { // uploaded first, the way the studio's /chat does
		ctx, cancel := ctxTimeout(2 * time.Minute)
		var urls []string
		for _, im := range imgs {
			u, err := attach.Upload(ctx, c, im)
			if err != nil {
				cancel()
				return err
			}
			urls = append(urls, u)
		}
		cancel()
		body["uploads"] = attach.Uploads(urls, imgs)
	}
	key := newIdempotencyKey()
	var out map[string]any
	if err := postIdempotent(c, videos.Base+"/videos", body, key, true, crTimeout, &out); err != nil {
		return err
	}
	id, _ := out["task_id"].(string)
	if id == "" {
		return clierr.API("the server answered without a task_id")
	}
	task, err := waitTask(c, id, crTimeout, false)
	if err != nil {
		return err
	}
	if videos.PhaseOrStatus(task) != "plan_ready" {
		task["idempotency_key"] = key
		return emit(task, func() { printStatusHuman(task) }) // needs_input, failed, …
	}
	plan, _ := getMap(c, videos.Path(id, "plan"))
	quote, err := getMap(c, videos.Path(id, "quote"))
	if err != nil {
		quote, _ = task["quote"].(map[string]any)
	}
	if !crYes {
		res := map[string]any{"task_id": id, "phase": "plan_ready", "plan": plan, "quote": quote,
			"editor_url": videos.EditorURL(task), "idempotency_key": key,
			"next": fmt.Sprintf("zebracat video generate %s --yes --wait", id)}
		return emit(res, func() {
			if plan != nil {
				printPlanHuman(plan)
			}
			if q := videos.QuoteLine(quote); q != "" {
				ui.KV([][2]string{{"price", q}})
			}
			ui.Info("Generate it with: zebracat video generate %s", id)
		})
	}
	genKey := newIdempotencyKey()
	var gen map[string]any
	if err := postIdempotent(c, videos.Path(id, "generate"), map[string]any{"should_render": !crNoRender}, genKey, true, crTimeout, &gen); err != nil {
		return err
	}
	final, err := waitTask(c, id, crTimeout, true)
	if err != nil {
		return err
	}
	final["quote"] = quote
	final["idempotency_key"] = genKey
	return emit(final, func() { printStatusHuman(final) })
}

// launchImages are the --image pictures the conversation starts with.
var launchImages []attach.Image

// openImages checks each --image path[:kind] (PNG, JPEG or WebP, up to 12 MB).
func openImages(specs []string) ([]attach.Image, error) {
	var out []attach.Image
	for _, spec := range specs {
		path, kind := spec, "other"
		if i := strings.LastIndex(spec, ":"); i > 1 { // "photo.png:person" (a Windows "C:\" drive is not a kind)
			k := spec[i+1:]
			for _, known := range attach.Kinds {
				if k == known {
					path, kind = spec[:i], k
				}
			}
		}
		im, err := attach.Open(path)
		if err != nil {
			return nil, clierr.Usage("--image: %v", err)
		}
		im.Kind = kind
		out = append(out, im)
	}
	return out, nil
}

// loginCmd is `zebracat login`: the same as `zebracat auth login`.
var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "Log in (same as `zebracat auth login`)",
	RunE:  func(cmd *cobra.Command, args []string) error { return apiKeyLogin() },
}

func init() {
	f := createCmd.Flags()
	f.StringVar(&crScript, "script", "", "Plan from a script file (\"-\" reads stdin)")
	f.StringArrayVar(&crImages, "image", nil, "Attach a picture: <path>[:person|place|product|style|logo|other] (PNG/JPEG/WebP, ≤12 MB; repeatable)")
	f.BoolVar(&crYes, "yes", false, "Don't ask: also generate the plan (spends credits) and wait for the video")
	f.BoolVar(&crNoRender, "no-render", false, "Keep an editable project; don't export the MP4")
	f.DurationVar(&crTimeout, "timeout", 30*time.Minute, "Max time to wait")
	rootCmd.AddCommand(createCmd, loginCmd)
}

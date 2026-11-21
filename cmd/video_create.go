package cmd

import (
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/zebracatai/zebracat-cli/internal/attach"
	brief_ "github.com/zebracatai/zebracat-cli/internal/brief"
	"github.com/zebracatai/zebracat-cli/internal/client"
	"github.com/zebracatai/zebracat-cli/internal/clierr"
	"github.com/zebracatai/zebracat-cli/internal/ui"
	"github.com/zebracatai/zebracat-cli/internal/videos"
)

// createOpts are the `video create` flags. Tests reset them with co = createOpts{}.
type createOpts struct {
	brief, prompt, file, input, script, url string
	preset, mediaType, audio, aspect        string
	language, quality, voice, background    string
	seconds, style, brand                   int
	uploads                                 []string
	planOnly, noRender, dryRun, wait        bool
	yes, noWait                             bool
	images                                  []string
	webhook                                 string
	idemKey                                 string
	timeout                                 time.Duration

	// deprecated / legacy
	from, typ, footage, mood, promptStyle, audioFile string
	duration                                         int
	render                                           bool
}

var co createOpts

// inputAliases are the friendly --input spellings of the API's input_kind values.
var inputAliases = map[string]string{
	"vo-script":     "voiceover_script",
	"script-scenes": "script_with_scenes",
}

// legacyMediaTypes maps the old --type/--footage values to v2 media types.
// ai_avatar is handled apart: v2 makes it an AI video with the person speaking.
var legacyMediaTypes = map[string]string{
	"ai_animation":     "ai_video",
	"moving_ai_images": "ai_image_video",
	"ai_image":         "ai_image_video",
	"ai_images":        "ai_image_video",
	"stock_footage":    "stock",
}

// createLong is the help of `create` and `video create`: one command, one
// behaviour (G9, owner O9 2026-11-20), registered under both names.
const createLong = `Make a video from an idea, a script or a link: Zebracat writes a plan (the
screenplay: scenes, words, voices, look) with its price, then generates it.

Plan first, generate on your word. Without --yes the command stops at the plan
and prints it with its quote (JSON for scripts); on a terminal it then asks
"Generate for N credits?". --yes plans and generates in one go and waits for
the finished video. --dry-run answers the price and creates nothing.
--no-wait returns as soon as the request is accepted.

In a terminal, with only words (or --script / --image), it opens the
conversation instead: change the plan in plain words, then Generate.

Inputs: --input idea (default) | script | vo-script | script-scenes | url.
--script <file> and --url set it; --file reads the brief from a file. A video
link as the brief (TikTok, YouTube, Reels) is used as the reference video.
--upload takes a public URL or a local file (uploaded first); --image takes a
local picture. Both take :kind (person, place, product, style, logo, other).

Every create sends an Idempotency-Key (a fresh UUID unless you pass
--idempotency-key) and prints it, so re-running with the same key never makes
a second video. --audio-file <url> keeps the old audio-to-video call.`

const createExample = `  zebracat create "30s explainer on compound interest"
  zebracat create "why cats knock things off tables" --json        # plan + quote, spends nothing
  zebracat create "a tea ad" --media-type ai_video --seconds 20 --yes  # plan AND generate
  zebracat create "a launch video for this mug" --image mug.png:product
  zebracat create https://www.tiktok.com/@someone/video/7300000000000000000
  zebracat create --brief "…" --seconds 30 --dry-run                  # the price only
  zebracat plan show <task_id>; zebracat video generate <task_id> --yes --wait`

var (
	createCmd      = newCreateCmd()
	videoCreateCmd = newCreateCmd()
)

// newCreateCmd builds `create [brief]`; the same command is added at the root
// and under `video`, so both print the same help and do the same thing.
func newCreateCmd() *cobra.Command {
	c := &cobra.Command{
		Use:     "create [brief]",
		Short:   "Make a video: writes the plan and its price; --yes also generates it",
		Long:    createLong,
		Example: createExample,
		RunE:    runCreate,
	}
	addCreateFlags(c)
	return c
}

// shellFlags are the flags the conversation can take: with only these, a
// person at a terminal gets the conversation instead of a one-shot plan.
var shellFlags = map[string]bool{"brief": true, "prompt": true, "script": true, "image": true}

func runCreate(cmd *cobra.Command, args []string) error {
	imgs, err := openImages(co.images)
	if err != nil {
		return err
	}
	if !co.yes && !co.dryRun && shellAllowed() && onlyShellFlags(cmd) {
		launchImages = imgs
		first := strings.TrimSpace(strings.Join(args, " "))
		if co.brief != "" || co.prompt != "" {
			first = strings.TrimSpace(co.brief + co.prompt)
		}
		if co.script != "" {
			first = "/script " + co.script
		}
		return runInteractive(first)
	}
	path, payload, err := buildCreate(args)
	if err != nil {
		return err
	}
	if co.wait && !co.yes && !co.planOnly && path == videos.Base+"/videos" {
		note("create stops at the plan now; add --yes to also generate it (spends credits)")
	}
	c, err := newClient()
	if err != nil {
		return err
	}
	if path != videos.Base+"/videos" { // the old audio-to-video call spends at once
		return runAudioCreate(c, path, payload)
	}
	if err := resolveUploads(c, payload, imgs); err != nil {
		return err
	}
	key := co.idemKey
	if key == "" {
		key = newIdempotencyKey()
	}
	wait := !co.noWait
	var created map[string]any
	if err := postIdempotent(c, path, payload, key, wait, co.timeout, &created); err != nil {
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
	if !wait || taskID == "" {
		return emit(created, func() { printCreatedHuman(created, taskID) })
	}
	generating := payload["auto_generate"] == true
	task, err := waitTask(c, taskID, co.timeout, generating)
	if err != nil {
		return err
	}
	task["idempotency_key"] = key
	if generating || videos.PhaseOrStatus(task) != "plan_ready" {
		return emit(task, func() { printStatusHuman(task) }) // finished, needs_input, failed, …
	}
	return planReady(c, taskID, task, key)
}

// planReady prints the plan and its quote; on a terminal it asks once whether
// to generate it (D1/D2), otherwise it names the command that does.
func planReady(c *client.Client, id string, task map[string]any, key string) error {
	plan, _ := getMap(c, videos.Path(id, "plan"))
	quote, err := getMap(c, videos.Path(id, "quote"))
	if err != nil {
		quote, _ = task["quote"].(map[string]any)
	}
	res := map[string]any{"task_id": id, "phase": "plan_ready", "plan": plan, "quote": quote,
		"editor_url": videos.EditorURL(task), "idempotency_key": key,
		"next": fmt.Sprintf("zebracat video generate %s --yes --wait", id)}
	show := func() {
		if plan != nil {
			printPlanHuman(plan)
		}
		if q := videos.QuoteLine(quote); q != "" {
			ui.KV([][2]string{{"price", q}})
		}
	}
	if co.planOnly || !interactive() {
		return emit(res, func() {
			show()
			ui.Info("Generate it with: zebracat video generate %s", id)
		})
	}
	show()
	if !confirm("Generate this video for " + videos.QuoteLine(quote) + "?") {
		ui.Info("Not generated. The plan is kept: zebracat video generate %s", id)
		return nil
	}
	genKey := newIdempotencyKey()
	var gen map[string]any
	if err := postIdempotent(c, videos.Path(id, "generate"), map[string]any{"should_render": !co.noRender}, genKey, true, co.timeout, &gen); err != nil {
		return err
	}
	final, err := waitTask(c, id, co.timeout, true)
	if err != nil {
		return err
	}
	final["quote"] = quote
	final["idempotency_key"] = genKey
	return emit(final, func() { printStatusHuman(final) })
}

// runAudioCreate is the old audio-to-video call: it spends at once, so D2.
func runAudioCreate(c *client.Client, path string, payload map[string]any) error {
	ok, err := confirmSpend(co.yes, "create --audio-file", func() (string, error) {
		return "Make a video from this audio now? It spends credits.", nil
	})
	if err != nil {
		return err
	}
	if !ok {
		return emit(map[string]any{"created": false}, func() { ui.Info("Not created. Nothing was charged.") })
	}
	key := co.idemKey
	if key == "" {
		key = newIdempotencyKey()
	}
	var created map[string]any
	if err := postIdempotent(c, path, payload, key, co.wait, co.timeout, &created); err != nil {
		return err
	}
	if created == nil {
		created = map[string]any{}
	}
	created["idempotency_key"] = key
	taskID, _ := created["task_id"].(string)
	if !co.wait || taskID == "" {
		return emit(created, func() { printCreatedHuman(created, taskID) })
	}
	final, err := waitTask(c, taskID, co.timeout, true)
	if err != nil {
		return err
	}
	return emit(final, func() { printStatusHuman(final) })
}

// onlyShellFlags reports whether every flag the user set is one the
// conversation can take.
func onlyShellFlags(cmd *cobra.Command) bool {
	only := true
	cmd.LocalNonPersistentFlags().Visit(func(f *pflag.Flag) {
		if !shellFlags[f.Name] {
			only = false
		}
	})
	return only
}

// resolveUploads uploads the local files among the uploads (pictures from
// --image, and --upload values that are paths) and puts their URLs in.
func resolveUploads(c *client.Client, body map[string]any, imgs []attach.Image) error {
	ups, _ := body["uploads"].([]map[string]any)
	for _, u := range ups {
		v, _ := u["url"].(string)
		if isWebURL(v) {
			continue
		}
		kind, _ := u["kind"].(string)
		got, err := mediaURL(c, "--upload", v, "picture", kind)
		if err != nil {
			return err
		}
		u["url"] = got
	}
	if len(imgs) > 0 { // uploaded first (POST /uploads), then sent as the plan's uploads
		ctx, cancel := ctxTimeout(uploadTimeout)
		defer cancel()
		var stored []attach.Stored
		for _, im := range imgs {
			st, err := attach.Upload(ctx, c, im)
			if err != nil {
				return err
			}
			stored = append(stored, st)
		}
		for _, u := range attach.Uploads(stored, imgs) {
			ups = append(ups, u)
		}
	}
	if len(ups) > 0 {
		body["uploads"] = ups
	}
	return nil
}

// buildCreate turns the flags into the request: POST /videos, or the old v1
// audio endpoint for --audio-file. Every value is a structured flag; nothing
// is read out of the brief's text.
func buildCreate(args []string) (string, map[string]any, error) {
	brief := strings.TrimSpace(co.brief)
	if brief == "" {
		brief = strings.TrimSpace(co.prompt)
	}
	if brief == "" && len(args) > 0 {
		brief = strings.TrimSpace(strings.Join(args, " "))
	}
	input := co.input

	if co.from != "" {
		switch co.from {
		case "idea", "agentic":
		case "script":
			if input == "" {
				input = "script"
			}
		case "blog":
			if input == "" {
				input = "url"
			}
		case "audio":
			if co.audioFile == "" {
				co.audioFile = brief
			}
		default:
			return "", nil, clierr.Usage("unknown --from %q (use --input idea|script|vo-script|script-scenes|url)", co.from)
		}
	}
	if co.audioFile != "" {
		return buildAudioCreate()
	}

	if co.file != "" {
		s, err := readTextArg("--file", co.file)
		if err != nil {
			return "", nil, err
		}
		brief = s
	}
	if co.script != "" {
		if _, err := os.Stat(co.script); err == nil || co.script == "-" {
			s, err := readTextArg("--script", co.script)
			if err != nil {
				return "", nil, err
			}
			brief = s
		} else {
			note("--script now takes a file (or - for stdin); using the value as the script text")
			brief = strings.TrimSpace(co.script)
		}
		if input == "" {
			input = "script"
		}
	}
	if co.url != "" {
		brief = strings.TrimSpace(co.url)
		if input == "" {
			input = "url"
		}
	}
	if brief == "" {
		return "", nil, clierr.Usage("say what the video is about: pass it as an argument, --brief, --file, --script or --url")
	}
	sourceVideo := ""
	if input == "" {
		// A lone link as the brief: a video link is the reference video, a
		// page is read as the source (the conversation does the same).
		switch k, link := brief_.Read(brief); k {
		case brief_.KindVideo:
			sourceVideo = link
		case brief_.KindPage:
			brief, input = link, "url"
		}
	}
	if input == "" {
		input = "idea"
	}
	if v, ok := inputAliases[input]; ok {
		input = v
	}
	warnUnknown("--input", input, "create.input_kind")
	if input == "url" {
		if err := checkURL("--url", brief); err != nil {
			return "", nil, err
		}
	}

	if err := checkWebhookURL(co.webhook); err != nil {
		return "", nil, err
	}
	body := map[string]any{
		"brief":         brief,
		"input_kind":    input,
		"auto_generate": co.yes && !co.planOnly,
		"should_render": !co.noRender,
	}
	if sourceVideo != "" {
		body["source_video"] = sourceVideo
	}
	if co.webhook != "" {
		body["webhook_url"] = co.webhook
	}
	if co.dryRun {
		body["dry_run"] = true
	}
	if co.preset != "" {
		warnUnknown("--preset", co.preset, "create.preset")
		body["preset"] = co.preset
	}

	opts := map[string]any{}
	media, audio := co.mediaType, co.audio
	for _, legacy := range []struct{ flag, val string }{{"--type", co.typ}, {"--footage", co.footage}} {
		if legacy.val == "" || media != "" {
			continue
		}
		switch {
		case legacy.val == "ai_avatar":
			// v2 has no avatar media type: an avatar video is an AI video whose
			// presenter speaks on camera (owner, 2026-10-12; doors.read_seed).
			media = "ai_video"
			if audio == "" {
				audio = "dialogue"
			}
			note("%s ai_avatar → --media-type ai_video --audio dialogue; add the presenter with --upload <portrait-url>:person", legacy.flag)
		case legacyMediaTypes[legacy.val] != "":
			media = legacyMediaTypes[legacy.val]
			note("%s %s → --media-type %s", legacy.flag, legacy.val, media)
		default:
			media = legacy.val
		}
	}
	for _, o := range []struct{ flag, key, val string }{
		{"--media-type", "media_type", media},
		{"--audio", "audio_mode", audio},
		{"--aspect", "aspect", co.aspect},
		{"--quality", "quality", co.quality},
	} {
		if o.val == "" {
			continue
		}
		warnUnknown(o.flag, o.val, "create.options."+o.key)
		opts[o.key] = o.val
	}
	if co.language != "" {
		opts["language"] = co.language
	}
	seconds := co.seconds
	if seconds == 0 && co.duration != 0 {
		seconds = co.duration
	}
	if seconds < 0 {
		return "", nil, clierr.Usage("--seconds must be positive")
	}
	if seconds > 0 {
		opts["seconds"] = seconds
	}
	if len(opts) > 0 {
		body["options"] = opts
	}

	if co.voice != "" {
		body["voice_id"] = co.voice
	}
	if co.style != 0 {
		body["style_id"] = co.style
	}
	if co.background != "" {
		body["background_video"] = co.background
	}
	if co.brand != 0 {
		body["brand_id"] = co.brand
	}
	if len(co.uploads) > 0 {
		ups := []map[string]any{}
		for _, raw := range co.uploads {
			u, err := parseUpload(raw)
			if err != nil {
				return "", nil, err
			}
			ups = append(ups, u)
		}
		body["uploads"] = ups
	}
	if co.mood != "" || co.promptStyle != "" {
		note("--mood and --prompt-style are ignored now; ask for it in the brief or with `zebracat plan chat`")
	}
	if co.render {
		note("--render is the default now; pass --no-render to keep an editable project only")
	}
	return videos.Base + "/videos", body, nil
}

// buildAudioCreate is the old audio-to-video request (POST /videos has no audio input).
func buildAudioCreate() (string, map[string]any, error) {
	if err := checkURL("--audio-file", co.audioFile); err != nil {
		return "", nil, err
	}
	body := map[string]any{"audio_url": co.audioFile, "should_render": co.render && !co.noRender}
	if co.language != "" {
		body["language"] = co.language
	}
	if s := co.seconds; s != 0 {
		body["duration"] = s
	} else if co.duration != 0 {
		body["duration"] = co.duration
	}
	if co.voice != "" {
		body["voice_id"] = co.voice
	}
	if co.aspect != "" {
		body["aspect_ratio"] = co.aspect
	}
	if co.style != 0 {
		body["style_id"] = co.style
	}
	return videos.Base + "/video/audio", body, nil
}

// parseUpload reads "<url or file>[:kind]". The kind is the part after the
// last ":" when it is one word of letters and underscores, so "https://…",
// "host:8080/x.png" and "C:\\x.png" stay whole and a kind this CLI's schema
// copy doesn't list yet (e.g. logo before a refresh) still reaches the server.
// A local file is uploaded later (resolveUploads).
func parseUpload(raw string) (map[string]any, error) {
	u, kind := strings.TrimSpace(raw), "other"
	if i := strings.LastIndex(u, ":"); i > 0 && isWord(u[i+1:]) {
		u, kind = u[:i], u[i+1:]
		warnUnknown("--upload kind", kind, "create.uploads.kind")
	}
	if err := checkMedia("--upload", u); err != nil {
		return nil, err
	}
	return map[string]any{"url": u, "kind": kind}, nil
}

// isWord: one or more lowercase letters or underscores.
func isWord(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if (r < 'a' || r > 'z') && r != '_' {
			return false
		}
	}
	return true
}

func checkURL(flag, v string) error {
	p, err := url.Parse(v)
	if err != nil || (p.Scheme != "http" && p.Scheme != "https") || p.Host == "" {
		return clierr.Usage("%s needs a public http(s) URL, got %q", flag, v)
	}
	return nil
}

// warnUnknown notes (stderr) a value this CLI's schema copy doesn't list. The
// value is still sent: the server may know it (a newer server than this copy),
// and when it doesn't, its typed 400 names the allowed values (cli-audit #3).
func warnUnknown(flag, got, key string) {
	if !client.CheckEnum(key, got) {
		note("%s %q is not in this CLI's list (%s); sending it, the server decides", flag, got, strings.Join(client.Enum(key), ", "))
	}
}

// enumUsage is the exit-2 error for a value outside the schema's enum.
func enumUsage(flag, got, key string) *clierr.Error {
	e := clierr.Usage("%s %q is not one of: %s", flag, got, strings.Join(client.Enum(key), ", "))
	e.Code = "invalid_choice"
	return e
}

func printCreatedHuman(created map[string]any, taskID string) {
	ui.Success("Submitted. task_id=%s", taskID)
	if p := videos.Phase(created); p != "" {
		ui.Info("phase: %s", phaseLabel(p))
	}
	for _, w := range asList(created["warnings"]) {
		if wm, ok := w.(map[string]any); ok {
			ui.Warn("%v: %v", wm["field"], wm["hint"])
		}
	}
	if s := videos.EditorURL(created); s != "" {
		ui.Link("✎ Track in studio:", s)
	}
	ui.Info("Follow it: zebracat video status %s --wait", taskID)
}

func printDryRunHuman(out map[string]any) {
	ui.Success("Dry run — nothing was created or charged.")
	if q, ok := out["quote"].(map[string]any); ok {
		ui.Info("Quote: %s", videos.QuoteLine(q))
	}
	if valid, ok := out["valid"].(bool); ok && !valid {
		ui.Warn("The request is not valid as sent.")
	}
}

func asList(v any) []any {
	l, _ := v.([]any)
	return l
}

func addCreateFlags(c *cobra.Command) {
	f := c.Flags()
	f.StringVar(&co.brief, "brief", "", "What the video is about (or pass it as the argument)")
	f.StringVar(&co.prompt, "prompt", "", "Alias of --brief")
	f.StringVar(&co.file, "file", "", "Read the brief from a file (- for stdin)")
	f.StringVar(&co.input, "input", "", "What the brief is: idea (default)|script|vo-script|script-scenes|url")
	f.StringVar(&co.script, "script", "", "Read a script from a file (- for stdin); sets --input script")
	f.StringVar(&co.url, "url", "", "Make the video from a web page; sets --input url")
	f.StringVar(&co.preset, "preset", "", "A preset (see zebracat preset list)")
	f.StringVar(&co.mediaType, "media-type", "", "Video type: "+client.EnumHelp("create.options.media_type")+" (see zebracat video types)")
	f.StringVar(&co.audio, "audio", "", "Audio: "+client.EnumHelp("create.options.audio_mode"))
	f.StringVar(&co.aspect, "aspect", "", "Aspect: "+client.EnumHelp("create.options.aspect"))
	f.StringVar(&co.language, "language", "", "Language (e.g. english; see zebracat language list)")
	f.IntVar(&co.seconds, "seconds", 0, "Target length in seconds")
	f.StringVar(&co.quality, "quality", "", "Quality: "+client.EnumHelp("create.options.quality")+" (see zebracat model list)")
	f.StringVar(&co.voice, "voice", "", "Voice ID (see zebracat voice list)")
	f.IntVar(&co.style, "style", 0, "Visual style ID (see zebracat style list)")
	f.StringVar(&co.background, "background", "", "Background clip src for brainrot videos (see zebracat background list)")
	f.StringArrayVar(&co.uploads, "upload", nil, "A picture or clip to use: <url or file>[:"+client.EnumHelp("create.uploads.kind")+"] (repeatable)")
	f.StringArrayVar(&co.images, "image", nil, "A local picture: <path>[:kind] (PNG/JPEG/WebP, ≤12 MB; repeatable)")
	f.IntVar(&co.brand, "brand", 0, "Brand kit ID (see zebracat brand list)")
	f.BoolVar(&co.yes, "yes", false, "Also generate the plan (spends credits) and wait for the video; required to generate off a terminal")
	f.BoolVar(&co.noRender, "no-render", false, "Keep an editable project; don't export the MP4")
	f.BoolVar(&co.dryRun, "dry-run", false, "Validate and quote only: nothing is created or charged")
	f.BoolVar(&co.noWait, "no-wait", false, "Return as soon as the request is accepted (follow with video status --wait)")
	f.StringVar(&co.webhook, "webhook-url", "", "An https URL to call when this video changes phase (signed like account webhooks)")
	f.StringVar(&co.idemKey, "idempotency-key", "", "Idempotency-Key header (default: a new UUID, printed in the output)")
	f.DurationVar(&co.timeout, "timeout", 30*time.Minute, "Max time to wait")

	f.BoolVar(&co.planOnly, "plan-only", false, "Stop at the plan even with --yes (the default without --yes)")
	f.BoolVar(&co.wait, "wait", false, "Kept for old scripts: create always waits now (see --no-wait)")
	_ = f.MarkHidden("wait")
	f.StringVar(&co.audioFile, "audio-file", "", "Make a video from an audio file URL (the old audio-to-video call; spends at once)")
	f.StringVar(&co.audioFile, "audio-url", "", "Alias of --audio-file")
	_ = f.MarkHidden("audio-url")
	f.StringVar(&co.from, "from", "", "Old input selector")
	_ = f.MarkDeprecated("from", "use --input idea|script|vo-script|script-scenes|url (or --script/--url/--audio-file)")
	f.StringVar(&co.typ, "type", "", "Old video type")
	_ = f.MarkDeprecated("type", "use --media-type "+client.EnumHelp("create.options.media_type"))
	f.StringVar(&co.footage, "footage", "", "Old footage type")
	_ = f.MarkDeprecated("footage", "use --media-type "+client.EnumHelp("create.options.media_type"))
	f.IntVar(&co.duration, "duration", 0, "Old length flag")
	_ = f.MarkDeprecated("duration", "use --seconds")
	f.StringVar(&co.mood, "mood", "", "Old mood flag")
	_ = f.MarkDeprecated("mood", "ignored; say it in the brief or use `zebracat plan chat`")
	f.StringVar(&co.promptStyle, "prompt-style", "", "Old narration style flag")
	_ = f.MarkDeprecated("prompt-style", "ignored; say it in the brief or use `zebracat plan chat`")
	f.BoolVar(&co.render, "render", false, "Old render flag (rendering is now the default)")
	_ = f.MarkHidden("render")
}

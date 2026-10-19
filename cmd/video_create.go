package cmd

import (
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/spf13/cobra"

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

var videoCreateCmd = &cobra.Command{
	Use:   "create [brief]",
	Short: "Create a video with Director v2 (one call, or plan first with --plan-only)",
	Long: `Create a video the way /chat on the dashboard does: Director v2 writes a plan
(the screenplay), then generates it.

One call (plan, then generate and render automatically):
  zebracat video create "30s explainer on compound interest" --wait

Plan first, review, then generate:
  id=$(zebracat video create --brief "why cats knock things off tables" --plan-only --wait | jq -r .task_id)
  zebracat plan show "$id"
  zebracat video quote "$id"
  zebracat video generate "$id" --yes --wait

What would it cost? (spends nothing, creates nothing):
  zebracat video create --brief "…" --seconds 30 --dry-run

Inputs: --input idea (default) | script | vo-script | script-scenes | url.
--script <file> and --url set it for you; --file reads the brief from a file.

Every create sends an Idempotency-Key (a fresh UUID unless you pass
--idempotency-key) and prints it, so re-running with the same key never makes
a second video.

--audio-file <url> keeps the v1 audio-to-video endpoint.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		path, payload, err := buildCreate(args)
		if err != nil {
			return err
		}
		c, err := newClient()
		if err != nil {
			return err
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
		if dry, _ := created["dry_run"].(bool); dry {
			return emit(created, func() { printDryRunHuman(created) })
		}
		taskID, _ := created["task_id"].(string)
		if !co.wait || taskID == "" {
			return emit(created, func() { printCreatedHuman(created, taskID) })
		}
		untilDone := path != videos.Base+"/videos" || !co.planOnly
		final, err := waitTask(c, taskID, co.timeout, untilDone)
		if err != nil {
			return err
		}
		final["idempotency_key"] = key
		return emit(final, func() { printStatusHuman(final) })
	},
}

// buildCreate turns the flags into the request: POST /videos (v2), or the v1
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
		return "", nil, clierr.Usage("a brief is required: pass it as an argument, --brief, --file, --script or --url")
	}
	if input == "" {
		input = "idea"
	}
	if v, ok := inputAliases[input]; ok {
		input = v
	}
	if !client.CheckEnum("create.input_kind", input) {
		return "", nil, enumUsage("--input", input, "create.input_kind")
	}
	if input == "url" {
		if err := checkURL("--url", brief); err != nil {
			return "", nil, err
		}
	}

	body := map[string]any{
		"brief":         brief,
		"input_kind":    input,
		"auto_generate": !co.planOnly,
		"should_render": !co.noRender,
	}
	if co.dryRun {
		body["dry_run"] = true
	}
	if co.preset != "" {
		if !client.CheckEnum("create.preset", co.preset) {
			return "", nil, enumUsage("--preset", co.preset, "create.preset")
		}
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
		if !client.CheckEnum("create.options."+o.key, o.val) {
			return "", nil, enumUsage(o.flag, o.val, "create.options."+o.key)
		}
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
		var ups []map[string]any
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
		note("--mood and --prompt-style have no Director v2 meaning and are ignored; ask for it in the brief or with `zebracat plan chat`")
	}
	if co.render {
		note("--render is the default now; pass --no-render to keep an editable project only")
	}
	return videos.Base + "/videos", body, nil
}

// buildAudioCreate is the v1 audio-to-video request (v2 has no audio door).
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

// parseUpload reads "<url>[:kind]". The kind is split off only when the last
// ":"-separated part is one of the API's upload kinds, so "https://…" stays whole.
func parseUpload(raw string) (map[string]any, error) {
	u, kind := strings.TrimSpace(raw), "other"
	if i := strings.LastIndex(u, ":"); i > 0 {
		if k := u[i+1:]; k != "" && len(client.Enum("create.uploads.kind")) > 0 && client.CheckEnum("create.uploads.kind", k) {
			u, kind = u[:i], k
		}
	}
	if err := checkURL("--upload", u); err != nil {
		return nil, err
	}
	return map[string]any{"url": u, "kind": kind}, nil
}

func checkURL(flag, v string) error {
	p, err := url.Parse(v)
	if err != nil || (p.Scheme != "http" && p.Scheme != "https") || p.Host == "" {
		return clierr.Usage("%s needs a public http(s) URL, got %q", flag, v)
	}
	return nil
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

func init() {
	f := videoCreateCmd.Flags()
	f.StringVar(&co.brief, "brief", "", "What the video is about (or pass it as the argument)")
	f.StringVar(&co.prompt, "prompt", "", "Alias of --brief")
	f.StringVar(&co.file, "file", "", "Read the brief from a file (- for stdin)")
	f.StringVar(&co.input, "input", "", "What the brief is: idea (default)|script|vo-script|script-scenes|url")
	f.StringVar(&co.script, "script", "", "Read a script from a file (- for stdin); sets --input script")
	f.StringVar(&co.url, "url", "", "Make the video from a web page; sets --input url")
	f.StringVar(&co.preset, "preset", "", "A preset (see `zebracat presets list`)")
	f.StringVar(&co.mediaType, "media-type", "", "Media type: "+client.EnumHelp("create.options.media_type"))
	f.StringVar(&co.audio, "audio", "", "Audio: "+client.EnumHelp("create.options.audio_mode"))
	f.StringVar(&co.aspect, "aspect", "", "Aspect: "+client.EnumHelp("create.options.aspect"))
	f.StringVar(&co.language, "language", "", "Language (e.g. english; see `zebracat video languages`)")
	f.IntVar(&co.seconds, "seconds", 0, "Target length in seconds")
	f.StringVar(&co.quality, "quality", "", "Quality: "+client.EnumHelp("create.options.quality")+" (see `zebracat models list`)")
	f.StringVar(&co.voice, "voice", "", "Voice ID (see `zebracat voice list`)")
	f.IntVar(&co.style, "style", 0, "Visual style ID (see `zebracat style list`)")
	f.StringVar(&co.background, "background", "", "Background video (a catalogue src) for split-screen presets")
	f.StringArrayVar(&co.uploads, "upload", nil, "A picture to use: <url>[:"+client.EnumHelp("create.uploads.kind")+"] (repeatable)")
	f.IntVar(&co.brand, "brand", 0, "Brand kit ID (see `zebracat brand list`)")
	f.BoolVar(&co.planOnly, "plan-only", false, "Stop at the plan (auto_generate=false); generate later with `video generate`")
	f.BoolVar(&co.noRender, "no-render", false, "Keep an editable project; don't export the MP4")
	f.BoolVar(&co.dryRun, "dry-run", false, "Validate and quote only: nothing is created or charged")
	f.StringVar(&co.idemKey, "idempotency-key", "", "Idempotency-Key header (default: a new UUID, printed in the output)")
	f.BoolVar(&co.wait, "wait", false, "Block until plan_ready/needs_input (--plan-only) or the video is finished")
	f.DurationVar(&co.timeout, "timeout", 30*time.Minute, "Max time to wait with --wait")

	f.StringVar(&co.audioFile, "audio-file", "", "Make a video from an audio file URL (v1 audio-to-video)")
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
	_ = f.MarkDeprecated("mood", "ignored on Director v2; say it in the brief or use `zebracat plan chat`")
	f.StringVar(&co.promptStyle, "prompt-style", "", "Old narration style flag")
	_ = f.MarkDeprecated("prompt-style", "ignored on Director v2; say it in the brief or use `zebracat plan chat`")
	f.BoolVar(&co.render, "render", false, "Old render flag (rendering is now the default)")
	_ = f.MarkHidden("render")
}

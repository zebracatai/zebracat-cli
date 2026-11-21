package cmd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/zebracatai/zebracat-cli/internal/client"
	"github.com/zebracatai/zebracat-cli/internal/clierr"
	"github.com/zebracatai/zebracat-cli/internal/ui"
)

var (
	aLanguage string
	aGender   string
	aSearch   string
	aLimit    int
	aAll      bool
	aFull     bool
	aMode     string
	aMood     string
	aCategory string
	aMinSec   float64
	aMaxSec   float64
	aPage     int

	cloneName  string
	cloneURL   string
	cloneDesc  string
	cloneYes   bool
	renameName string

	avImage  string
	avVideo  string
	avName   string
	avGender string
	avDesc   string

	tplAnswers string
	tplSet     []string
)

const pub = "/api/v1/public"

// getJSON GETs a path and returns the decoded body.
func getJSON(path string) (any, error) {
	c, err := newClient()
	if err != nil {
		return nil, err
	}
	ctx, cancel := ctxTimeout(60 * time.Second)
	defer cancel()
	var out any
	if _, err := c.Do(ctx, "GET", path, nil, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func getAndEmit(path string) error {
	out, err := getJSON(path)
	if err != nil {
		return err
	}
	return emit(out, nil) // themed table on a terminal, JSON when piped
}

// sendJSON sends a JSON body (POST, PATCH, PUT, DELETE) and returns the answer.
func sendJSON(method, path string, body any) (any, error) {
	c, err := newClient()
	if err != nil {
		return nil, err
	}
	ctx, cancel := ctxTimeout(2 * time.Minute)
	defer cancel()
	var out any
	if _, err := c.Do(ctx, method, path, body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// postAndEmit POSTs a JSON body and renders the result.
func postAndEmit(path string, body any) error {
	out, err := sendJSON("POST", path, body)
	if err != nil {
		return err
	}
	return emit(out, nil)
}

// deleteAndEmit DELETEs a path; an empty answer prints {status: deleted, id}.
func deleteAndEmit(path, id string) error {
	out, err := sendJSON("DELETE", path, nil)
	if err != nil {
		return err
	}
	if out == nil {
		out = map[string]any{"status": "deleted", "id": id}
	}
	return emit(out, func() { ui.Success("Deleted %s", id) })
}

// esc escapes one path segment.
func esc(s string) string { return url.PathEscape(s) }

// ---- voice ------------------------------------------------------------------

var voiceCmd = &cobra.Command{Use: "voice", Short: "Voices to pick from, and your cloned voices"}

var voiceListCmd = &cobra.Command{
	Use:   "list",
	Short: "List voices (50 by default; --search, --all)",
	Long: `List the voices a video can use, one row per voice: voice_id, name, gender,
accent, language. The full catalog has thousands of voices, so the list stops
at --limit (50) unless you pass --all; narrow it with --language, --gender
and --search (words in the name, accent or description).`,
	Example: `  zebracat voice list --language english --gender female --search calm
  zebracat voice list --all --json > voices.json`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		q := url.Values{}
		if aLanguage != "" {
			q.Set("language", aLanguage)
		}
		if aGender != "" {
			q.Set("gender", aGender)
		}
		raw, err := getJSON(withQuery(pub+"/voices", q))
		if err != nil {
			return err
		}
		all := flattenVoices(raw)
		matched := filterVoices(all, aSearch)
		shown := matched
		if !aAll && aLimit > 0 && len(shown) > aLimit {
			shown = shown[:aLimit]
		}
		if !aFull {
			shown = compactVoices(shown)
		}
		out := map[string]any{"voices": shown, "count": len(shown), "total": len(matched),
			"truncated": len(shown) < len(matched)}
		if len(shown) < len(matched) {
			out["more"] = "add --limit N, --search words, or --all"
		}
		return emit(out, func() {
			rows := [][]string{}
			for _, v := range shown {
				rows = append(rows, []string{strOr(v["voice_id"]), strOr(v["name"]), strOr(v["gender"]), strOr(v["accent"]), strOr(v["language"])})
			}
			ui.Table([]string{"VOICE_ID", "NAME", "GENDER", "ACCENT", "LANGUAGE"}, rows)
			if len(shown) < len(matched) {
				ui.Info("%d of %d voices: add --search, --limit or --all", len(shown), len(matched))
			}
		})
	},
}

// flattenVoices turns the catalog's {data: {language: {region: [voice…]}}}
// (or any nesting of maps around lists of voices) into one list, each voice
// carrying the language and region it was filed under.
func flattenVoices(raw any) []map[string]any {
	var out []map[string]any
	var walk func(v any, path []string)
	walk = func(v any, path []string) {
		switch t := v.(type) {
		case map[string]any:
			if _, isVoice := t["voice_id"]; isVoice {
				out = append(out, withPath(t, path))
				return
			}
			keys := make([]string, 0, len(t))
			for k := range t {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				walk(t[k], append(append([]string(nil), path...), k))
			}
		case []any:
			for _, x := range t {
				walk(x, path)
			}
		}
	}
	if m, ok := raw.(map[string]any); ok && m["data"] != nil {
		raw = m["data"]
	}
	walk(raw, nil)
	return out
}

func withPath(v map[string]any, path []string) map[string]any {
	out := make(map[string]any, len(v)+2)
	for k, x := range v {
		out[k] = x
	}
	if len(path) > 0 && out["language"] == nil {
		out["language"] = path[0]
	}
	if len(path) > 1 && out["region"] == nil {
		out["region"] = path[1]
	}
	return out
}

// filterVoices keeps the voices whose text fields contain every search word.
func filterVoices(vs []map[string]any, search string) []map[string]any {
	words := strings.Fields(strings.ToLower(search))
	if len(words) == 0 {
		return vs
	}
	var out []map[string]any
	for _, v := range vs {
		var hay strings.Builder
		for _, x := range v {
			if s, ok := x.(string); ok {
				hay.WriteString(strings.ToLower(s))
				hay.WriteByte(' ')
			}
		}
		ok := true
		for _, w := range words {
			if !strings.Contains(hay.String(), w) {
				ok = false
				break
			}
		}
		if ok {
			out = append(out, v)
		}
	}
	return out
}

// compactFields are what a voice row keeps without --full.
var compactFields = []string{"voice_id", "name", "gender", "accent", "age", "language", "region", "description", "preview_url"}

func compactVoices(vs []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(vs))
	for _, v := range vs {
		c := map[string]any{}
		for _, k := range compactFields {
			if x, ok := v[k]; ok && x != nil && x != "" {
				c[k] = x
			}
		}
		out = append(out, c)
	}
	return out
}

var voiceClonesCmd = &cobra.Command{
	Use:     "clones",
	Short:   "List your cloned voices",
	Example: `  zebracat voice clones`,
	Args:    cobra.NoArgs,
	RunE:    func(cmd *cobra.Command, args []string) error { return getAndEmit(pub + "/voice/clone") },
}

var voiceGetCmd = &cobra.Command{
	Use:     "get <voice_id>",
	Short:   "Show one of your cloned voices",
	Example: `  zebracat voice get abc123`,
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		out, err := getJSON(pub + "/voice/clone")
		if err != nil {
			return err
		}
		for _, v := range asList(out) {
			if vm, ok := v.(map[string]any); ok && fmt.Sprint(vm["voice_id"]) == args[0] {
				return emit(vm, nil)
			}
		}
		e := clierr.Usage("no cloned voice %q", args[0])
		e.Code, e.Exit, e.Hint = "not_found", clierr.CodeAPI, "List them with `zebracat voice clones`."
		return e
	},
}

var voiceRenameCmd = &cobra.Command{
	Use:     "rename <voice_id> --name <name>",
	Short:   "Rename one of your cloned voices",
	Example: `  zebracat voice rename abc123 --name "Podcast voice"`,
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if strings.TrimSpace(renameName) == "" {
			return clierr.Usage("--name is required")
		}
		out, err := sendJSON("PATCH", pub+"/voice/clone/"+esc(args[0]), map[string]any{"name": renameName})
		if err != nil {
			return err
		}
		return emit(out, func() { ui.Success("Renamed %s", args[0]) })
	},
}

var voiceDeleteCmd = &cobra.Command{
	Use:     "delete <voice_id>",
	Short:   "Delete one of your cloned voices",
	Example: `  zebracat voice delete abc123`,
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return deleteAndEmit(pub+"/voice/clone/"+esc(args[0]), args[0])
	},
}

var voiceCloneCmd = &cobra.Command{
	Use:   "clone --name <name> --audio <file or URL>",
	Short: "Clone a voice from an audio sample (counts against your plan's voices)",
	Long: `Clone a voice from a short, clean recording of one speaker: a local MP3 or WAV
file, or a public URL. The new voice_id works anywhere a voice does.

A cloned voice counts against your plan's voice slots. On a terminal you are
asked first; scripts and agents pass --yes.`,
	Example: `  zebracat voice clone --name "My voice" --audio sample.mp3 --yes`,
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if strings.TrimSpace(cloneName) == "" || strings.TrimSpace(cloneURL) == "" {
			return clierr.Usage("--name and --audio (a local file or a public URL) are required")
		}
		ok, err := confirmSpend(cloneYes, "voice clone", func() (string, error) {
			return fmt.Sprintf("Clone the voice %q? It uses one of your plan's voice slots.", cloneName), nil
		})
		if err != nil || !ok {
			if err == nil {
				return emit(map[string]any{"cloned": false}, func() { ui.Info("Not cloned.") })
			}
			return err
		}
		desc := cloneDesc
		if desc == "" {
			desc = cloneName
		}
		body, err := multipartWithFile(map[string]string{"name": cloneName, "description": desc}, "src", cloneURL)
		if err != nil {
			return err
		}
		out, err := sendJSONRaw("POST", pub+"/voice/clone", body)
		if err != nil {
			return err
		}
		return emit(out, nil)
	},
}

// multipartWithFile is a multipart body with fields and one file read from a
// local path or downloaded from a URL.
func multipartWithFile(fields map[string]string, fileField, src string) (client.RawBody, error) {
	name, data, err := readFileOrURL(src)
	if err != nil {
		return client.RawBody{}, err
	}
	var b bytes.Buffer
	mw := multipart.NewWriter(&b)
	keys := make([]string, 0, len(fields))
	for k := range fields {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		_ = mw.WriteField(k, fields[k])
	}
	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, fileField, name))
	ct := http.DetectContentType(data)
	if i := strings.Index(ct, ";"); i > 0 {
		ct = ct[:i]
	}
	h.Set("Content-Type", ct)
	part, err := mw.CreatePart(h)
	if err != nil {
		return client.RawBody{}, clierr.API("%v", err)
	}
	_, _ = part.Write(data)
	_ = mw.Close()
	return client.RawBody{ContentType: mw.FormDataContentType(), Data: b.Bytes()}, nil
}

// maxFetch caps a file the CLI downloads to send on (a voice sample, a photo).
const maxFetch = 100 << 20

// readFileOrURL reads a local file, or downloads a public URL.
func readFileOrURL(src string) (string, []byte, error) {
	if !isWebURL(src) {
		p := expandHome(src)
		data, err := os.ReadFile(p)
		if err != nil {
			return "", nil, clierr.Usage("cannot read %s: %v", src, err)
		}
		return filepath.Base(p), data, nil
	}
	hc := &http.Client{Timeout: 5 * time.Minute}
	resp, err := hc.Get(src) //nolint:gosec // the user's own URL
	if err != nil {
		return "", nil, clierr.Network("cannot download %s: %v", src, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", nil, clierr.API("cannot download %s (HTTP %d)", src, resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxFetch+1))
	if err != nil {
		return "", nil, clierr.Network("download of %s broke: %v", src, err)
	}
	if len(data) > maxFetch {
		return "", nil, clierr.Usage("%s is over %d MB", src, maxFetch>>20)
	}
	u, _ := url.Parse(src)
	name := filepath.Base(u.Path)
	if name == "" || name == "/" || name == "." {
		name = "file"
	}
	return name, data, nil
}

// sendJSONRaw sends a prepared body (multipart) and decodes the answer.
func sendJSONRaw(method, path string, body client.RawBody) (any, error) {
	c, err := newClient()
	if err != nil {
		return nil, err
	}
	ctx, cancel := ctxTimeout(uploadTimeout)
	defer cancel()
	var out any
	if _, err := c.Do(ctx, method, path, body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ---- avatar -----------------------------------------------------------------

var avatarCmd = &cobra.Command{Use: "avatar", Short: "Stock avatars, and your own avatar from a photo"}
var avatarListCmd = &cobra.Command{
	Use:     "list",
	Short:   "List avatars",
	Example: `  zebracat avatar list --mode custom`,
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		q := url.Values{}
		if aMode != "" {
			q.Set("mode", aMode)
		}
		return getAndEmit(withQuery(pub+"/avatars", q))
	},
}

var avatarCloneCmd = &cobra.Command{
	Use:   "clone (--image <photo> | --video <clip>)",
	Short: "Make your own avatar from a photo or a video",
	Long: `Make an avatar from a photo (the photo is the avatar) or a short video (its
first clean frame is used). Each takes a local file or a public URL. Read its
state with ` + "`zebracat avatar clone status <avatar_id>`" + `.`,
	Example: `  zebracat avatar clone --image me.jpg --name "Me" --gender female`,
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if (avImage == "") == (avVideo == "") {
			return clierr.Usage("pass exactly one of --image or --video")
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		body := map[string]any{}
		if avImage != "" {
			u, err := mediaURL(c, "--image", avImage, "", "person")
			if err != nil {
				return err
			}
			body["image_url"] = u
		} else {
			u, err := mediaURL(c, "--video", avVideo, "clone", "")
			if err != nil {
				return err
			}
			body["video_url"] = u
		}
		for k, v := range map[string]string{"name": avName, "gender": avGender, "description": avDesc} {
			if v != "" {
				body[k] = v
			}
		}
		return postAndEmit(pub+"/avatars/clone", body)
	},
}

var avatarStatusCmd = &cobra.Command{
	Use:     "status <avatar_id>",
	Short:   "Show whether your avatar is ready",
	Example: `  zebracat avatar clone status 812`,
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return getAndEmit(pub + "/avatars/clone/" + esc(args[0]))
	},
}

// ---- style / music / template ---------------------------------------------------

var styleCmd = &cobra.Command{Use: "style", Short: "Visual styles"}
var styleListCmd = &cobra.Command{
	Use:     "list",
	Short:   "List visual styles (id and name, by category)",
	Example: `  zebracat style list`,
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		out, err := getJSON(pub + "/visual_styles")
		if err != nil {
			return err
		}
		return emit(out, func() { printStyles(out) })
	},
}

var musicCmd = &cobra.Command{Use: "music", Short: "Background music"}
var musicListCmd = &cobra.Command{
	Use:   "list --mood <mood>",
	Short: "List background music for a mood",
	Long: `List background music for a mood. When the mood list is unavailable, the
same mood is searched instead (see music search).`,
	Example: `  zebracat music list --mood calm`,
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if aMood == "" {
			return clierr.Usage("--mood is required (e.g. energetic, happy, calm, epic)")
		}
		out, err := sendJSON("POST", pub+"/music", map[string]any{"mood": aMood})
		var ce *clierr.Error
		if err != nil && errors.As(err, &ce) && ce.Status >= 500 {
			note("the mood list answered HTTP %d; searching %q instead", ce.Status, aMood)
			out, err = sendJSON("POST", pub+"/music/search", musicSearchBody(aMood))
		}
		if err != nil {
			return err
		}
		return emit(out, nil)
	},
}

var musicSearchCmd = &cobra.Command{
	Use:     "search <words>",
	Short:   "Search background music by words",
	Example: `  zebracat music search "lofi piano" --max-seconds 60`,
	Args:    cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return postAndEmit(pub+"/music/search", musicSearchBody(strings.Join(args, " ")))
	},
}

func musicSearchBody(words string) map[string]any {
	body := map[string]any{"search": words, "duration_gte": aMinSec, "duration_lte": aMaxSec}
	if aPage > 1 {
		body["page"] = aPage
	}
	return body
}

var templateCmd = &cobra.Command{Use: "template", Short: "Video templates"}
var templateListCmd = &cobra.Command{
	Use:     "list",
	Short:   "List video templates",
	Example: `  zebracat template list --category ai`,
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		q := url.Values{}
		if aCategory != "" {
			q.Set("category", aCategory)
		}
		return getAndEmit(withQuery(pub+"/templates", q))
	},
}

var templateResolveCmd = &cobra.Command{
	Use:   "resolve <template> [--set key=value]…",
	Short: "Fill a template's blanks; answers the brief that makes its video",
	Long: `Fill a template's blanks and print the brief (with aspect and length) that
makes its video. Pass the answers as --set key=value (repeatable) or as one
JSON object with --answers. An unknown key answers the keys the template
expects. Then make the video with zebracat create "<brief>".`,
	Example: `  zebracat template resolve product-hero --set subject="our espresso machine"`,
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		answers := map[string]any{}
		if strings.TrimSpace(tplAnswers) != "" {
			if err := json.Unmarshal([]byte(tplAnswers), &answers); err != nil {
				return clierr.Usage("--answers must be a JSON object: %v", err)
			}
		}
		for _, kv := range tplSet {
			k, v, ok := strings.Cut(kv, "=")
			if !ok || strings.TrimSpace(k) == "" {
				return clierr.Usage("--set takes key=value, got %q", kv)
			}
			answers[strings.TrimSpace(k)] = v
		}
		return postAndEmit(pub+"/templates/resolve", map[string]any{"template": args[0], "answers": answers})
	},
}

// ---- character ----------------------------------------------------------------

var (
	charName        string
	charDesc        string
	charImage       string
	sugType         string
	sugInput        string
	sugStyle        int
	sugCharacterTxt string
)

var characterCmd = &cobra.Command{Use: "character", Short: "Your saved characters"}
var characterListCmd = &cobra.Command{
	Use:     "list",
	Short:   "List your saved characters",
	Example: `  zebracat character list`,
	Args:    cobra.NoArgs,
	RunE:    func(cmd *cobra.Command, args []string) error { return getAndEmit(pub + "/characters") },
}

var characterGetCmd = &cobra.Command{
	Use:     "get <id>",
	Short:   "Show one saved character",
	Example: `  zebracat character get 42`,
	Args:    cobra.ExactArgs(1),
	RunE:    func(cmd *cobra.Command, args []string) error { return getAndEmit(pub + "/characters/" + esc(args[0])) },
}

var characterCreateCmd = &cobra.Command{
	Use:     "create --name <name> [--image <photo>]",
	Short:   "Save a character (a name, a description, a picture)",
	Example: `  zebracat character create --name "Captain Mia" --description "a pirate cat" --image mia.png`,
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if strings.TrimSpace(charName) == "" {
			return clierr.Usage("--name is required")
		}
		return saveCharacter("POST", pub+"/characters", true)
	},
}

var characterUpdateCmd = &cobra.Command{
	Use:     "update <id> [--name] [--description] [--image]",
	Short:   "Change a saved character",
	Example: `  zebracat character update 42 --description "a pirate cat with an eye patch"`,
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if charName == "" && charDesc == "" && charImage == "" {
			return clierr.Usage("nothing to change: pass --name, --description or --image")
		}
		return saveCharacter("PATCH", pub+"/characters/"+esc(args[0]), false)
	},
}

// saveCharacter sends the character flags: JSON, or multipart with the picture.
func saveCharacter(method, path string, create bool) error {
	fields := map[string]string{}
	if charName != "" || create {
		fields["name"] = charName
	}
	if charDesc != "" {
		fields["description"] = charDesc
	}
	var out any
	var err error
	if charImage != "" {
		var body client.RawBody
		if body, err = multipartWithFile(fields, "image", charImage); err != nil {
			return err
		}
		out, err = sendJSONRaw(method, path, body)
	} else {
		out, err = sendJSON(method, path, fields)
	}
	if err != nil {
		return err
	}
	return emit(out, nil)
}

var characterDeleteCmd = &cobra.Command{
	Use:     "delete <id>",
	Short:   "Delete a saved character",
	Example: `  zebracat character delete 42`,
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return deleteAndEmit(pub+"/characters/"+esc(args[0]), args[0])
	},
}

var characterSuggestCmd = &cobra.Command{
	Use:     "suggest --input <idea or script> --style <id>",
	Short:   "Suggest characters for a video idea",
	Example: `  zebracat character suggest --input "a heist movie with cats" --style 263 --type ai_video`,
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if strings.TrimSpace(sugInput) == "" || sugStyle == 0 {
			return clierr.Usage("--input and --style are required")
		}
		body := map[string]any{"video_type": sugType, "video_input": sugInput, "style_id": sugStyle}
		if sugCharacterTxt != "" {
			body["character_description"] = sugCharacterTxt
		}
		return postAndEmit(pub+"/characters/suggest", body)
	},
}

// ---- brand --------------------------------------------------------------------

var (
	brName    string
	brWebsite string
	brDesc    string
	brLogo    string
	brColors  []string
	brVoice   string
)

var brandCmd = &cobra.Command{Use: "brand", Short: "Your brand kits (logo, colors, voice)"}
var brandListCmd = &cobra.Command{
	Use:     "list",
	Short:   "List your brand kits",
	Example: `  zebracat brand list`,
	Args:    cobra.NoArgs,
	RunE:    func(cmd *cobra.Command, args []string) error { return getAndEmit(pub + "/brands") },
}

var brandGetCmd = &cobra.Command{
	Use:     "get <id>",
	Short:   "Show one brand kit",
	Example: `  zebracat brand get 7`,
	Args:    cobra.ExactArgs(1),
	RunE:    func(cmd *cobra.Command, args []string) error { return getAndEmit(pub + "/brands/" + esc(args[0])) },
}

var brandCreateCmd = &cobra.Command{
	Use:     "create --name <name> [--logo <file or URL>] [--color #hex]…",
	Short:   "Create a brand kit",
	Example: `  zebracat brand create --name "Bean Co" --website https://bean.co --logo logo.png --color "#6b3bf4"`,
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if strings.TrimSpace(brName) == "" {
			return clierr.Usage("--name is required")
		}
		return saveBrand("POST", pub+"/brands")
	},
}

var brandUpdateCmd = &cobra.Command{
	Use:     "update <id> [flags]",
	Short:   "Change a brand kit",
	Example: `  zebracat brand update 7 --color "#111111" --color "#f4c53b"`,
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return saveBrand("PATCH", pub+"/brands/"+esc(args[0]))
	},
}

func saveBrand(method, path string) error {
	body := map[string]any{}
	for k, v := range map[string]string{"name": brName, "website": brWebsite, "description": brDesc, "voice_id": brVoice} {
		if v != "" {
			body[k] = v
		}
	}
	if len(brColors) > 0 {
		body["colors"] = brColors
	}
	if brLogo != "" {
		c, err := newClient()
		if err != nil {
			return err
		}
		u, err := mediaURL(c, "--logo", brLogo, "", "logo")
		if err != nil {
			return err
		}
		body["light_logo_src"] = u
	}
	if len(body) == 0 {
		return clierr.Usage("nothing to change: pass --name, --website, --description, --logo, --color or --voice")
	}
	out, err := sendJSON(method, path, body)
	if err != nil {
		return err
	}
	return emit(out, nil)
}

var brandDeleteCmd = &cobra.Command{
	Use:     "delete <id>",
	Short:   "Delete a brand kit",
	Example: `  zebracat brand delete 7`,
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return deleteAndEmit(pub+"/brands/"+esc(args[0]), args[0])
	},
}

// ---- legacy -------------------------------------------------------------------

var videoPromptStylesCmd = &cobra.Command{
	Use:     "prompt-styles",
	Short:   "List narration styles of the old script command",
	Example: `  zebracat video prompt-styles`,
	Args:    cobra.NoArgs,
	RunE:    func(cmd *cobra.Command, args []string) error { return getAndEmit(pub + "/video/prompt_styles") },
}

func init() {
	vl := voiceListCmd.Flags()
	vl.StringVar(&aLanguage, "language", "", "Only this language (e.g. english)")
	vl.StringVar(&aGender, "gender", "", "Only this gender (male|female)")
	vl.StringVar(&aSearch, "search", "", "Words in the name, accent or description")
	vl.IntVar(&aLimit, "limit", 50, "Max voices (see --all)")
	vl.BoolVar(&aAll, "all", false, "Every matching voice (the full catalog is large)")
	vl.BoolVar(&aFull, "full", false, "Every field of each voice, not only the main ones")
	voiceRenameCmd.Flags().StringVar(&renameName, "name", "", "The new name (required)")
	vc := voiceCloneCmd.Flags()
	vc.StringVar(&cloneName, "name", "", "Name for the cloned voice (required)")
	vc.StringVar(&cloneURL, "audio", "", "The voice sample: a local MP3/WAV file or a public URL (required)")
	vc.StringVar(&cloneDesc, "description", "", "A description (default: the name)")
	vc.BoolVar(&cloneYes, "yes", false, "Don't ask; required when not on a terminal")
	voiceCmd.AddCommand(voiceListCmd, voiceClonesCmd, voiceGetCmd, voiceRenameCmd, voiceDeleteCmd, voiceCloneCmd)

	avatarListCmd.Flags().StringVar(&aMode, "mode", "", "video|custom")
	ac := avatarCloneCmd.Flags()
	ac.StringVar(&avImage, "image", "", "A photo of the person: a local file or a public URL")
	ac.StringVar(&avVideo, "video", "", "A short video of the person: a local file or a public URL")
	ac.StringVar(&avName, "name", "", "The avatar's name")
	ac.StringVar(&avGender, "gender", "", "male|female")
	ac.StringVar(&avDesc, "description", "", "A description")
	avatarCloneCmd.AddCommand(avatarStatusCmd)
	avatarCmd.AddCommand(avatarListCmd, avatarCloneCmd)

	styleCmd.AddCommand(styleListCmd)

	musicListCmd.Flags().StringVar(&aMood, "mood", "", "Mood (e.g. energetic, happy, calm, epic): required")
	for _, c := range []*cobra.Command{musicListCmd, musicSearchCmd} {
		c.Flags().Float64Var(&aMinSec, "min-seconds", 0, "Shortest track, in seconds")
		c.Flags().Float64Var(&aMaxSec, "max-seconds", 600, "Longest track, in seconds")
		c.Flags().IntVar(&aPage, "page", 1, "Page of results")
	}
	musicCmd.AddCommand(musicListCmd, musicSearchCmd)

	templateListCmd.Flags().StringVar(&aCategory, "category", "", "ai|stock|sketch|infinite_zoom")
	templateResolveCmd.Flags().StringVar(&tplAnswers, "answers", "", "The answers as one JSON object")
	templateResolveCmd.Flags().StringArrayVar(&tplSet, "set", nil, "One answer as key=value (repeatable)")
	templateCmd.AddCommand(templateListCmd, templateResolveCmd)

	for _, c := range []*cobra.Command{characterCreateCmd, characterUpdateCmd} {
		c.Flags().StringVar(&charName, "name", "", "Character name")
		c.Flags().StringVar(&charDesc, "description", "", "Character description")
		c.Flags().StringVar(&charImage, "image", "", "A picture of the character: a local file or a public URL")
	}
	characterCreateCmd.Flags().StringVar(&charImage, "image-url", "", "Alias of --image")
	_ = characterCreateCmd.Flags().MarkHidden("image-url")
	sf := characterSuggestCmd.Flags()
	sf.StringVar(&sugInput, "input", "", "The video's idea or script (required)")
	sf.IntVar(&sugStyle, "style", 0, "Visual style id (required; see zebracat style list)")
	sf.StringVar(&sugType, "type", "ai_video", "The video type")
	sf.StringVar(&sugCharacterTxt, "description", "", "What the characters should be like")
	characterCmd.AddCommand(characterListCmd, characterGetCmd, characterCreateCmd, characterUpdateCmd, characterDeleteCmd, characterSuggestCmd)

	for _, c := range []*cobra.Command{brandCreateCmd, brandUpdateCmd} {
		f := c.Flags()
		f.StringVar(&brName, "name", "", "Brand name")
		f.StringVar(&brWebsite, "website", "", "Website")
		f.StringVar(&brDesc, "description", "", "What the brand is")
		f.StringVar(&brLogo, "logo", "", "Logo: a local file or a public URL")
		f.StringArrayVar(&brColors, "color", nil, "A brand color, e.g. #6b3bf4 (repeatable)")
		f.StringVar(&brVoice, "voice", "", "Default voice id")
	}
	brandCmd.AddCommand(brandListCmd, brandGetCmd, brandCreateCmd, brandUpdateCmd, brandDeleteCmd)

	videoCmd.AddCommand(videoPromptStylesCmd)
	rootCmd.AddCommand(voiceCmd, avatarCmd, styleCmd, musicCmd, templateCmd, characterCmd, brandCmd)
}

package cmd

import (
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/zebracatai/zebracat-cli/internal/attach"
	"github.com/zebracatai/zebracat-cli/internal/client"
	"github.com/zebracatai/zebracat-cli/internal/clierr"
	"github.com/zebracatai/zebracat-cli/internal/ui"
)

// uploadTimeout bounds one file upload (a video on a slow link takes a while).
var uploadTimeout = 30 * time.Minute

var (
	upUse   string
	upKind  string
	upName  string
	upLimit int
)

var uploadCmd = &cobra.Command{
	Use:   "upload <file>",
	Short: "Upload a picture, video or audio file; answers its URL",
	Long: `Upload one file from your computer to Zebracat storage and print the URL to
use in other commands: a picture (PNG, JPEG, WebP up to 12 MB), a video (MP4,
MOV) or an audio file (MP3, WAV). --use says what the file is for, which sets
the types and limits the server takes: picture (12 MB), translate (MP4/MOV,
100 MB, 10 minutes), clone and swap (MP4/MOV, 100 MB), voice_clone (MP3/WAV,
10 MB). By default a picture is a picture, a video is for clone, audio is for
voice_clone; the server says when a file is over the limits.

Commands that take a URL also take a local path and upload it for you:
video create --upload, video clone --url, video translate --url, swap --video
and --character, voice clone --audio, avatar clone --image.`,
	Example: `  zebracat upload mug.png --kind product
  zebracat upload clip.mp4
  zebracat upload talk.mp4 --use translate
  url=$(zebracat upload voice.mp3 --json | jq -r .url)`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		m, err := attach.OpenMedia(args[0])
		if err != nil {
			return clierr.Usage("%v", err)
		}
		if err := attach.CheckUse(m, upUse); err != nil {
			return clierr.Usage("--use: %v", err)
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		ctx, cancel := ctxTimeout(uploadTimeout)
		defer cancel()
		st, err := attach.UploadMedia(ctx, c, m, upUse, upKind, upName)
		if err != nil {
			return err
		}
		out := map[string]any{"url": st.URL, "kind_hint": st.KindHint, "content_type": m.ContentType, "size": m.Bytes, "file": args[0]}
		return emit(out, func() {
			ui.Success("Uploaded %s (%s)", args[0], attach.Size(m.Bytes))
			ui.Link("URL:", st.URL)
		})
	},
}

var uploadListCmd = &cobra.Command{
	Use:     "list",
	Short:   "List your uploads",
	Example: `  zebracat upload list --limit 10`,
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		q := url.Values{}
		if upLimit > 0 {
			q.Set("limit", strconv.Itoa(upLimit))
		}
		if upKind != "" {
			q.Set("kind", upKind)
		}
		return getAndEmit(withQuery("/api/v1/public/uploads", q))
	},
}

// mediaURL is a flag value as a URL: an http(s) URL as is, a local file
// uploaded first (pictures, videos, audio) for use (see attach.Uses; "" =
// by the file's type). flag names the flag in errors.
func mediaURL(c *client.Client, flag, value, use, kind string) (string, error) {
	v := strings.TrimSpace(value)
	if v == "" {
		return "", nil
	}
	if isWebURL(v) {
		return v, nil
	}
	path := expandHome(v)
	if _, err := os.Stat(path); err != nil {
		return "", clierr.Usage("%s needs a public http(s) URL or a local file, got %q", flag, value)
	}
	m, err := attach.OpenMedia(path)
	if err == nil {
		err = attach.CheckUse(m, use)
	}
	if err != nil {
		return "", clierr.Usage("%s: %v", flag, err)
	}
	ctx, cancel := ctxTimeout(uploadTimeout)
	defer cancel()
	st, err := attach.UploadMedia(ctx, c, m, use, kind, "")
	if err != nil {
		return "", err
	}
	note("uploaded %s → %s", v, st.URL)
	return st.URL, nil
}

// isWebURL reports whether v is an absolute http(s) URL with a host.
func isWebURL(v string) bool {
	p, err := url.Parse(v)
	return err == nil && (p.Scheme == "http" || p.Scheme == "https") && p.Host != ""
}

// isLocalFile reports whether v names an existing local file (not a URL).
func isLocalFile(v string) bool {
	if isWebURL(v) {
		return false
	}
	st, err := os.Stat(expandHome(v))
	return err == nil && !st.IsDir()
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		if h, err := os.UserHomeDir(); err == nil {
			return h + p[1:]
		}
	}
	return p
}

// withQuery appends q to path when it has values.
func withQuery(path string, q url.Values) string {
	if len(q) == 0 {
		return path
	}
	return path + "?" + q.Encode()
}

func init() {
	uploadCmd.Flags().StringVar(&upUse, "use", "", "What the file is for: "+strings.Join(attach.Uses, ", ")+" (default: by the file type)")
	uploadCmd.Flags().StringVar(&upKind, "kind", "", "What the file shows, for a picture: "+client.EnumHelp("create.uploads.kind"))
	uploadCmd.Flags().StringVar(&upName, "name", "", "A name for the file in your library")
	uploadListCmd.Flags().IntVar(&upLimit, "limit", 50, "Max rows")
	uploadListCmd.Flags().StringVar(&upKind, "kind", "", "Only this kind")
	uploadCmd.AddCommand(uploadListCmd)
	rootCmd.AddCommand(uploadCmd)
}

// checkMedia is the usage check for a flag that takes a URL or a local file.
func checkMedia(flag, v string) error {
	if isWebURL(v) || isLocalFile(v) {
		return nil
	}
	return clierr.Usage("%s needs a public http(s) URL or a local file, got %q", flag, v)
}

// checkWebhookURL: --webhook-url is empty or an https URL (the server also
// refuses private hosts).
func checkWebhookURL(v string) error {
	if v == "" {
		return nil
	}
	p, err := url.Parse(v)
	if err != nil || p.Scheme != "https" || p.Host == "" {
		e := clierr.Usage("--webhook-url must be an https URL, got %q", v)
		e.Code = "invalid_webhook_url"
		return e
	}
	return nil
}

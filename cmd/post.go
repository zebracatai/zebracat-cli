package cmd

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/zebracatai/zebracat-cli/internal/clierr"
	"github.com/zebracatai/zebracat-cli/internal/ui"
	"github.com/zebracatai/zebracat-cli/internal/videos"
)

const schedBase = pub + "/scheduling"

var (
	postPlatforms []string
	postCaption   string
	postTitle     string
	postDesc      string
	postHashtags  []string
	postVideoURL  string
	postTask      string
	postProject   int
	postAt        string
	postYes       bool
	postStatus    string
	postRedirect  string
)

var postCmd = &cobra.Command{
	Use:   "post",
	Short: "Post or schedule videos to your social accounts",
	Long: `Post a finished video to your connected social accounts, now or at a time.
Connect an account once with ` + "`zebracat post connect <platform>`" + ` (it answers a
link to open in your browser).`,
}

var postConnectedCmd = &cobra.Command{
	Use:     "connected",
	Short:   "List your connected social accounts",
	Example: `  zebracat post connected`,
	Args:    cobra.NoArgs,
	RunE:    func(cmd *cobra.Command, args []string) error { return getAndEmit(schedBase + "/connected") },
}

var postConnectCmd = &cobra.Command{
	Use:     "connect <platform>",
	Short:   "Connect a social account: answers the link to open in your browser",
	Example: `  zebracat post connect tiktok`,
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		body := map[string]any{"platform": args[0]}
		if postRedirect != "" {
			body["redirect_uri"] = postRedirect
		}
		out, err := sendJSON("POST", schedBase+"/connect", body)
		if err != nil {
			return err
		}
		return emit(out, func() {
			m, _ := out.(map[string]any)
			if u, _ := m["authorization_url"].(string); u != "" {
				ui.Info("Open this link to connect %s:", args[0])
				ui.Link("→", u)
				return
			}
			ui.Auto(out)
		})
	},
}

var postDisconnectCmd = &cobra.Command{
	Use:     "disconnect <platform>",
	Short:   "Disconnect a social account",
	Example: `  zebracat post disconnect tiktok`,
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return postAndEmit(schedBase+"/disconnect", map[string]any{"platform": args[0]})
	},
}

var postScheduleCmd = &cobra.Command{
	Use:   "schedule --platform <p> --caption <text> (--task <task_id> | --video-url <url>) [--at <time>]",
	Short: "Post a video now or at a time (posts publicly: needs --yes)",
	Long: `Post a finished video to one or more connected accounts, now or at --at
(RFC 3339, e.g. 2026-12-01T09:00:00Z). --task takes the video from one of your
finished videos; --video-url takes any public MP4.

Posting is public: on a terminal you are asked first; scripts and agents pass --yes.`,
	Example: `  zebracat post schedule --platform tiktok --platform youtube --task 6f1c0d2e --caption "New drop" --yes
  zebracat post schedule --platform instagram --video-url https://…/v.mp4 --caption "Hi" --at 2026-12-01T09:00:00Z --yes`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(postPlatforms) == 0 || strings.TrimSpace(postCaption) == "" {
			return clierr.Usage("--platform (one or more) and --caption are required")
		}
		if (postTask == "") == (postVideoURL == "") {
			return clierr.Usage("pass exactly one of --task or --video-url")
		}
		var at time.Time
		if postAt != "" {
			t, err := time.Parse(time.RFC3339, postAt)
			if err != nil {
				return clierr.Usage("--at takes an RFC 3339 time like 2026-12-01T09:00:00Z, got %q", postAt)
			}
			at = t
		}
		when := "now"
		if !at.IsZero() {
			when = "at " + at.UTC().Format(time.RFC3339)
		}
		ok, err := confirmSpend(postYes, "post schedule", func() (string, error) {
			return fmt.Sprintf("Post this video publicly to %s %s?", strings.Join(postPlatforms, ", "), when), nil
		})
		if err != nil {
			return err
		}
		if !ok {
			return emit(map[string]any{"posted": false}, func() { ui.Info("Not posted.") })
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		body := map[string]any{"platforms": postPlatforms, "caption": postCaption}
		if postTask != "" {
			ctx, cancel := ctxTimeout(60 * time.Second)
			st, err := videos.Fetch(ctx, c, postTask)
			cancel()
			if err != nil {
				return err
			}
			mp4 := renderedMP4(st)
			if mp4 == "" {
				e := clierr.API("video %s has no finished MP4 yet (%s)", postTask, videos.PhaseOrStatus(st))
				e.Code = "not_ready"
				return e
			}
			body["video_url"] = mp4
			if pid, ok := st["project_id"].(float64); ok {
				body["project_id"] = int(pid)
			}
		} else {
			if err := checkURL("--video-url", postVideoURL); err != nil {
				return err
			}
			body["video_url"] = postVideoURL
		}
		if postProject != 0 {
			body["project_id"] = postProject
		}
		for k, v := range map[string]string{"title": postTitle, "description": postDesc} {
			if v != "" {
				body[k] = v
			}
		}
		if len(postHashtags) > 0 {
			body["hashtags"] = postHashtags
		}
		if !at.IsZero() {
			body["schedule_time"] = at.UTC().Format(time.RFC3339)
		}
		return postAndEmit(schedBase+"/posts", body)
	},
}

var postListCmd = &cobra.Command{
	Use:     "list",
	Short:   "List your posts",
	Example: `  zebracat post list --status scheduled`,
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		q := url.Values{}
		if len(postPlatforms) > 0 {
			q.Set("platform", postPlatforms[0])
		}
		if postStatus != "" {
			q.Set("status", postStatus)
		}
		if postProject != 0 {
			q.Set("project_id", strconv.Itoa(postProject))
		}
		return getAndEmit(withQuery(schedBase+"/posts/list", q))
	},
}

var postGetCmd = &cobra.Command{
	Use:     "get <post_id>",
	Short:   "Show one post and how it went on each platform",
	Example: `  zebracat post get 311`,
	Args:    cobra.ExactArgs(1),
	RunE:    func(cmd *cobra.Command, args []string) error { return getAndEmit(schedBase + "/posts/" + esc(args[0])) },
}

var postCancelCmd = &cobra.Command{
	Use:     "cancel <post_id>",
	Short:   "Cancel a scheduled post",
	Example: `  zebracat post cancel 311`,
	Args:    cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return deleteAndEmit(schedBase+"/posts/"+esc(args[0]), args[0])
	},
}

func init() {
	postConnectCmd.Flags().StringVar(&postRedirect, "redirect-uri", "", "Where the browser returns after connecting")
	sf := postScheduleCmd.Flags()
	sf.StringArrayVar(&postPlatforms, "platform", nil, "A connected platform, e.g. tiktok, youtube, instagram (repeatable)")
	sf.StringVar(&postCaption, "caption", "", "The post's caption (required)")
	sf.StringVar(&postTitle, "title", "", "Title (YouTube)")
	sf.StringVar(&postDesc, "description", "", "Description")
	sf.StringArrayVar(&postHashtags, "hashtag", nil, "A hashtag (repeatable)")
	sf.StringVar(&postTask, "task", "", "Post this finished video (a task_id)")
	sf.StringVar(&postVideoURL, "video-url", "", "Post this public MP4")
	sf.IntVar(&postProject, "project", 0, "Link the post to this project id")
	sf.StringVar(&postAt, "at", "", "When to post (RFC 3339); default now")
	sf.BoolVar(&postYes, "yes", false, "Don't ask; required when not on a terminal")
	lf := postListCmd.Flags()
	lf.StringArrayVar(&postPlatforms, "platform", nil, "Only this platform")
	lf.StringVar(&postStatus, "status", "", "Only this status")
	lf.IntVar(&postProject, "project", 0, "Only posts of this project id")
	postCmd.AddCommand(postConnectedCmd, postConnectCmd, postDisconnectCmd, postScheduleCmd, postListCmd, postGetCmd, postCancelCmd)
	rootCmd.AddCommand(postCmd)
}

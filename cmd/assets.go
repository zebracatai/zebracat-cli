package cmd

import (
	"net/url"
	"time"

	"github.com/spf13/cobra"

	"github.com/zebracatai/zebracat-cli/internal/clierr"
)

var (
	aLanguage string
	aGender   string
	aMode     string
	aMood     string
	aCategory string
	cloneName string
	cloneURL  string
)

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
	return emit(out, nil) // lists render as JSON; pipe-friendly by default
}

// ---- voice ----
var voiceCmd = &cobra.Command{Use: "voice", Short: "List and clone voices"}

var voiceListCmd = &cobra.Command{
	Use:   "list",
	Short: "List available voices",
	RunE: func(cmd *cobra.Command, args []string) error {
		q := url.Values{}
		if aLanguage != "" {
			q.Set("language", aLanguage)
		}
		if aGender != "" {
			q.Set("gender", aGender)
		}
		p := "/api/v1/public/voices"
		if len(q) > 0 {
			p += "?" + q.Encode()
		}
		return getAndEmit(p)
	},
}

var voiceCloneCmd = &cobra.Command{
	Use:   "clone",
	Short: "Clone a voice from an audio sample URL",
	RunE: func(cmd *cobra.Command, args []string) error {
		if cloneName == "" || cloneURL == "" {
			return clierr.Usage("--name and --audio (a public URL) are required")
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		ctx, cancel := ctxTimeout(2 * time.Minute)
		defer cancel()
		var out any
		if _, err := c.Do(ctx, "POST", "/api/v1/public/voice/clone", map[string]any{"name": cloneName, "audio_url": cloneURL}, &out); err != nil {
			return err
		}
		return emit(out, nil)
	},
}

// ---- avatar / style / music / template / character ----
var avatarCmd = &cobra.Command{Use: "avatar", Short: "List avatars"}
var avatarListCmd = &cobra.Command{
	Use:   "list",
	Short: "List avatars",
	RunE: func(cmd *cobra.Command, args []string) error {
		p := "/api/v1/public/avatars"
		if aMode != "" {
			p += "?mode=" + url.QueryEscape(aMode)
		}
		return getAndEmit(p)
	},
}

var styleCmd = &cobra.Command{Use: "style", Short: "List visual styles"}
var styleListCmd = &cobra.Command{
	Use:   "list",
	Short: "List visual styles",
	RunE:  func(cmd *cobra.Command, args []string) error { return getAndEmit("/api/v1/public/visual_styles") },
}

var musicCmd = &cobra.Command{Use: "music", Short: "List background music"}
var musicListCmd = &cobra.Command{
	Use:   "list",
	Short: "List background music (by mood)",
	RunE: func(cmd *cobra.Command, args []string) error {
		p := "/api/v1/public/music"
		if aMood != "" {
			p += "?mood=" + url.QueryEscape(aMood)
		}
		return getAndEmit(p)
	},
}

var templateCmd = &cobra.Command{Use: "template", Short: "List preset templates"}
var templateListCmd = &cobra.Command{
	Use:   "list",
	Short: "List preset templates",
	RunE: func(cmd *cobra.Command, args []string) error {
		p := "/api/v1/public/templates"
		if aCategory != "" {
			p += "?category=" + url.QueryEscape(aCategory)
		}
		return getAndEmit(p)
	},
}

var characterCmd = &cobra.Command{Use: "character", Short: "List saved characters"}
var characterListCmd = &cobra.Command{
	Use:   "list",
	Short: "List saved characters",
	RunE:  func(cmd *cobra.Command, args []string) error { return getAndEmit("/api/v1/public/characters") },
}

var videoPromptStylesCmd = &cobra.Command{
	Use:   "prompt-styles",
	Short: "List narration / script styles",
	RunE:  func(cmd *cobra.Command, args []string) error { return getAndEmit("/api/v1/public/video/prompt_styles") },
}

var langCmd = &cobra.Command{
	Use:   "languages",
	Short: "List supported voice-over languages",
	RunE:  func(cmd *cobra.Command, args []string) error { return getAndEmit("/api/v1/public/video/languages") },
}

func init() {
	voiceListCmd.Flags().StringVar(&aLanguage, "language", "", "Filter by language")
	voiceListCmd.Flags().StringVar(&aGender, "gender", "", "Filter by gender (male|female)")
	voiceCloneCmd.Flags().StringVar(&cloneName, "name", "", "Name for the cloned voice")
	voiceCloneCmd.Flags().StringVar(&cloneURL, "audio", "", "Public URL of the audio sample")
	voiceCmd.AddCommand(voiceListCmd, voiceCloneCmd)

	avatarListCmd.Flags().StringVar(&aMode, "mode", "", "video|custom")
	avatarCmd.AddCommand(avatarListCmd)

	styleCmd.AddCommand(styleListCmd)

	musicListCmd.Flags().StringVar(&aMood, "mood", "", "Filter by mood")
	musicCmd.AddCommand(musicListCmd)

	templateListCmd.Flags().StringVar(&aCategory, "category", "", "ai|stock|sketch|infinite_zoom")
	templateCmd.AddCommand(templateListCmd)

	characterCmd.AddCommand(characterListCmd)

	videoCmd.AddCommand(videoPromptStylesCmd, langCmd)
	rootCmd.AddCommand(voiceCmd, avatarCmd, styleCmd, musicCmd, templateCmd, characterCmd)
}

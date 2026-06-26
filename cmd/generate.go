package cmd

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/zebracatai/zebracat-cli/internal/ui"
)

var (
	sgDuration    int
	sgMood        string
	sgPromptStyle string
	sgLanguage    string

	imgStyle  int
	imgWidth  int
	imgHeight int
	imgCount  int
)

var scriptCmd = &cobra.Command{
	Use:   "script <idea>",
	Short: "Generate a voice-over script from an idea",
	Long: `Generate a ready-to-use voice-over script from a one-line idea — no video.

  zebracat script "benefits of morning walks" --duration 30 --prompt-style storytime`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		body := map[string]any{"idea": strings.Join(args, " ")}
		if sgDuration != 0 {
			body["duration"] = sgDuration
		}
		if sgMood != "" {
			body["mood"] = sgMood
		}
		if sgPromptStyle != "" {
			body["prompt_style"] = sgPromptStyle
		}
		if sgLanguage != "" {
			body["language"] = sgLanguage
		}
		ctx, cancel := ctxTimeout(90 * time.Second)
		defer cancel()
		var out map[string]any
		if _, err := c.Do(ctx, "POST", "/api/v1/public/script_generator", body, &out); err != nil {
			return err
		}
		return emit(out, func() {
			ui.Heading("Script")
			if s, ok := out["script"].(string); ok {
				fmt.Println(s)
			}
		})
	},
}

var imageCmd = &cobra.Command{
	Use:   "image <prompt>",
	Short: "Generate an AI image",
	Long: `Generate one or more AI images from a prompt.

  zebracat image "a neon city skyline at dusk" --n 2`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		body := map[string]any{"prompt": strings.Join(args, " ")}
		if imgStyle != 0 {
			body["style_id"] = imgStyle
		}
		if imgWidth != 0 {
			body["width"] = imgWidth
		}
		if imgHeight != 0 {
			body["height"] = imgHeight
		}
		if imgCount != 0 {
			body["images"] = imgCount
		}
		ctx, cancel := ctxTimeout(3 * time.Minute)
		defer cancel()
		var out any
		if _, err := c.Do(ctx, "POST", "/api/v1/public/generate_image", body, &out); err != nil {
			return err
		}
		return emit(out, nil)
	},
}

func init() {
	sf := scriptCmd.Flags()
	sf.IntVar(&sgDuration, "duration", 0, "Target length in seconds: 15|30|60|120|180")
	sf.StringVar(&sgMood, "mood", "", "Mood (e.g. energetic, inspiring)")
	sf.StringVar(&sgPromptStyle, "prompt-style", "", "Narration style (see `zebracat video prompt-styles`)")
	sf.StringVar(&sgLanguage, "language", "", "Language")

	gf := imageCmd.Flags()
	gf.IntVar(&imgStyle, "style", 0, "Visual style ID (see `zebracat style list`)")
	gf.IntVar(&imgWidth, "width", 0, "Width in px")
	gf.IntVar(&imgHeight, "height", 0, "Height in px")
	gf.IntVar(&imgCount, "n", 0, "Number of images")

	rootCmd.AddCommand(scriptCmd, imageCmd)
}

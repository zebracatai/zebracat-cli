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

	legacyYes bool
)

var scriptCmd = &cobra.Command{
	Use:   "script <idea>",
	Short: "Write a voice-over script from an idea (old call; spends API balance)",
	Long: `Write a ready-to-use voice-over script from a one-line idea, no video. This is
an old call kept working; for a video, zebracat create writes the script as
part of its plan.

It spends API balance: on a terminal you are asked first; scripts and agents
pass --yes.`,
	Example: `  zebracat script "benefits of morning walks" --duration 30 --yes`,
	Args:    cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if ok, err := legacySpend("script"); !ok {
			return err
		}
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
	Short: "Make AI images from a prompt (old call; spends API balance)",
	Long: `Make one or more AI images from a prompt. This is an old call kept working.

It spends API balance: on a terminal you are asked first; scripts and agents
pass --yes.`,
	Example: `  zebracat image "a neon city skyline at dusk" --n 2 --yes`,
	Args:    cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if ok, err := legacySpend("image"); !ok {
			return err
		}
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

// legacySpend is D2 for script and image: --yes, or a y on a terminal.
func legacySpend(action string) (bool, error) {
	ok, err := confirmSpend(legacyYes, action, func() (string, error) {
		return "This spends API balance (see zebracat account pricing). Go ahead?", nil
	})
	if err == nil && !ok {
		_ = emit(map[string]any{"done": false}, func() { ui.Info("Nothing was made or charged.") })
	}
	return ok, err
}

func init() {
	sf := scriptCmd.Flags()
	sf.IntVar(&sgDuration, "duration", 0, "Target length in seconds: 15|30|60|120|180")
	sf.StringVar(&sgMood, "mood", "", "Mood (e.g. energetic, inspiring)")
	sf.StringVar(&sgPromptStyle, "prompt-style", "", "Narration style (see `zebracat video prompt-styles`)")
	sf.BoolVar(&legacyYes, "yes", false, "Don't ask; required when not on a terminal")
	sf.StringVar(&sgLanguage, "language", "", "Language")

	gf := imageCmd.Flags()
	gf.IntVar(&imgStyle, "style", 0, "Visual style ID (see `zebracat style list`)")
	gf.IntVar(&imgWidth, "width", 0, "Width in px")
	gf.IntVar(&imgHeight, "height", 0, "Height in px")
	gf.IntVar(&imgCount, "n", 0, "Number of images")
	gf.BoolVar(&legacyYes, "yes", false, "Don't ask; required when not on a terminal")

	rootCmd.AddCommand(scriptCmd, imageCmd)
}

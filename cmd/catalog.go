package cmd

import (
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/spf13/cobra"

	"github.com/zebracatai/zebracat-cli/internal/ui"
	"github.com/zebracatai/zebracat-cli/internal/videos"
)

// ---- catalogs: public pick-lists, read-only ---------------------------------
//
// Nouns are singular (`model list`, `preset list`, `language list`); the old
// plural spellings stay as aliases so scripts keep working (G8).

var (
	catTranslate bool
	catCategory  string
)

var modelCmd = &cobra.Command{Use: "model", Aliases: []string{"models"}, Short: "Video qualities and the models behind them"}
var modelListCmd = &cobra.Command{
	Use:     "list",
	Short:   "List qualities, their models and credits per 5 s",
	Example: `  zebracat model list`,
	Args:    cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		out, err := getJSON(videos.Base + "/video/models")
		if err != nil {
			return err
		}
		return emit(out, func() { printModels(out) })
	},
}

var presetCmd = &cobra.Command{Use: "preset", Aliases: []string{"presets"}, Short: "Video presets (street interview, podcast, UGC ad, …)"}
var presetListCmd = &cobra.Command{
	Use:     "list",
	Short:   "List presets for `create --preset`",
	Example: `  zebracat preset list`,
	Args:    cobra.NoArgs,
	RunE:    func(cmd *cobra.Command, args []string) error { return getAndEmit(videos.Base + "/video/presets") },
}

var videoTypesCmd = &cobra.Command{
	Use:   "types",
	Short: "List video types: who can use each and how it is priced",
	Long: `List the video types (AI video, AI images, stock, brainrot, motion graphics,
3D): whether your plan can use each, whether it takes a style, and how it is
priced. The exact price of a video is its quote: zebracat video quote <task_id>.`,
	Example: `  zebracat video types`,
	Args:    cobra.NoArgs,
	RunE:    func(cmd *cobra.Command, args []string) error { return getAndEmit(videos.Base + "/video/types") },
}

var backgroundCmd = &cobra.Command{Use: "background", Short: "Background clips for brainrot videos"}
var backgroundListCmd = &cobra.Command{
	Use:   "list",
	Short: "List background clips (Subway, Minecraft, satisfying, …)",
	Long: `List the background clips a brainrot video plays under its captions, by
category. Pass a clip's src to create --background, or to a plan with
plan edit --tool set_background.`,
	Example: `  zebracat background list --category minecraft
  zebracat create "5 facts about owls" --media-type brainrot --background <src>`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		q := url.Values{}
		if catCategory != "" {
			q.Set("category", catCategory)
		}
		return getAndEmit(withQuery(videos.Base+"/video/backgrounds", q))
	},
}

var languageCmd = &cobra.Command{Use: "language", Short: "Languages for voice-overs and translation"}
var languageListCmd = &cobra.Command{
	Use:   "list",
	Short: "List voice-over languages (--translate: translation targets)",
	Example: `  zebracat language list
  zebracat language list --translate`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error { return listLanguages(catTranslate) },
}

// videoLanguagesCmd is the old spelling of `language list`, kept for scripts.
var videoLanguagesCmd = &cobra.Command{
	Use:    "languages",
	Short:  "List voice-over languages (same as `language list`)",
	Hidden: true,
	Args:   cobra.NoArgs,
	RunE:   func(cmd *cobra.Command, args []string) error { return listLanguages(catTranslate) },
}

func listLanguages(translate bool) error {
	path := videos.Base + "/video/languages"
	if translate {
		path = videos.Base + "/video/translate/languages"
	}
	out, err := getJSON(path)
	if err != nil {
		return err
	}
	return emit(out, func() { printLanguages(out) })
}

// ---- human renderers --------------------------------------------------------

// printModels: one row per quality and model, with credits per 5 s.
func printModels(out any) {
	m, _ := out.(map[string]any)
	rows := [][]string{}
	for _, q := range asList(m["qualities"]) {
		qm, _ := q.(map[string]any)
		avail := "yes"
		if qm["available"] == false {
			avail = "no"
		}
		models := asList(qm["models"])
		if len(models) == 0 {
			rows = append(rows, []string{fmt.Sprint(qm["quality"]), "(picked per scene)", "", avail})
		}
		for _, mo := range models {
			mm, _ := mo.(map[string]any)
			speaks := ""
			if mm["speaking"] == true {
				speaks = " · speaks"
			}
			rows = append(rows, []string{fmt.Sprint(qm["quality"]), fmt.Sprint(mm["model"]) + speaks, fmt.Sprint(mm["credits_per_5s"]), avail})
		}
	}
	ui.Table([]string{"QUALITY", "MODEL", "CREDITS / 5 S", "ON YOUR PLAN"}, rows)
}

// printLanguages reads both language answers: a map of id → {language, …}
// (voice-over languages) or {languages: [{code, name}]} (translation).
func printLanguages(out any) {
	rows := [][]string{}
	switch v := out.(type) {
	case map[string]any:
		if list, ok := v["languages"].([]any); ok {
			for _, l := range list {
				lm, _ := l.(map[string]any)
				rows = append(rows, []string{fmt.Sprint(lm["code"]), fmt.Sprint(lm["name"])})
			}
			ui.Table([]string{"CODE", "LANGUAGE"}, rows)
			return
		}
		keys := make([]string, 0, len(v))
		for k := range v {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			lm, _ := v[k].(map[string]any)
			name := fmt.Sprint(lm["language"])
			if lm == nil {
				name = fmt.Sprint(v[k])
			}
			rows = append(rows, []string{name, strOr(lm["name"]), strOr(lm["voice_id"])})
		}
		ui.Table([]string{"LANGUAGE", "DEFAULT VOICE", "VOICE ID"}, rows)
	default:
		ui.Auto(out)
	}
}

// printStyles groups the visual styles by category: id and name only.
func printStyles(out any) {
	m, ok := out.(map[string]any)
	if !ok {
		ui.Auto(out)
		return
	}
	cats := make([]string, 0, len(m))
	for k := range m {
		cats = append(cats, k)
	}
	sort.Strings(cats)
	for _, cat := range cats {
		list := asList(m[cat])
		if len(list) == 0 {
			continue
		}
		ui.Heading(strings.ReplaceAll(cat, "_", " "))
		rows := [][]string{}
		for _, s := range list {
			sm, _ := s.(map[string]any)
			rows = append(rows, []string{fmt.Sprint(sm["id"]), fmt.Sprint(sm["name"])})
		}
		ui.Table([]string{"ID", "NAME"}, rows)
		fmt.Println()
	}
}

// printPricing: one row per priced item, in plain words.
func printPricing(out any) {
	m, _ := out.(map[string]any)
	rows := [][]string{}
	for _, p := range asList(m["prices"]) {
		pm, _ := p.(map[string]any)
		rows = append(rows, []string{strOr(pm["label"]), fmt.Sprintf("$%v", pm["usd_price"]), "per " + strOr(pm["unit"])})
	}
	ui.Table([]string{"WHAT", "PRICE", "UNIT"}, rows)
}

func strOr(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}

func init() {
	modelCmd.AddCommand(modelListCmd)
	presetCmd.AddCommand(presetListCmd)
	backgroundListCmd.Flags().StringVar(&catCategory, "category", "", "Only this category (see the categories in the answer)")
	backgroundCmd.AddCommand(backgroundListCmd)
	languageListCmd.Flags().BoolVar(&catTranslate, "translate", false, "List the languages a video can be translated into")
	videoLanguagesCmd.Flags().BoolVar(&catTranslate, "translate", false, "List the languages a video can be translated into")
	languageCmd.AddCommand(languageListCmd)
	videoCmd.AddCommand(videoTypesCmd, videoLanguagesCmd)
	rootCmd.AddCommand(modelCmd, presetCmd, backgroundCmd, languageCmd)
}

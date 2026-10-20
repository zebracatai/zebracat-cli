package cmd

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/spf13/cobra"

	"github.com/zebracatai/zebracat-cli/internal/client"
	"github.com/zebracatai/zebracat-cli/internal/clierr"
	"github.com/zebracatai/zebracat-cli/internal/ui"
	"github.com/zebracatai/zebracat-cli/internal/videos"
)

var (
	peTool    string
	peArgs    string
	peBaseRev int
	puToRev   int

	genDryRun   bool
	genNoRender bool
	genYes      bool
	genWait     bool
	genRev      int
	genIdemKey  string
	genTimeout  time.Duration

	ansQuestion string
	ansAnswer   string

	clURL      string
	clMode     string
	clTopic    string
	clLanguage string
	clNoRender bool
	clWait     bool
	clIdemKey  string
	clTimeout  time.Duration
)

var planCmd = &cobra.Command{
	Use:   "plan",
	Short: "Read and change a Director v2 plan before you generate",
	Long: `A plan is the screenplay Director v2 writes for a video: scenes, words,
voices, look. Make one with ` + "`zebracat video create --plan-only`" + `, then:

  zebracat plan show <task_id>
  zebracat plan edit <task_id> --tool set_duration --args '{"seconds":15}'
  zebracat plan chat <task_id> "make it funnier"
  zebracat plan undo <task_id>

Reading and changing a plan is free (plan chat has an hourly allowance).`,
}

var planShowCmd = &cobra.Command{
	Use:   "show <task_id>",
	Short: "Show the plan (screenplay) as the dashboard card reads it",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		plan, err := getMap(c, videos.Path(args[0], "plan"))
		if err != nil {
			return err
		}
		return emit(plan, func() { printPlanHuman(plan) })
	},
}

var planEditCmd = &cobra.Command{
	Use:   "edit <task_id> --tool <name> --args '<json>'",
	Short: "Apply one structured edit to the plan",
	Long: `Apply one structured edit, the same tools the dashboard's plan card uses.

Tools: ` + strings.Join(client.Enum("plan_edit.tool"), ", ") + `

  zebracat plan edit <task_id> --tool set_duration --args '{"seconds":15}'
  zebracat plan edit <task_id> --tool rewrite_line --args '{"beat":2,"text":"…"}' --base-rev 4

--base-rev defaults to the plan's current rev; pass it to fail (409) when the
plan changed since you read it.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if peTool == "" {
			return clierr.Usage("--tool is required")
		}
		if !client.CheckEnum("plan_edit.tool", peTool) {
			return enumUsage("--tool", peTool, "plan_edit.tool")
		}
		toolArgs := map[string]any{}
		if strings.TrimSpace(peArgs) != "" {
			if err := json.Unmarshal([]byte(peArgs), &toolArgs); err != nil {
				return clierr.Usage("--args must be a JSON object: %v", err)
			}
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		rev, err := baseRev(c, args[0], peBaseRev)
		if err != nil {
			return err
		}
		body := map[string]any{"tool": peTool, "args": toolArgs, "base_rev": rev}
		return postPlan(c, videos.Path(args[0], "plan", "edit"), body)
	},
}

var planUndoCmd = &cobra.Command{
	Use:   "undo <task_id>",
	Short: "Undo the last plan change (or go back to --to-rev)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		rev, err := baseRev(c, args[0], peBaseRev)
		if err != nil {
			return err
		}
		body := map[string]any{"base_rev": rev}
		if puToRev > 0 {
			body["to_rev"] = puToRev
		}
		return postPlan(c, videos.Path(args[0], "plan", "undo"), body)
	},
}

var planChatCmd = &cobra.Command{
	Use:   "chat <task_id> <message>",
	Short: "Change the plan in plain words (the dashboard's plan chat)",
	Args:  cobra.MinimumNArgs(2),
	RunE: func(cmd *cobra.Command, args []string) error {
		msg := strings.TrimSpace(strings.Join(args[1:], " "))
		if msg == "" {
			return clierr.Usage("the message is empty")
		}
		if utf8.RuneCountInString(msg) > 4000 {
			return clierr.Usage("the message is longer than 4000 characters")
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		ctx, cancel := ctxTimeout(3 * time.Minute)
		defer cancel()
		var out map[string]any
		if _, err := c.Do(ctx, "POST", videos.Path(args[0], "plan", "chat"), map[string]any{"message": msg}, &out); err != nil {
			return err
		}
		return emit(out, func() {
			if r, _ := out["reply"].(string); r != "" {
				ui.Heading("Zebracat")
				fmt.Println("  " + r)
			}
			// The turn answers the new plan flat ({reply, rev, summary_lines, screenplay, …}).
			if _, ok := out["screenplay"].(map[string]any); ok {
				printPlanHuman(out)
			}
		})
	},
}

// baseRev is the --base-rev flag, or the plan's current rev when unset (-1).
func baseRev(c *client.Client, taskID string, flag int) (int, error) {
	if flag >= 0 {
		return flag, nil
	}
	plan, err := getMap(c, videos.Path(taskID, "plan"))
	if err != nil {
		return 0, err
	}
	rev, _ := plan["rev"].(float64)
	return int(rev), nil
}

func postPlan(c *client.Client, path string, body map[string]any) error {
	ctx, cancel := ctxTimeout(2 * time.Minute)
	defer cancel()
	var out map[string]any
	if _, err := c.Do(ctx, "POST", path, body, &out); err != nil {
		return err
	}
	return emit(out, func() { printPlanHuman(out) })
}

func printPlanHuman(plan map[string]any) {
	sp, _ := plan["screenplay"].(map[string]any)
	title, _ := sp["title"].(string)
	if title == "" {
		title = "Plan"
	}
	ui.Heading(title)
	for _, l := range videos.SummaryLines(plan) {
		fmt.Println("  " + l)
	}
	fmt.Println()
	for _, b := range videos.Beats(plan) {
		fmt.Println("  " + b)
	}
	fmt.Println()
	ui.Info("rev %v · edit with `zebracat plan edit|chat|undo` · quote with `zebracat video quote`", plan["rev"])
}

// ---- video quote / generate / answer / clone -------------------------------

var videoQuoteCmd = &cobra.Command{
	Use:   "quote <task_id>",
	Short: "What Generate will charge for the current plan",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		q, err := getMap(c, videos.Path(args[0], "quote"))
		if err != nil {
			return err
		}
		return emit(q, func() { ui.Info("Quote: %s", videos.QuoteLine(q)) })
	},
}

var videoGenerateCmd = &cobra.Command{
	Use:   "generate <task_id>",
	Short: "Generate the video from its plan (spends credits or API balance)",
	Long: `Generate the video from its current plan — the dashboard's Generate button.

On a terminal it shows the quote and asks first. Scripts and agents (JSON
output, or no terminal) are never asked: pass --yes, or the command refuses.
--dry-run answers the quote and spends nothing.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		c, err := newClient()
		if err != nil {
			return err
		}
		id := args[0]
		body := map[string]any{"should_render": !genNoRender}
		if genRev > 0 {
			body["rev"] = genRev
		}
		if genDryRun {
			body["dry_run"] = true
		} else if !genYes {
			if !interactive() {
				e := clierr.Usage("generate spends credits: pass --yes to confirm (or --dry-run for the quote)")
				e.Code = "confirmation_required"
				return e
			}
			q, err := getMap(c, videos.Path(id, "quote"))
			if err != nil {
				return err
			}
			if !confirm("Generate this video for " + videos.QuoteLine(q) + "?") {
				out := map[string]any{"task_id": id, "generated": false}
				return emit(out, func() { ui.Info("Not generated. The plan is kept.") })
			}
		}
		key := genIdemKey
		if key == "" {
			key = newIdempotencyKey()
		}
		var out map[string]any
		if err := postIdempotent(c, videos.Path(id, "generate"), body, key, genWait, genTimeout, &out); err != nil {
			return err
		}
		if out == nil {
			out = map[string]any{}
		}
		out["idempotency_key"] = key
		if dry, _ := out["dry_run"].(bool); dry {
			return emit(out, func() { printDryRunHuman(out) })
		}
		if !genWait {
			return emit(out, func() { printCreatedHuman(out, id) })
		}
		final, err := waitTask(c, id, genTimeout, true)
		if err != nil {
			return err
		}
		return emit(final, func() { printStatusHuman(final) })
	},
}

var videoAnswerCmd = &cobra.Command{
	Use:   "answer <task_id> --question <id> --answer <text>",
	Short: "Answer the question a plan stopped on (phase needs_input)",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		if ansQuestion == "" || strings.TrimSpace(ansAnswer) == "" {
			return clierr.Usage("--question and --answer are required (see `zebracat video status <task_id>`)")
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		ctx, cancel := ctxTimeout(60 * time.Second)
		defer cancel()
		var out map[string]any
		body := map[string]any{"question_id": ansQuestion, "answer": ansAnswer}
		if _, err := c.Do(ctx, "POST", videos.Path(args[0], "answer"), body, &out); err != nil {
			return err
		}
		return emit(out, func() { printStatusHuman(out) })
	},
}

var videoCloneCmd = &cobra.Command{
	Use:   "clone --url <video> [--mode remix]",
	Short: "Clone a reference video: plan it on Director v2, then generate",
	Long: `Clone a reference video (TikTok, YouTube, Instagram or a direct link): Director v2
measures it, writes a plan in the chosen mode and generates it.

Modes: ` + client.EnumHelp("clone.mode") + `

  zebracat video clone --url https://www.tiktok.com/@x/video/1 --mode generate_similar --topic "home coffee" --wait`,
	RunE: func(cmd *cobra.Command, args []string) error {
		if err := checkURL("--url", clURL); err != nil {
			return err
		}
		if clMode != "" && !client.CheckEnum("clone.mode", clMode) {
			return enumUsage("--mode", clMode, "clone.mode")
		}
		body := map[string]any{"video_url": clURL, "should_render": !clNoRender}
		if clMode != "" {
			body["mode"] = clMode
		}
		if clTopic != "" {
			body["topic"] = clTopic
		}
		if clLanguage != "" {
			body["language"] = clLanguage
		}
		c, err := newClient()
		if err != nil {
			return err
		}
		key := clIdemKey
		if key == "" {
			key = newIdempotencyKey()
		}
		var out map[string]any
		if err := postIdempotent(c, videos.Base+"/video/clone", body, key, clWait, clTimeout, &out); err != nil {
			return err
		}
		if out == nil {
			out = map[string]any{}
		}
		out["idempotency_key"] = key
		taskID, _ := out["task_id"].(string)
		if !clWait || taskID == "" {
			return emit(out, func() { printCreatedHuman(out, taskID) })
		}
		final, err := waitTask(c, taskID, clTimeout, true)
		if err != nil {
			return err
		}
		return emit(final, func() { printStatusHuman(final) })
	},
}

func init() {
	planEditCmd.Flags().StringVar(&peTool, "tool", "", "Edit tool: "+strings.Join(client.Enum("plan_edit.tool"), "|"))
	planEditCmd.Flags().StringVar(&peArgs, "args", "{}", "The tool's arguments as a JSON object")
	planEditCmd.Flags().IntVar(&peBaseRev, "base-rev", -1, "The plan rev you edited from (default: the current rev)")
	planUndoCmd.Flags().IntVar(&peBaseRev, "base-rev", -1, "The plan rev you are undoing from (default: the current rev)")
	planUndoCmd.Flags().IntVar(&puToRev, "to-rev", 0, "Go back to this rev instead of one step")
	planCmd.AddCommand(planShowCmd, planEditCmd, planChatCmd, planUndoCmd)

	gf := videoGenerateCmd.Flags()
	gf.BoolVar(&genDryRun, "dry-run", false, "Answer the quote; spend nothing")
	gf.BoolVar(&genNoRender, "no-render", false, "Keep an editable project; don't export the MP4")
	gf.BoolVar(&genYes, "yes", false, "Don't ask; required when not on a terminal")
	gf.BoolVar(&genWait, "wait", false, "Block until the video is finished")
	gf.IntVar(&genRev, "rev", 0, "Generate this plan rev (default: the current one)")
	gf.StringVar(&genIdemKey, "idempotency-key", "", "Idempotency-Key header (default: a new UUID, printed in the output)")
	gf.DurationVar(&genTimeout, "timeout", 30*time.Minute, "Max time to wait with --wait")

	videoAnswerCmd.Flags().StringVar(&ansQuestion, "question", "", "The question id (from video status)")
	videoAnswerCmd.Flags().StringVar(&ansAnswer, "answer", "", "Your answer")

	cf := videoCloneCmd.Flags()
	cf.StringVar(&clURL, "url", "", "The reference video's URL (required)")
	cf.StringVar(&clMode, "mode", "", "Clone mode: "+client.EnumHelp("clone.mode")+" (default remix)")
	cf.StringVar(&clTopic, "topic", "", "Your topic for the new video")
	cf.StringVar(&clLanguage, "language", "", "Language of the new video")
	cf.BoolVar(&clNoRender, "no-render", false, "Keep an editable project; don't export the MP4")
	cf.BoolVar(&clWait, "wait", false, "Block until the video is finished")
	cf.StringVar(&clIdemKey, "idempotency-key", "", "Idempotency-Key header (default: a new UUID, printed in the output)")
	cf.DurationVar(&clTimeout, "timeout", 30*time.Minute, "Max time to wait with --wait")

	videoCmd.AddCommand(videoQuoteCmd, videoGenerateCmd, videoAnswerCmd, videoCloneCmd)
	rootCmd.AddCommand(planCmd)
}

package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/zebracatai/zebracat-cli/internal/client"
	"github.com/zebracatai/zebracat-cli/internal/ui"
	"github.com/zebracatai/zebracat-cli/internal/version"
)

// flagEnums names the schema enum (client.Enums) behind a flag, keyed
// "<command path> --<flag>", so `commands --json` lists its allowed values.
var flagEnums = map[string]string{
	"video create --media-type": "create.options.media_type",
	"video create --audio":      "create.options.audio_mode",
	"video create --aspect":     "create.options.aspect",
	"video create --quality":    "create.options.quality",
	"video create --preset":     "create.preset",
	"plan edit --tool":          "plan_edit.tool",
	"video clone --mode":        "clone.mode",
}

// cmdFlag is one flag in `zebracat commands --json`.
type cmdFlag struct {
	Name      string   `json:"name"`
	Shorthand string   `json:"shorthand,omitempty"`
	Type      string   `json:"type"`
	Default   string   `json:"default"`
	Usage     string   `json:"usage"`
	Enum      []string `json:"enum,omitempty"`
	Required  bool     `json:"required"`
}

// cmdInfo is one command in `zebracat commands --json`.
type cmdInfo struct {
	Name     string    `json:"name"`  // "video generate"
	Usage    string    `json:"usage"` // "zebracat video generate <task_id> [flags]"
	Summary  string    `json:"summary"`
	Group    string    `json:"group"`
	Paid     bool      `json:"paid"`
	NeedsYes bool      `json:"needs_yes"` // refuses off a terminal without --yes
	Aliases  []string  `json:"aliases"`
	Example  string    `json:"example,omitempty"`
	Flags    []cmdFlag `json:"flags"`
}

var commandsCmd = &cobra.Command{
	Use:   "commands",
	Short: "List every command, its flags and whether it spends (--json for agents)",
	Long: `List every command with its flags, allowed values, and whether it spends
money. With --json (or when piped) it is a machine-readable description of the
whole CLI: an agent can read it once instead of parsing --help pages.

"paid": true means the command spends credits or API dollars. "needs_yes": true
means it refuses to spend off a terminal unless you pass --yes.`,
	Example: `  zebracat commands --json | jq '.commands[] | select(.paid) | .name'
  zebracat commands`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		out := describeCommands()
		return emit(out, func() {
			byGroup := map[string][]cmdInfo{}
			for _, c := range out["commands"].([]cmdInfo) {
				byGroup[c.Group] = append(byGroup[c.Group], c)
			}
			w := 0
			for _, c := range out["commands"].([]cmdInfo) {
				if n := ui.Width(c.Name); n > w {
					w = n
				}
			}
			groups := append([]cobra.Group(nil), helpGroups...)
			groups = append(groups, cobra.Group{ID: "", Title: "Other:"})
			for _, g := range groups {
				cs := byGroup[g.ID]
				if len(cs) == 0 {
					continue
				}
				ui.Heading(g.Title)
				for _, c := range cs {
					paid := ""
					if c.Paid {
						paid = ui.Yellow("  $ paid")
					}
					fmt.Printf("  %s%s  %s%s\n", c.Name, strings.Repeat(" ", w-ui.Width(c.Name)), c.Summary, paid)
				}
				fmt.Println()
			}
		})
	},
}

// describeCommands is the body of `zebracat commands --json`.
func describeCommands() map[string]any {
	prepareCommands()
	var list []cmdInfo
	walkCommands(rootCmd, func(c *cobra.Command) {
		if c == rootCmd || !c.IsAvailableCommand() || pureGroups[c] {
			return
		}
		path := strings.TrimPrefix(c.CommandPath(), rootCmd.Name()+" ")
		top := strings.SplitN(path, " ", 2)[0]
		if top == "help" || top == "completion" {
			return
		}
		info := cmdInfo{
			Name: path, Usage: c.UseLine(), Summary: c.Short, Group: groupOf(path),
			Paid: isPaid(path), Aliases: append([]string{}, c.Aliases...), Example: c.Example,
			Flags: []cmdFlag{},
		}
		info.NeedsYes = info.Paid && c.Flags().Lookup("yes") != nil
		c.LocalFlags().VisitAll(func(f *pflag.Flag) {
			if f.Hidden || f.Deprecated != "" || f.Name == "help" {
				return
			}
			info.Flags = append(info.Flags, describeFlag(path, f))
		})
		list = append(list, info)
	})
	sort.SliceStable(list, func(i, j int) bool { return list[i].Name < list[j].Name })

	var global []cmdFlag
	rootCmd.PersistentFlags().VisitAll(func(f *pflag.Flag) {
		if !f.Hidden && f.Deprecated == "" {
			global = append(global, describeFlag("", f))
		}
	})
	groups := []map[string]string{}
	for _, g := range helpGroups {
		groups = append(groups, map[string]string{"id": g.ID, "title": strings.TrimSuffix(g.Title, ":")})
	}
	return map[string]any{
		"version":      version.Version,
		"commands":     list,
		"global_flags": global,
		"groups":       groups,
		"exit_codes": map[string]string{
			"0": "ok", "1": "API or network error", "2": "usage error (fix the command)",
			"3": "authentication or permission", "4": "timeout",
		},
		"error_envelope": "stderr: {\"error\":{\"code\",\"message\",\"type\",\"retryable\",\"hint\"?,\"request_id\"?,\"field\"?,\"allowed_values\"?,\"docs_url\"?,\"errors\"?}}",
	}
}

func describeFlag(path string, f *pflag.Flag) cmdFlag {
	out := cmdFlag{Name: f.Name, Shorthand: f.Shorthand, Type: f.Value.Type(), Default: f.DefValue, Usage: f.Usage}
	if key, ok := flagEnums[path+" --"+f.Name]; ok {
		out.Enum = client.Enum(key)
	}
	if req, ok := f.Annotations[cobra.BashCompOneRequiredFlag]; ok && len(req) > 0 && req[0] == "true" {
		out.Required = true
	}
	return out
}

func init() { rootCmd.AddCommand(commandsCmd) }

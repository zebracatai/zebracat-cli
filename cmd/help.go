package cmd

import (
	"strings"

	"github.com/spf13/cobra"
)

// helpGroups are the sections of `zebracat --help`, in order. The first eight
// are the product's categories, the same words the API reference, the MCP
// tools and the docs use (api-gap-audit §1, owner O1, 2026-11-20); Workflows,
// the ninth, joins when it ships (O10). "Legacy" holds the old v1-shaped
// commands and "This CLI" the commands about the CLI itself.
var helpGroups = []cobra.Group{
	{ID: "videos", Title: "Videos:"},
	{ID: "remake", Title: "Remake a video:"},
	{ID: "inspiration", Title: "Inspiration:"},
	{ID: "catalogs", Title: "Catalogs:"},
	{ID: "library", Title: "Library:"},
	{ID: "publish", Title: "Publish:"},
	{ID: "account", Title: "Account:"},
	{ID: "webhooks", Title: "Webhooks:"},
	{ID: "legacy", Title: "Legacy:"},
	{ID: "cli", Title: "This CLI:"},
}

// commandTable is THE one place that says which group each command is listed
// under in `zebracat --help` and `zebracat commands`, and which commands spend
// money (credits or API dollars). A rename or regroup edits only this table;
// TestEveryCommandIsInTheTable fails when a command is missing.
//
// Group is set on every top-level command; a subcommand sets it only when it
// belongs to another category than its parent (`video clone` is "Remake a
// video" although `video` is "Videos"). `commands --json` reads it per command.
var commandTable = []struct {
	Command string // path under zebracat, e.g. "video" or "video generate"
	Group   string // helpGroups ID
	Paid    bool   // spends credits or API dollars (D2: needs --yes off a terminal)
}{
	// Videos
	{Command: "chat", Group: "videos"},
	{Command: "create", Group: "videos", Paid: true},
	{Command: "video", Group: "videos"},
	{Command: "video create", Paid: true},
	{Command: "video generate", Paid: true},
	{Command: "plan", Group: "videos"},

	// Remake a video
	{Command: "video clone", Group: "remake", Paid: true},
	{Command: "video translate", Group: "remake", Paid: true},
	{Command: "swap", Group: "remake", Paid: true},
	{Command: "swap options", Group: "remake"},

	// Inspiration
	{Command: "inspiration", Group: "inspiration"},

	// Catalogs: public pick-lists, read-only
	{Command: "video types", Group: "catalogs"},
	{Command: "model", Group: "catalogs"},
	{Command: "preset", Group: "catalogs"},
	{Command: "voice", Group: "catalogs"},
	{Command: "language", Group: "catalogs"},
	{Command: "style", Group: "catalogs"},
	{Command: "music", Group: "catalogs"},
	{Command: "background", Group: "catalogs"},
	{Command: "template", Group: "catalogs"},
	{Command: "avatar", Group: "catalogs"},

	// Library: the user's own things
	{Command: "upload", Group: "library"},
	{Command: "character", Group: "library"},
	{Command: "brand", Group: "library"},
	{Command: "voice clone", Group: "library", Paid: true},
	{Command: "voice clones", Group: "library"},
	{Command: "voice get", Group: "library"},
	{Command: "voice rename", Group: "library"},
	{Command: "voice delete", Group: "library"},
	{Command: "avatar clone", Group: "library"},
	{Command: "avatar status", Group: "library"},

	// Publish
	{Command: "post", Group: "publish"},
	{Command: "post schedule", Group: "publish", Paid: true},

	// Account
	{Command: "account", Group: "account"},
	{Command: "auth", Group: "account"},
	{Command: "login", Group: "account"},

	// Webhooks
	{Command: "webhook", Group: "webhooks"},

	// Legacy: v1-shaped calls, kept working
	{Command: "script", Group: "legacy", Paid: true},
	{Command: "image", Group: "legacy", Paid: true},
	{Command: "video prompt-styles", Group: "legacy"},

	// This CLI
	{Command: "api", Group: "cli"},
	{Command: "commands", Group: "cli"},
	{Command: "config", Group: "cli"},
	{Command: "doctor", Group: "cli"},
	{Command: "update", Group: "cli"},
	{Command: "version", Group: "cli"},
	{Command: "completion", Group: "cli"},
	{Command: "help", Group: "cli"},
}

// isPaid reports whether the command at path (e.g. "video generate") spends.
func isPaid(path string) bool {
	for _, r := range commandTable {
		if r.Command == path {
			return r.Paid
		}
	}
	return false
}

// groupOf is the help group of the command at path ("video clone"): its own
// row's group, else its nearest parent's, else "".
func groupOf(path string) string {
	for p := path; p != ""; {
		for _, r := range commandTable {
			if r.Command == p && r.Group != "" {
				return r.Group
			}
		}
		i := strings.LastIndex(p, " ")
		if i < 0 {
			break
		}
		p = p[:i]
	}
	return ""
}

// assignHelpGroups registers the groups and puts every top-level command in
// its group. A command missing from the table lands under "Additional Commands".
func assignHelpGroups() {
	for i := range helpGroups {
		rootCmd.AddGroup(&helpGroups[i])
	}
	for _, c := range rootCmd.Commands() {
		if g := groupOf(c.Name()); g != "" {
			c.GroupID = g
		}
	}
	rootCmd.SetHelpCommandGroupID("cli")
	rootCmd.SetCompletionCommandGroupID("cli")
}

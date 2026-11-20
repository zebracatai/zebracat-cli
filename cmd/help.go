package cmd

import "github.com/spf13/cobra"

// helpGroups are the sections of `zebracat --help`, in order.
var helpGroups = []cobra.Group{
	{ID: "make", Title: "Make videos:"},
	{ID: "assets", Title: "Voices, looks and assets:"},
	{ID: "account", Title: "Account:"},
	{ID: "cli", Title: "This CLI:"},
}

// commandTable is THE one place that says which group each top-level command
// is listed under in `zebracat --help` and `zebracat commands`, and which
// commands spend money (credits or API dollars). A rename or regroup edits
// only this table; TestEveryCommandIsInTheTable fails when a command is missing.
var commandTable = []struct {
	Command string // path under zebracat, e.g. "video" or "video generate"
	Group   string // helpGroups ID; set on top-level commands only
	Paid    bool   // spends credits or API dollars (D2: needs --yes off a terminal)
}{
	{Command: "chat", Group: "make"},
	{Command: "create", Group: "make", Paid: true},
	{Command: "video", Group: "make"},
	{Command: "video create", Paid: true},
	{Command: "video generate", Paid: true},
	{Command: "video clone", Paid: true},
	{Command: "video translate", Paid: true},
	{Command: "plan", Group: "make"},
	{Command: "swap", Group: "make", Paid: true},
	{Command: "image", Group: "make", Paid: true},
	{Command: "script", Group: "make"},
	{Command: "models", Group: "make"},
	{Command: "presets", Group: "make"},

	{Command: "voice", Group: "assets"},
	{Command: "voice clone", Paid: true},
	{Command: "avatar", Group: "assets"},
	{Command: "character", Group: "assets"},
	{Command: "style", Group: "assets"},
	{Command: "music", Group: "assets"},
	{Command: "template", Group: "assets"},
	{Command: "brand", Group: "assets"},

	{Command: "account", Group: "account"},
	{Command: "auth", Group: "account"},
	{Command: "login", Group: "account"},
	{Command: "webhook", Group: "account"},

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

// groupOf is the help group of the top-level command name, or "".
func groupOf(name string) string {
	for _, r := range commandTable {
		if r.Command == name {
			return r.Group
		}
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

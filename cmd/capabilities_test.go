package cmd

import (
	"encoding/json"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/zebracatai/zebracat-cli/internal/openapi"
)

// cliOnly are the commands with no API capability behind them, and why.
var cliOnly = map[string]string{
	"chat":        "the interactive conversation (it uses the Videos routes)",
	"auth login":  "stores a key on this computer",
	"auth logout": "removes the stored key",
	"auth status": "reads the stored key",
	"auth whoami": "which account the stored key belongs to",
	"config set":  "local settings",
	"config list": "local settings",
	"doctor":      "checks this install",
	"update":      "updates this binary",
	"version":     "this binary's version",
	"api":         "a raw call to any route",
	"voice get":   "one row of `voice clones` (the API has no single-voice read)",
}

type capRow struct {
	ID       string            `json:"id"`
	Category string            `json:"category"`
	CLI      *string           `json:"cli"`
	Why      map[string]string `json:"why"`
}

func capabilityRows(t *testing.T) []capRow {
	t.Helper()
	var doc struct {
		Categories   []string `json:"categories"`
		Capabilities []capRow `json:"capabilities"`
	}
	if err := json.Unmarshal(openapi.Capabilities, &doc); err != nil || len(doc.Capabilities) == 0 {
		t.Fatalf("capabilities.json: %v (%d rows)", err, len(doc.Capabilities))
	}
	return doc.Capabilities
}

// G2: the API's capability table and the CLI can't drift. Every row that
// names a CLI command names a real command (by its own name, not an alias)
// and real flags, in the row's category; every CLI command is named by a row
// or is CLI-only on purpose; a row without a command says why.
func TestCapabilityTable(t *testing.T) {
	prepareCommands()
	titles := map[string]string{}
	for _, g := range helpGroups {
		titles[g.ID] = strings.TrimSuffix(g.Title, ":")
	}
	named := map[string]bool{}
	for _, r := range capabilityRows(t) {
		if r.CLI == nil {
			if r.Why["cli"] == "" {
				t.Errorf("%s: no CLI command and no why.cli", r.ID)
			}
			continue
		}
		var words, flags []string
		for _, w := range strings.Fields(*r.CLI) {
			if strings.HasPrefix(w, "--") {
				flags = append(flags, strings.TrimPrefix(w, "--"))
			} else {
				words = append(words, w)
			}
		}
		c, rest, err := rootCmd.Find(words)
		path := strings.Join(words, " ")
		if err != nil || len(rest) != 0 || strings.TrimPrefix(c.CommandPath(), "zebracat ") != path {
			t.Errorf("%s: cli %q is not a command", r.ID, *r.CLI)
			continue
		}
		for _, f := range flags {
			if c.Flags().Lookup(f) == nil {
				t.Errorf("%s: %q has no --%s", r.ID, path, f)
			}
		}
		if g := groupOf(path); g != "cli" && titles[g] != r.Category {
			t.Errorf("%s: %q is listed under %q in help, the table says %q", r.ID, path, titles[g], r.Category)
		}
		named[path] = true
	}
	var missing []string
	walkCommands(rootCmd, func(c *cobra.Command) {
		path := strings.TrimPrefix(c.CommandPath(), "zebracat ")
		if c == rootCmd || !c.Runnable() || c.Hidden || pureGroups[c] || path == "help" || strings.HasPrefix(path, "completion") || strings.HasPrefix(path, "__") {
			return
		}
		if !named[path] && cliOnly[path] == "" {
			missing = append(missing, path)
		}
	})
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("commands with no capability row (add the row to the API's table, or to cliOnly with why): %v", missing)
	}
}

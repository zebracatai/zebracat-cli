package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/zebracatai/zebracat-cli/internal/ui"
	"github.com/zebracatai/zebracat-cli/internal/update"
	"github.com/zebracatai/zebracat-cli/internal/version"
)

// pathBinary is one zebracat found on PATH.
type pathBinary struct {
	Path    string `json:"path"`
	Version string `json:"version"`
	Method  string `json:"install_method"`
	Runs    bool   `json:"runs_first"` // the one a bare `zebracat` starts
	This    bool   `json:"this"`       // the binary answering now
	Remove  string `json:"remove,omitempty"`
}

// removeCommand is how to uninstall a copy, by how it was installed.
func removeCommand(b pathBinary) string {
	if b.Method == "npm" {
		return "npm uninstall -g @zebracat/cli"
	}
	return "rm " + b.Path
}

// zebracatsOnPath lists every zebracat on PATH, in PATH order, without duplicates.
func zebracatsOnPath() []pathBinary {
	name := "zebracat"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	me := update.CurrentPath()
	seen := map[string]bool{}
	out := []pathBinary{}
	for _, dir := range filepath.SplitList(os.Getenv("PATH")) {
		if dir == "" || !filepath.IsAbs(dir) {
			continue // a relative entry (".") would name whatever is in the current folder
		}
		p := filepath.Join(dir, name)
		st, err := os.Stat(p)
		if err != nil || st.IsDir() || st.Mode()&0o111 == 0 {
			continue
		}
		real := p
		if r, err := filepath.EvalSymlinks(p); err == nil {
			real = r
		}
		if seen[real] {
			continue
		}
		seen[real] = true
		b := pathBinary{Path: p, Method: methodOfPath(real), Runs: len(out) == 0, This: real == me}
		if b.This {
			b.Version = version.Version
		} else {
			b.Version = versionOf(p)
		}
		out = append(out, b)
	}
	return out
}

func methodOfPath(p string) string {
	if strings.Contains(filepath.ToSlash(p), "/node_modules/") {
		return "npm"
	}
	return "binary"
}

// versionOf is another copy's version, read without running it: an npm copy's
// package.json; anything else is "unknown" (doctor never executes other binaries).
func versionOf(path string) string {
	real := path
	if r, err := filepath.EvalSymlinks(path); err == nil {
		real = r
	}
	if methodOfPath(real) == "npm" {
		for dir := filepath.Dir(real); dir != filepath.Dir(dir); dir = filepath.Dir(dir) {
			b, err := os.ReadFile(filepath.Join(dir, "package.json"))
			if err != nil {
				continue
			}
			var pkg struct {
				Name, Version string
			}
			if json.Unmarshal(b, &pkg) == nil && pkg.Name == "@zebracat/cli" && pkg.Version != "" {
				return pkg.Version
			}
			break
		}
	}
	return "unknown"
}

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check this install: which zebracat runs, other copies on PATH, sign-in, updates",
	Example: `  zebracat doctor
  zebracat doctor --json | jq .problems`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		bins := zebracatsOnPath()
		problems := []string{}
		for i := range bins {
			if !bins[i].This {
				bins[i].Remove = removeCommand(bins[i])
				problems = append(problems, fmt.Sprintf("another zebracat (v%s, %s) at %s", bins[i].Version, bins[i].Method, bins[i].Path))
			}
		}
		if len(bins) > 0 && !bins[0].This {
			problems = append([]string{"`zebracat` on your PATH starts " + bins[0].Path + " (v" + bins[0].Version + "), not this one"}, problems...)
		}

		auth := map[string]any{"signed_in": false}
		if c, err := newClient(); err == nil && c.IsAuthenticated() {
			ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
			var acct map[string]any
			_, aerr := c.Do(ctx, "GET", "/api/v1/public/account", nil, &acct)
			cancel()
			auth = map[string]any{"signed_in": aerr == nil, "mode": c.AuthMode(), "base_url": c.BaseURL}
			if aerr != nil {
				auth["error"] = aerr.Error()
				problems = append(problems, "sign-in check failed: "+aerr.Error())
			} else {
				auth["email"] = acct["email"]
			}
		}
		lctx, lcancel := context.WithTimeout(context.Background(), 4*time.Second)
		latest, _ := update.Latest(lctx)
		lcancel()
		out := map[string]any{
			"version": version.Version, "path": update.CurrentPath(), "install_method": update.Method(),
			"on_path": bins, "auth": auth, "latest": latest,
			"update_available": latest != "" && update.Newer("v"+version.Version, latest),
			"problems":         problems,
		}
		if out["update_available"] == true {
			problems = append(problems, "Zebracat CLI "+latest+" is available — run `"+update.UpgradeCommand()+"`")
			out["problems"] = problems
		}
		return emit(out, func() {
			fmt.Print(ui.Banner())
			ui.KV([][2]string{{"version", "v" + version.Version}, {"binary", update.CurrentPath()}, {"installed with", update.Method()}})
			fmt.Println()
			for _, b := range bins {
				mark := "  "
				if b.Runs {
					mark = "→ "
				}
				line := fmt.Sprintf("%s%s  v%s  (%s)", mark, b.Path, b.Version, b.Method)
				if b.This {
					line += "  ← this one"
				}
				fmt.Println(line)
				if b.Remove != "" {
					fmt.Println("     remove it: " + b.Remove)
				}
			}
			fmt.Println()
			if auth["signed_in"] == true {
				ui.Success("Signed in (%v) as %v", auth["mode"], auth["email"])
			} else if _, failed := auth["error"]; !failed {
				ui.Warn("Not signed in — run `zebracat auth login`")
			}
			if len(problems) == 0 {
				ui.Success("All good.")
			}
			for _, p := range problems {
				ui.Warn("%s", p)
			}
		})
	},
}

func init() { rootCmd.AddCommand(doctorCmd) }

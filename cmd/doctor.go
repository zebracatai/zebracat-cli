package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
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
	switch b.Method {
	case "npm":
		return "npm uninstall -g @zebracat/cli"
	case "brew":
		return "brew uninstall zebracat"
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
	s := filepath.ToSlash(p)
	switch {
	case strings.Contains(s, "/node_modules/"):
		return "npm"
	case strings.Contains(s, "/Cellar/") || strings.Contains(s, "/homebrew/") || strings.Contains(s, "/linuxbrew/"):
		return "brew"
	}
	return "binary"
}

// versionOf asks another zebracat for its version (JSON, else its first
// version-looking word).
func versionOf(path string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	c := exec.CommandContext(ctx, path, "version", "--json")
	c.Env = append(os.Environ(), "ZEBRACAT_NO_UPDATE_CHECK=1", "NO_COLOR=1", "ZEBRACAT_OUTPUT=json")
	out, err := c.Output()
	if err != nil && len(out) == 0 {
		return "unknown"
	}
	var v struct {
		Version string `json:"version"`
	}
	if json.Unmarshal(out, &v) == nil && v.Version != "" {
		return strings.TrimPrefix(v.Version, "v")
	}
	for _, f := range strings.Fields(string(out)) {
		f = strings.TrimPrefix(f, "v")
		if f != "" && f[0] >= '0' && f[0] <= '9' && strings.Count(f, ".") >= 1 {
			return f
		}
	}
	return "unknown"
}

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check this install: which zebracat runs, other copies on PATH, sign-in, updates",
	RunE: func(cmd *cobra.Command, args []string) error {
		bins := zebracatsOnPath()
		var problems []string
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
				ui.Warn("Not signed in — run `zebracat login`")
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

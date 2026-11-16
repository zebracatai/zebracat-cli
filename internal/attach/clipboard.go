package attach

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// ErrNoImage: the clipboard holds no picture (maybe text, which the terminal's
// own paste handles).
var ErrNoImage = errors.New("there's no picture in the clipboard")

// The commands, OS and environment the clipboard reader uses (tests swap them).
var (
	run      = func(name string, args ...string) ([]byte, error) { return exec.Command(name, args...).Output() }
	lookPath = exec.LookPath
	goos     = runtime.GOOS
	getenv   = os.Getenv
)

// Clipboard saves the clipboard's picture to a temp PNG and checks it:
// macOS via osascript («class PNGf»), Linux via wl-paste (Wayland) or xclip
// (X11), Windows via PowerShell's Clipboard.GetImage().
func Clipboard() (Image, error) {
	f, err := os.CreateTemp("", "zebracat-paste-*.png")
	if err != nil {
		return Image{}, err
	}
	tmp := f.Name()
	f.Close()
	fail := func(err error) (Image, error) {
		os.Remove(tmp)
		return Image{}, err
	}
	switch goos {
	case "darwin":
		script := []string{
			"-e", fmt.Sprintf("set f to open for access (POSIX file %q) with write permission", tmp),
			"-e", "try",
			"-e", "write (the clipboard as «class PNGf») to f",
			"-e", "end try",
			"-e", "close access f",
		}
		if _, err := run("osascript", script...); err != nil {
			return fail(ErrNoImage)
		}
	case "windows":
		ps := "Add-Type -AssemblyName System.Windows.Forms; Add-Type -AssemblyName System.Drawing; " +
			"$i = [System.Windows.Forms.Clipboard]::GetImage(); " +
			fmt.Sprintf("if ($i -ne $null) { $i.Save('%s', [System.Drawing.Imaging.ImageFormat]::Png) }", strings.ReplaceAll(tmp, "'", "''"))
		if _, err := run("powershell", "-NoProfile", "-STA", "-Command", ps); err != nil {
			return fail(ErrNoImage)
		}
	default:
		data, err := linuxClipboard()
		if err != nil {
			return fail(err)
		}
		if err := os.WriteFile(tmp, data, 0o600); err != nil {
			return fail(err)
		}
	}
	if st, err := os.Stat(tmp); err != nil || st.Size() == 0 {
		return fail(ErrNoImage)
	}
	im, err := Open(tmp)
	if err != nil {
		return fail(err)
	}
	im.Temp = true
	return im, nil
}

// linuxClipboard reads a PNG from wl-paste (Wayland) or xclip (X11).
func linuxClipboard() ([]byte, error) {
	if getenv("WAYLAND_DISPLAY") != "" {
		if _, err := lookPath("wl-paste"); err == nil {
			types, _ := run("wl-paste", "--list-types")
			if !hasLine(types, "image/png") {
				return nil, ErrNoImage
			}
			return run("wl-paste", "--type", "image/png")
		}
	}
	if _, err := lookPath("xclip"); err == nil {
		targets, _ := run("xclip", "-selection", "clipboard", "-t", "TARGETS", "-o")
		if !hasLine(targets, "image/png") {
			return nil, ErrNoImage
		}
		return run("xclip", "-selection", "clipboard", "-t", "image/png", "-o")
	}
	return nil, errors.New("pasting a picture needs wl-clipboard (Wayland) or xclip (X11) — or drag the file in")
}

func hasLine(b []byte, want string) bool {
	for _, l := range strings.Split(string(b), "\n") {
		if strings.TrimSpace(l) == want {
			return true
		}
	}
	return false
}

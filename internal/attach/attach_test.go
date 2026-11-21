package attach

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zebracatai/zebracat-cli/internal/apitest"
	"github.com/zebracatai/zebracat-cli/internal/client"
	"github.com/zebracatai/zebracat-cli/internal/config"
)

func pngBytes(w, h int) []byte {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	img.Set(0, 0, color.RGBA{0x6b, 0x3b, 0xf4, 0xff})
	var b bytes.Buffer
	_ = png.Encode(&b, img)
	return b.Bytes()
}

func write(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// webpVP8X is a minimal WebP header with a 1080×1920 canvas.
func webpVP8X() []byte {
	b := make([]byte, 30)
	copy(b, "RIFF")
	copy(b[8:], "WEBPVP8X")
	w, h := 1080-1, 1920-1
	b[24], b[25], b[26] = byte(w), byte(w>>8), byte(w>>16)
	b[27], b[28], b[29] = byte(h), byte(h>>8), byte(h>>16)
	return b
}

func TestOpenChecksFormatAndSize(t *testing.T) {
	dir := t.TempDir()
	im, err := Open(write(t, dir, "a.png", pngBytes(1080, 1920)))
	if err != nil || im.Format != "png" || im.Width != 1080 || im.Height != 1920 {
		t.Fatalf("png: %+v %v", im, err)
	}
	if !strings.HasPrefix(im.Chip(1), "[Image #1 · 1080×1920 · ") || !strings.HasSuffix(im.Chip(1), "]") {
		t.Fatalf("chip %q", im.Chip(1))
	}
	var jb bytes.Buffer
	_ = jpeg.Encode(&jb, image.NewRGBA(image.Rect(0, 0, 640, 480)), nil)
	if im, err := Open(write(t, dir, "b.jpg", jb.Bytes())); err != nil || im.Format != "jpeg" || im.Width != 640 {
		t.Fatalf("jpeg: %+v %v", im, err)
	}
	if im, err := Open(write(t, dir, "c.webp", webpVP8X())); err != nil || im.Format != "webp" || im.Width != 1080 || im.Height != 1920 {
		t.Fatalf("webp: %+v %v", im, err)
	}
	if _, err := Open(write(t, dir, "d.txt", []byte("hello"))); err == nil || !strings.Contains(err.Error(), "PNG, JPEG or WebP") {
		t.Fatalf("text file: %v", err)
	}
	big := append(pngBytes(10, 10), make([]byte, MaxBytes)...)
	if _, err := Open(write(t, dir, "e.png", big)); err == nil || !strings.Contains(err.Error(), "up to 12.0 MB") {
		t.Fatalf("too big: %v", err)
	}
}

func TestPathsFromAPaste(t *testing.T) {
	dir := t.TempDir()
	spaced := filepath.Join(dir, "my photo.png")
	_ = os.WriteFile(spaced, pngBytes(4, 4), 0o644)
	plain := write(t, dir, "plain.png", pngBytes(4, 4))
	notes := write(t, dir, "notes.txt", []byte("hello"))
	bad := write(t, dir, "broken.png", []byte("not a picture"))
	home := t.TempDir()
	t.Setenv("HOME", home)
	write(t, home, "h.png", pngBytes(4, 4))

	yes := map[string]int{
		plain:                                 1,
		"'" + spaced + "'":                    1, // quoted (Terminal.app, iTerm2)
		`"` + spaced + `"`:                    1,
		strings.ReplaceAll(spaced, " ", `\ `): 1, // backslash-escaped (macOS drag-and-drop)
		"file://" + strings.ReplaceAll(spaced, " ", "%20"): 1,
		"~/h.png":                     1,
		plain + " '" + spaced + "'\n": 2, // two dropped files
	}
	for paste, n := range yes {
		imgs, err := Paths(paste)
		if err != nil || len(imgs) != n {
			t.Fatalf("%q: %d images, %v", paste, len(imgs), err)
		}
	}
	for _, paste := range []string{
		"make a video like " + plain, // a sentence stays text
		notes,                        // a non-image file stays text
		filepath.Join(dir, "missing.png"),
		"relative/photo.png",
		"https://example.com/a.png",
	} {
		if imgs, err := Paths(paste); len(imgs) != 0 || err != nil {
			t.Fatalf("%q should stay text: %v %v", paste, imgs, err)
		}
	}
	if _, err := Paths(bad); err == nil {
		t.Fatal("a .png that is not a picture says why")
	}
}

// fakeOS swaps the commands the clipboard reader runs.
func fakeOS(t *testing.T, os_ string, env map[string]string, have []string, cmd func(name string, args []string) ([]byte, error)) {
	t.Helper()
	oldRun, oldLook, oldOS, oldEnv := run, lookPath, goos, getenv
	t.Cleanup(func() { run, lookPath, goos, getenv = oldRun, oldLook, oldOS, oldEnv })
	goos = os_
	getenv = func(k string) string { return env[k] }
	lookPath = func(name string) (string, error) {
		for _, h := range have {
			if h == name {
				return "/usr/bin/" + name, nil
			}
		}
		return "", errors.New("not found")
	}
	run = func(name string, args ...string) ([]byte, error) { return cmd(name, args) }
}

// between is the text between two markers.
func between(s, a, b string) string {
	i := strings.Index(s, a)
	if i < 0 {
		return ""
	}
	s = s[i+len(a):]
	if j := strings.Index(s, b); j >= 0 {
		return s[:j]
	}
	return ""
}

func TestClipboardPerOS(t *testing.T) {
	pic := pngBytes(1080, 1920)
	// macOS: osascript writes the PNG to the temp file it is given.
	fakeOS(t, "darwin", nil, nil, func(name string, args []string) ([]byte, error) {
		if name != "osascript" {
			t.Fatalf("ran %s", name)
		}
		path := between(strings.Join(args, " "), `POSIX file "`, `"`)
		return nil, os.WriteFile(path, pic, 0o600)
	})
	if im, err := Clipboard(); err != nil || !im.Temp || im.Width != 1080 {
		t.Fatalf("macOS: %+v %v", im, err)
	} else {
		os.Remove(im.Path)
	}
	// macOS, text in the clipboard: the file stays empty.
	fakeOS(t, "darwin", nil, nil, func(string, []string) ([]byte, error) { return nil, nil })
	if _, err := Clipboard(); err != ErrNoImage {
		t.Fatalf("macOS without a picture: %v", err)
	}
	// Linux Wayland.
	fakeOS(t, "linux", map[string]string{"WAYLAND_DISPLAY": "wayland-0"}, []string{"wl-paste"}, func(name string, args []string) ([]byte, error) {
		if args[0] == "--list-types" {
			return []byte("text/plain\nimage/png\n"), nil
		}
		return pic, nil
	})
	if im, err := Clipboard(); err != nil || im.Format != "png" {
		t.Fatalf("Wayland: %+v %v", im, err)
	} else {
		os.Remove(im.Path)
	}
	// Linux X11 with only text.
	fakeOS(t, "linux", nil, []string{"xclip"}, func(name string, args []string) ([]byte, error) {
		if strings.Contains(strings.Join(args, " "), "TARGETS") {
			return []byte("UTF8_STRING\nTEXT\n"), nil
		}
		t.Fatal("never reads a picture that isn't there")
		return nil, nil
	})
	if _, err := Clipboard(); err != ErrNoImage {
		t.Fatalf("X11 text: %v", err)
	}
	// Linux with no clipboard tool.
	fakeOS(t, "linux", nil, nil, func(string, []string) ([]byte, error) { return nil, nil })
	if _, err := Clipboard(); err == nil || !strings.Contains(err.Error(), "xclip") {
		t.Fatalf("no tool: %v", err)
	}
	// Windows: PowerShell saves the clipboard picture.
	fakeOS(t, "windows", nil, nil, func(name string, args []string) ([]byte, error) {
		path := between(args[len(args)-1], "Save('", "'")
		return nil, os.WriteFile(path, pic, 0o600)
	})
	if im, err := Clipboard(); err != nil || im.Height != 1920 {
		t.Fatalf("Windows: %+v %v", im, err)
	} else {
		os.Remove(im.Path)
	}
}

// Upload posts one "file" to the public API's /uploads and returns the stored URL.
func TestUploadToThePublicAPI(t *testing.T) {
	srv := apitest.New(t)
	var gotName, gotKey, gotType string
	var gotLen, gotParts int
	srv.Handle(UploadRoute, func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		gotKey = r.Header.Get("X-API-KEY")
		f, h, err := r.FormFile("file")
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(f)
		gotLen, gotName, gotType = len(b), h.Filename, h.Header.Get("Content-Type")
		gotParts = len(r.MultipartForm.File) + len(r.MultipartForm.Value)
		w.WriteHeader(201)
		_, _ = w.Write([]byte(`{"url": "https://cdn.example.com/uploads/u1.png", "kind_hint": "product"}`))
	})
	srv.Handle("/api/user/upload_assets", func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("the dashboard upload route is not on api.zebracat.ai")
	})
	t.Setenv("ZEBRACAT_API_KEY", "sk-dummy")
	cl := client.New(srv.URL, &config.Credentials{}, "")
	data := pngBytes(8, 8)
	im, _ := Open(write(t, t.TempDir(), "mug.png", data))
	st, err := Upload(context.Background(), cl, im)
	if err != nil || st.URL != "https://cdn.example.com/uploads/u1.png" || st.KindHint != "product" {
		t.Fatalf("upload: %+v %v", st, err)
	}
	if gotName != "mug.png" || gotKey != "sk-dummy" || gotLen != len(data) || gotType != "image/png" || gotParts != 1 {
		t.Fatalf("multipart: name %q key %q len %d type %q parts %d", gotName, gotKey, gotLen, gotType, gotParts)
	}
	// The person's kind wins; else the server's hint; else other.
	up := Uploads([]Stored{st, st, {URL: "u3", KindHint: "spaceship"}}, []Image{{Kind: "person"}, {}, {}})
	if up[0]["url"] != st.URL || up[0]["kind"] != "person" || up[1]["kind"] != "product" || up[2]["kind"] != "other" {
		t.Fatalf("uploads %v", up)
	}
}

// A refused upload (type, size) surfaces the API's error, and no url is an error.
func TestUploadErrors(t *testing.T) {
	srv := apitest.New(t)
	answer := `{"error": {"code": "unsupported_file_type", "message": "Only PNG, JPEG or WebP pictures.", "type": "invalid_request_error"}}`
	status := 400
	srv.Handle(UploadRoute, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(answer))
	})
	t.Setenv("ZEBRACAT_API_KEY", "sk-dummy")
	cl := client.New(srv.URL, &config.Credentials{}, "")
	im, _ := Open(write(t, t.TempDir(), "mug.png", pngBytes(8, 8)))
	if _, err := Upload(context.Background(), cl, im); err == nil || !strings.Contains(err.Error(), "PNG, JPEG or WebP") {
		t.Fatalf("400: %v", err)
	}
	status, answer = 201, `{}`
	if _, err := Upload(context.Background(), cl, im); err == nil || !strings.Contains(err.Error(), "no url") {
		t.Fatalf("no url: %v", err)
	}
}

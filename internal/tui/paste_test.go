package tui

import (
	"bytes"
	"fmt"
	"image"
	"image/png"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/muesli/termenv"

	"github.com/zebracatai/zebracat-cli/internal/attach"
)

func picture(t *testing.T, name string, w, h int) string {
	t.Helper()
	var b bytes.Buffer
	_ = png.Encode(&b, image.NewRGBA(image.Rect(0, 0, w, h)))
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func paste(m *model, text string) []tea.Msg {
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(text), Paste: true})
	return msgs(cmd)
}

// A dropped picture path becomes a chip; Backspace on an empty line removes it.
func TestDroppedPictureBecomesAChip(t *testing.T) {
	m, _ := against(t)
	p := picture(t, "my photo.png", 1080, 1920)
	paste(m, "'"+p+"'")
	if len(m.images) != 1 || m.in.Value() != "" {
		t.Fatalf("images %d, input %q", len(m.images), m.in.Value())
	}
	if v := m.View(); !strings.Contains(v, "[Image #1 · 1080×1920 · ") {
		t.Fatalf("no chip:\n%s", v)
	}
	m.Update(tea.KeyMsg{Type: tea.KeyBackspace})
	if len(m.images) != 0 {
		t.Fatal("backspace on an empty line removes the chip")
	}
	// Text paste (even several lines) stays text.
	paste(m, "NARRATOR: line one\nNARRATOR: line two")
	if len(m.images) != 0 || m.in.LineCount() != 2 {
		t.Fatalf("text paste: images %d lines %d", len(m.images), m.in.LineCount())
	}
}

// Ctrl+V reads the clipboard picture; without one it says how to paste text.
func TestCtrlVPastesAClipboardPicture(t *testing.T) {
	m, _ := against(t)
	old := clipboardImage
	defer func() { clipboardImage = old }()
	p := picture(t, "clip.png", 640, 480)
	clipboardImage = func() (attach.Image, error) { im, err := attach.Open(p); im.Temp = true; return im, err }
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	m.Update(find[pastedMsg](t, msgs(cmd)))
	if len(m.images) != 1 || m.images[0].Width != 640 {
		t.Fatalf("images %+v", m.images)
	}
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("v"), Alt: true}) // Alt+V, for Windows Terminal
	m.Update(find[pastedMsg](t, msgs(cmd)))
	if len(m.images) != 2 {
		t.Fatalf("Alt+V: images %d", len(m.images))
	}
	m.dropImage(1)
	clipboardImage = func() (attach.Image, error) { return attach.Image{}, attach.ErrNoImage }
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlV})
	_, cmd = m.Update(find[pastedMsg](t, msgs(cmd)))
	if out := printed(msgs(cmd)); !strings.Contains(out, "No picture in the clipboard") || len(m.images) != 1 {
		t.Fatalf("no picture: %q", out)
	}
}

// On send the pictures are uploaded like the studio's, then go to the plan.
func TestPicturesGoWithThePlan(t *testing.T) {
	m, srv := against(t)
	n := 0
	srv.Handle("/api/user/upload_assets", func(w http.ResponseWriter, r *http.Request) {
		n++
		_ = r.ParseMultipartForm(1 << 20)
		if r.FormValue("path") != attach.UploadPath {
			t.Fatalf("path %q", r.FormValue("path"))
		}
		fmt.Fprintf(w, `{"uploaded_files": [{"url": "https://cdn.example.com/ai-chat/uploads/%d.png"}]}`, n)
	})
	paste(m, picture(t, "a.png", 8, 8)+" "+picture(t, "b.png", 8, 8))
	find[createdMsg](t, say(m, "a launch video for my mug"))
	post, _ := srv.Last("POST", "/videos")
	ups, _ := post.Body["uploads"].([]any)
	if n != 2 || len(ups) != 2 || ups[0].(map[string]any)["url"] != "https://cdn.example.com/ai-chat/uploads/1.png" ||
		ups[1].(map[string]any)["kind"] != "other" || post.Body["auto_generate"] != false {
		t.Fatalf("uploads %d, body %v", n, post.Body)
	}
	if len(m.images) != 0 {
		t.Fatal("sent pictures leave the input")
	}
}

// The plan chat takes words only: pictures wait for a new video.
func TestPicturesDontGoToThePlanChat(t *testing.T) {
	m, srv := against(t)
	m.job = &job{taskID: taskID}
	m.stage = stageReady
	paste(m, picture(t, "a.png", 8, 8))
	out := printed(say(m, "use this picture"))
	if !strings.Contains(out, "Pictures go with a new video") || srv.Count("POST", "/plan/chat") != 0 || len(m.images) != 1 {
		t.Fatalf("out %q chats %d", out, srv.Count("POST", "/plan/chat"))
	}
}

// The chips at 60 and 80 columns.
func TestChipGolden(t *testing.T) {
	lipgloss.SetColorProfile(termenv.Ascii)
	for _, w := range []int{60, 80} {
		m := sized(t, w, 24)
		m.images = []attach.Image{{Width: 1080, Height: 1920, Bytes: 245760}, {Width: 640, Height: 480, Bytes: 1 << 20}}
		golden(t, fmt.Sprintf("chips_%d", w), m.liveView())
		if maxWidth(m.liveView()) > w {
			t.Fatalf("chips wider than %d", w)
		}
	}
}

// A failed upload gives the pictures back to the input.
func TestFailedUploadKeepsThePictures(t *testing.T) {
	m, srv := against(t)
	srv.Handle("/api/user/upload_assets", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(500)
		_, _ = w.Write([]byte(`{"status": false, "message": "storage is down"}`))
	})
	paste(m, picture(t, "a.png", 8, 8))
	created := find[createdMsg](t, say(m, "a mug video"))
	_, cmd := m.Update(created)
	if out := printed(msgs(cmd)); len(m.images) != 1 || !strings.Contains(out, "still attached") || srv.Count("POST", "/videos") != 0 {
		t.Fatalf("images %d out %q", len(m.images), out)
	}
}

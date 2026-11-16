// Small cards and helpers the conversation prints: the plan's question, a
// task's error, an idempotency key.
package tui

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/zebracatai/zebracat-cli/internal/videos"
)

// clean drops control characters (ESC, C0/C1, DEL) from text the server or the
// plan model wrote, so it can never move the cursor, retitle the window or hide
// lines; newlines and tabs stay.
func clean(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n' || r == '\t':
			return r
		case r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f):
			return -1
		}
		return r
	}, s)
}

// questionCard shows the question a plan stopped on, its options numbered.
func questionCard(task map[string]any) string {
	_, text, opts := videos.Question(task)
	text = clean(text)
	var b strings.Builder
	b.WriteString(stTitle.Render("🦓 ") + text + "\n")
	for i, o := range opts {
		b.WriteString(stKey.Render(fmt.Sprintf("   %d ", i+1)) + clean(o) + "\n")
	}
	b.WriteString(stHint.Render("  Type your answer (or its number)."))
	return b.String()
}

// errText is a task's error as one line (v1 sends a string, v2 an object).
func errText(m map[string]any) string {
	switch e := m["error"].(type) {
	case string:
		return clean(e)
	case map[string]any:
		if s, _ := e["message"].(string); s != "" {
			return clean(s)
		}
	}
	return ""
}

func newKey() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

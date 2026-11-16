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

// questionCard shows the question a plan stopped on, its options numbered.
func questionCard(task map[string]any) string {
	_, text, opts := videos.Question(task)
	var b strings.Builder
	b.WriteString(stTitle.Render("🦓 ") + text + "\n")
	for i, o := range opts {
		b.WriteString(stKey.Render(fmt.Sprintf("   %d ", i+1)) + o + "\n")
	}
	b.WriteString(stHint.Render("  Type your answer (or its number)."))
	return b.String()
}

// errText is a task's error as one line (v1 sends a string, v2 an object).
func errText(m map[string]any) string {
	switch e := m["error"].(type) {
	case string:
		return e
	case map[string]any:
		if s, _ := e["message"].(string); s != "" {
			return s
		}
	}
	return ""
}

func newKey() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// The blocks the conversation prints: the plan card, the question, the finished
// video, the spend question; plus small helpers (task error, keys, clean text).
package tui

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"

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

// boxWidth is the card width for a terminal: its width minus a margin, 30–100.
func boxWidth(width int) int {
	w := width - 2
	if width <= 0 {
		w = 80
	}
	if w > 100 {
		w = 100
	}
	if w < 30 {
		w = 30
	}
	return w
}

// planCard is the plan as a bordered card: title, the facts line, the scenes,
// the voices, the price, then what the person can do next.
func planCard(j *job, width int) string {
	src := j.plan
	if src == nil {
		src = j.task
	}
	sp, _ := src["screenplay"].(map[string]any)
	title, _ := sp["title"].(string)
	if title == "" {
		if s, ok := j.task["plan_summary"].(map[string]any); ok {
			title, _ = s["title"].(string)
		}
	}
	if title == "" {
		title = "Your plan"
	}
	w := boxWidth(width)
	inner := w - 4 // border + padding
	wrap := func(s string) string { return ansi.Wrap(s, inner, "") }

	var b strings.Builder
	b.WriteString(wrap(stTitle.Render(clean(title))) + "\n")
	if facts := planFacts(sp, j.task); facts != "" {
		b.WriteString(wrap(stMuted.Render(clean(facts))) + "\n")
	}
	if beats := videos.Beats(src); len(beats) > 0 {
		b.WriteString("\n")
		for _, l := range beats {
			l = clean(l)
			if i := strings.Index(l, ") "); i > 0 && i < 12 { // "1. (5s) words": number and length dim
				l = stMuted.Render(l[:i+1]) + l[i+1:]
			}
			b.WriteString(hangText(l, inner, 3) + "\n")
		}
	} else {
		for _, l := range videos.SummaryLines(src) {
			b.WriteString(wrap(clean(l)) + "\n")
		}
	}
	b.WriteString("\n")
	if v := voices(sp); v != "" {
		b.WriteString(hangText(stMuted.Render("Voice  ")+clean(v), inner, 7) + "\n")
	}
	if q := videos.QuoteLine(j.quote); q != "" {
		b.WriteString(hangText(stMuted.Render("Price  ")+clean(q), inner, 7) + "\n")
	}
	if j.editorURL != "" {
		b.WriteString(hangText(stMuted.Render("Studio ")+stMuted.Render(clean(j.editorURL)), inner, 7) + "\n")
	}
	card := stBox.Width(w - 2).Render(strings.TrimRight(b.String(), "\n")) // Width excludes the border
	hint := ansi.Wrap(stHint.Render("  Change it in plain words · ctrl+g to generate"), w, "")
	return card + "\n" + hint
}

// hangText wraps s to width with every line after the first indented by indent.
func hangText(s string, width, indent int) string {
	lines := strings.Split(ansi.Wrap(s, width, ""), "\n")
	if len(lines) == 1 {
		return lines[0]
	}
	rest := strings.Join(lines[1:], " ")
	more := strings.Split(ansi.Wrap(rest, width-indent, ""), "\n")
	pad := strings.Repeat(" ", indent)
	for i := range more {
		more[i] = pad + more[i]
	}
	return lines[0] + "\n" + strings.Join(more, "\n")
}

// planFacts is "20 s · 4 scenes · vertical · ai video · english · narration".
func planFacts(sp, task map[string]any) string {
	var parts []string
	secs := 0.0
	beats, _ := sp["beats"].([]any)
	for _, b := range beats {
		if bm, ok := b.(map[string]any); ok {
			s, _ := bm["seconds"].(float64)
			secs += s
		}
	}
	if secs == 0 {
		if s, ok := task["plan_summary"].(map[string]any); ok {
			secs, _ = s["seconds"].(float64)
		}
	}
	if secs > 0 {
		parts = append(parts, strconv.FormatFloat(secs, 'f', -1, 64)+" s")
	}
	if len(beats) > 0 {
		if len(beats) == 1 {
			parts = append(parts, "1 scene")
		} else {
			parts = append(parts, fmt.Sprintf("%d scenes", len(beats)))
		}
	}
	for _, k := range []string{"aspect", "media_type", "language"} {
		if s, _ := sp[k].(string); s != "" {
			parts = append(parts, strings.ReplaceAll(s, "_", " "))
		}
	}
	if a, ok := sp["audio"].(map[string]any); ok {
		if s, _ := a["mode"].(string); s != "" {
			parts = append(parts, s)
		}
	}
	return strings.Join(parts, " · ")
}

// voices lists the cast's voices: "Narrator (Rachel) · Sam".
func voices(sp map[string]any) string {
	cast, _ := sp["cast"].([]any)
	var out []string
	for _, c := range cast {
		cm, _ := c.(map[string]any)
		name, _ := cm["name"].(string)
		if name == "" {
			continue
		}
		if v, ok := cm["voice"].(map[string]any); ok {
			for _, k := range []string{"name", "label", "voice_name"} {
				if s, _ := v[k].(string); s != "" {
					name += " (" + s + ")"
					break
				}
			}
		}
		out = append(out, name)
	}
	return strings.Join(out, " · ")
}

// doneCard is the finished video: its link, the studio link and how to open it.
func doneCard(j *job, elapsed time.Duration) string {
	var b strings.Builder
	b.WriteString(stKey.Render("● ") + "Your video is ready" + stMuted.Render(" · "+clock(elapsed)) + "\n")
	if j.videoURL != "" {
		b.WriteString(stMuted.Render("  Video  ") + stKey.Render(clean(j.videoURL)) + "\n")
	}
	if j.editorURL != "" {
		b.WriteString(stMuted.Render("  Studio "+clean(j.editorURL)) + "\n")
	}
	b.WriteString(stHint.Render("  ctrl+o to watch it · type a new idea to make another"))
	return b.String()
}

// questionCard is the question a plan stopped on, its options numbered.
func questionCard(task map[string]any) string {
	_, text, opts := videos.Question(task)
	var b strings.Builder
	b.WriteString(clean(text))
	for i, o := range opts {
		b.WriteString("\n" + stKey.Render(fmt.Sprintf("  %d ", i+1)) + clean(o))
	}
	return b.String()
}

// confirmView is the spend question.
func (m *model) confirmView() string {
	q := videos.QuoteLine(m.job.quote)
	price := "this video"
	if q != "" {
		price = strings.SplitN(q, " — ", 2)[0]
	}
	box := stAsk.Width(boxWidth(m.width) - 2).Render(
		stTitle.Render("Generate it for "+clean(price)+"?") + "\n" +
			stMuted.Render("This spends credits. The plan stays as it is either way.") + "\n\n" +
			stKey.Render("y") + " generate   " + stKey.Render("n") + stMuted.Render(" or ") + stKey.Render("esc") + " not now")
	return m.fit(box)
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

// errCode is a v2 task's error code ("" for none).
func errCode(m map[string]any) string {
	if e, ok := m["error"].(map[string]any); ok {
		s, _ := e["code"].(string)
		return s
	}
	return ""
}

func clock(d time.Duration) string {
	if d < 0 {
		d = 0
	}
	s := int(d.Seconds())
	return fmt.Sprintf("%d:%02d", s/60, s%60)
}

func newKey() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

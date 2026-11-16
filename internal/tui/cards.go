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

	"github.com/charmbracelet/lipgloss"
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

// planCard is the plan as a bordered card: title, the facts (Type, Style,
// Format, Length, Scenes; then Audio, Language), the scenes, the voices, the
// price and the studio link. A plan before Generate (a "promise": no screenplay
// until Generate writes it, as on the studio's plan card) shows its story's
// settings and scenes. The Generate / Discard choice sits under it (view.go).
func planCard(j *job, width int) string {
	src := j.plan
	if src == nil {
		src = j.task
	}
	sp, _ := src["screenplay"].(map[string]any)
	pr, _ := src["promise"].(map[string]any)
	title, _ := sp["title"].(string)
	if title == "" {
		title, _ = pr["title"].(string)
	}
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
	b.WriteString(wrap(stHead.Render(clean(title))) + "\n")
	if facts := planFacts(sp, pr, j.task); len(facts) > 0 {
		// two rows: what it is (Type, Style, Format, Length, Scenes), then how it sounds
		var rows [2][]string
		for _, f := range facts {
			r := 0
			if f[0] == "Audio" || f[0] == "Language" {
				r = 1
			}
			rows[r] = append(rows[r], stLabel.Render(f[0])+" "+clean(f[1]))
		}
		for _, row := range rows {
			if len(row) > 0 {
				b.WriteString(wrap(strings.Join(row, stMuted.Render(" · "))) + "\n")
			}
		}
	}
	beats := videos.Beats(src)
	if len(beats) == 0 {
		beats = promiseScenes(pr)
	}
	if len(beats) > 0 {
		b.WriteString("\n")
		for _, l := range beats {
			l = clean(l)
			// "1. (5s) words": the number in the accent, the length dim
			if d := strings.Index(l, ". ("); d > 0 && d < 5 {
				if e := strings.Index(l, ") "); e > d {
					l = stHead.Render(l[:d+1]) + " " + stMuted.Render(l[d+2:e+1]) + l[e+1:]
				}
			} else if d := strings.Index(l, ". "); d > 0 && d < 5 {
				l = stHead.Render(l[:d+1]) + l[d+1:]
			}
			b.WriteString(hangText(l, inner, 3) + "\n")
		}
	} else {
		for _, l := range videos.SummaryLines(src) {
			b.WriteString(wrap(clean(l)) + "\n")
		}
	}
	b.WriteString("\n")
	v := voices(sp)
	if v == "" {
		v = voices(pr)
	}
	if v != "" {
		b.WriteString(hangText(stLabel.Render("Voice  ")+clean(v), inner, 7) + "\n")
	}
	if main, note := priceLines(j.quote); main != "" {
		b.WriteString(hangText(stLabel.Render("Price  ")+priceStyle(j.quote).Render(clean(main)), inner, 7) + "\n")
		if note != "" {
			b.WriteString(hangText("       "+stMuted.Render(clean(note)), inner, 7) + "\n")
		}
	}
	if j.editorURL != "" {
		b.WriteString(hangText(stLabel.Render("Studio ")+stMuted.Render(clean(j.editorURL)), inner, 7) + "\n")
	}
	return stBox.Width(w - 2).Render(strings.TrimRight(b.String(), "\n")) // Width excludes the border
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

// typeLabels are the studio's names for the plan's media types (an unknown one
// is shown as it comes, underscores as spaces).
var typeLabels = map[string]string{
	"ai_video":        "AI video",
	"ai_image_video":  "AI images",
	"stock":           "Stock footage",
	"brainrot":        "Brainrot",
	"avatar":          "Avatar",
	"motion_graphics": "Motion graphics",
}

// audioLabels name the plan's audio modes.
var audioLabels = map[string]string{
	"narration": "voice-over",
	"dialogue":  "dialogue",
	"silent":    "no voice",
}

func label(set map[string]string, v string) string {
	if l, ok := set[v]; ok {
		return l
	}
	return v
}

// planFacts are the plan's labelled facts, only the ones the plan has: Type,
// Style, Format, Length, Scenes, Audio, Language. A written screenplay answers
// first; before Generate the promise's settings and the task's plan_summary do.
func planFacts(sp, pr, task map[string]any) [][2]string {
	var out [][2]string
	add := func(label, v string) {
		if v != "" {
			out = append(out, [2]string{label, strings.ReplaceAll(v, "_", " ")})
		}
	}
	summary, _ := task["plan_summary"].(map[string]any)
	settings, _ := pr["settings"].(map[string]any)
	str := func(m map[string]any, k string) string { v, _ := m[k].(string); return v }
	first := func(vs ...string) string {
		for _, v := range vs {
			if v != "" {
				return v
			}
		}
		return ""
	}
	secs := 0.0
	beats, _ := sp["beats"].([]any)
	for _, b := range beats {
		if bm, ok := b.(map[string]any); ok {
			s, _ := bm["seconds"].(float64)
			secs += s
		}
	}
	if secs == 0 {
		secs, _ = summary["seconds"].(float64)
	}
	if secs == 0 {
		if l, ok := pr["length"].(map[string]any); ok {
			secs, _ = l["estimate_s"].(float64)
		}
	}
	scenes := len(beats)
	if scenes == 0 {
		ps, _ := pr["scenes"].([]any)
		scenes = len(ps)
	}
	if scenes == 0 {
		n, _ := summary["scenes"].(float64)
		scenes = int(n)
	}
	style := str(sp, "style")
	if m, ok := sp["style"].(map[string]any); ok {
		style = str(m, "name")
	}
	audio := str(settings, "audio")
	if a, ok := sp["audio"].(map[string]any); ok {
		audio = str(a, "mode")
	}
	add("Type", label(typeLabels, first(str(sp, "media_type"), str(settings, "type"), str(summary, "media_type"))))
	add("Style", first(style, str(settings, "style")))
	add("Format", first(str(sp, "aspect"), str(settings, "aspect")))
	if secs > 0 {
		add("Length", strconv.FormatFloat(secs, 'f', -1, 64)+" s")
	}
	if scenes > 0 {
		add("Scenes", strconv.Itoa(scenes))
	}
	add("Audio", label(audioLabels, first(audio, str(summary, "audio_mode"))))
	add("Language", first(str(sp, "language"), str(settings, "language"), str(summary, "language")))
	return out
}

// promiseScenes are a plan's scenes before Generate writes the screenplay:
// "1. what is filmed — Speaker: line / Speaker: line".
func promiseScenes(pr map[string]any) []string {
	scenes, _ := pr["scenes"].([]any)
	var out []string
	for i, s := range scenes {
		sm, _ := s.(map[string]any)
		if sm == nil {
			continue
		}
		n := num(sm["n"])
		if sm["n"] == nil {
			n = strconv.Itoa(i + 1)
		}
		words, _ := sm["film"].(string)
		var said []string
		lines, _ := sm["lines"].([]any)
		for _, l := range lines {
			if lm, ok := l.(map[string]any); ok {
				text, _ := lm["text"].(string)
				if who, _ := lm["speaker"].(string); who != "" {
					text = who + ": " + text
				}
				said = append(said, text)
			}
		}
		if len(said) > 0 {
			words = strings.TrimSpace(words + " — " + strings.Join(said, " / "))
		}
		if words == "" {
			words = "(no words)"
		}
		out = append(out, n+". "+words)
	}
	return out
}

func num(v any) string {
	switch n := v.(type) {
	case float64:
		return strconv.FormatFloat(n, 'f', -1, 64)
	case nil:
		return "?"
	}
	return fmt.Sprint(v)
}

// priceLines is the card's price: the figure, and one short line under it. A
// plan priced only once Generate writes its screenplay shows the studio card's
// estimate and says the exact price comes then; a plan that spends no scene
// credits (stock footage, for one) says why its price is 0.
func priceLines(q map[string]any) (main, note string) {
	if q == nil {
		return "", ""
	}
	plan, _ := q["plan"].(map[string]any)
	media, _ := plan["media_type"].(string)
	estimate := q["basis"] == "estimate" && (media == "" || media == "ai_video")
	main = num(q["credits"]) + " credits"
	if estimate {
		main = "about " + main
	}
	if usd, _ := q["usd"].(string); usd != "" {
		main += " ($" + usd + ")"
	}
	switch q["pool"] {
	case "api_dollar":
		main += " from your API balance"
	case "account":
		main += " from your plan credits"
	}
	if suf, ok := q["sufficient"].(bool); ok && !suf {
		main += " — not enough balance"
	}
	if estimate {
		return main, "Estimate: the exact price is set when Generate writes the screenplay."
	}
	why, _ := q["why"].(string)
	return main, why
}

// generateLabel is the Generate choice: "Generate (about 76 credits)".
func generateLabel(q map[string]any) string {
	if q == nil {
		return "Generate"
	}
	main, _ := priceLines(q)
	main = strings.SplitN(main, " from your ", 2)[0]
	main = strings.SplitN(main, " (", 2)[0]
	return "Generate (" + main + ")"
}

// priceStyle colours the price: green when the balance covers it, red when it
// doesn't, yellow when the server didn't say.
func priceStyle(q map[string]any) lipgloss.Style {
	switch ok, known := q["sufficient"].(bool); {
	case known && ok:
		return stGood
	case known && !ok:
		return stErr
	}
	return stWarn
}

// voices lists the cast's voices: "Narrator (Rachel) · Sam". A voice the plan
// describes in words (before Generate casts it) is left out: the name says who.
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
	b.WriteString(stGood.Render("✓ ") + "Your video is ready" + stMuted.Render(" · "+clock(elapsed)) + "\n")
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

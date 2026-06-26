// Package dash holds presentation helpers for the dashboard-style project views,
// shared by the CLI (cmd) and the interactive shell (tui). It is colour-free —
// each caller applies its own theme (ui colours or lipgloss styles).
package dash

import (
	"fmt"
	"strings"
	"time"
)

// HumanType turns an internal video_type into a friendly label.
func HumanType(t string) string {
	switch t {
	case "ai_video":
		return "AI Video"
	case "moving_ai_images":
		return "Moving AI Images"
	case "ai_avatar", "avatar":
		return "Avatar"
	case "stock_footage":
		return "Stock"
	case "brainrot":
		return "Brainrot"
	case "idea":
		return "Idea"
	case "script":
		return "Script"
	case "blog":
		return "Blog"
	case "audio":
		return "Audio"
	case "translate", "translation":
		return "Translation"
	case "", "<nil>", "None":
		return "Video"
	}
	return titleize(strings.ReplaceAll(t, "_", " "))
}

// Kind buckets a status for colouring: ok | working | pending | failed | cancelled | other.
func Kind(status string) string {
	switch status {
	case "completed":
		return "ok"
	case "processing", "rendering", "avatar_rendering":
		return "working"
	case "pending":
		return "pending"
	case "failed", "render_failed", "avatar_render_failed":
		return "failed"
	case "cancelled":
		return "cancelled"
	}
	return "other"
}

// Label is the friendly status word shown to users.
func Label(status string) string {
	switch status {
	case "avatar_rendering":
		return "rendering"
	case "render_failed", "avatar_render_failed":
		return "failed"
	}
	return status
}

// Icon is the single-column status glyph.
func Icon(status string) string {
	switch Kind(status) {
	case "ok":
		return "✓"
	case "working":
		return "◐"
	case "pending":
		return "○"
	case "failed":
		return "✗"
	case "cancelled":
		return "⊘"
	}
	return "•"
}

// StudioURL is the human project page (open/watch/edit) for any project id.
func StudioURL(projectID any) string {
	var id string
	switch v := projectID.(type) {
	case float64:
		id = fmt.Sprintf("%d", int64(v))
	case int:
		id = fmt.Sprintf("%d", v)
	case string:
		id = v
	}
	if id == "" {
		return ""
	}
	return "https://studio.zebracat.ai/storyboard/" + id
}

// RelTime renders an RFC3339 timestamp as a friendly "2h ago" / "Jan 2".
func RelTime(s string) string {
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		if t, err = time.Parse(time.RFC3339Nano, s); err != nil {
			return ""
		}
	}
	d := time.Since(t)
	switch {
	case d < 0:
		return "just now"
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
	return t.Format("Jan 2")
}

func titleize(s string) string {
	parts := strings.Fields(s)
	for i, p := range parts {
		parts[i] = strings.ToUpper(p[:1]) + p[1:]
	}
	return strings.Join(parts, " ")
}

package dash

import (
	"strings"
	"testing"
	"time"
)

func TestHumanType(t *testing.T) {
	cases := map[string]string{
		"ai_video": "AI Video", "moving_ai_images": "Moving AI Images",
		"ai_avatar": "Avatar", "brainrot": "Brainrot", "": "Video",
		"some_new_type": "Some New Type",
	}
	for in, want := range cases {
		if got := HumanType(in); got != want {
			t.Errorf("HumanType(%q)=%q want %q", in, got, want)
		}
	}
}

func TestKindAndIcon(t *testing.T) {
	if Kind("completed") != "ok" || Icon("completed") != "✓" {
		t.Fatal("completed should be ok/✓")
	}
	if Kind("render_failed") != "failed" || Icon("render_failed") != "✗" {
		t.Fatal("render_failed should be failed/✗")
	}
	if Kind("processing") != "working" {
		t.Fatal("processing should be working")
	}
}

func TestStudioURL(t *testing.T) {
	if got := StudioURL(float64(1094257)); got != "https://studio.zebracat.ai/storyboard/1094257" {
		t.Fatalf("got %q", got)
	}
	if StudioURL("") != "" {
		t.Fatal("empty id -> empty url")
	}
}

func TestRelTime(t *testing.T) {
	now := time.Now().UTC().Format(time.RFC3339)
	if got := RelTime(now); got != "just now" {
		t.Fatalf("recent should be 'just now', got %q", got)
	}
	old := time.Now().Add(-3 * time.Hour).UTC().Format(time.RFC3339)
	if got := RelTime(old); !strings.Contains(got, "h ago") {
		t.Fatalf("3h ago expected 'Nh ago', got %q", got)
	}
	if RelTime("not-a-time") != "" {
		t.Fatal("bad input -> empty")
	}
}

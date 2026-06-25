package cmd

import "testing"

func resetVideoFlags() {
	vFrom, vPrompt, vScript, vURL, vAudioURL = "", "", "", "", ""
	vType, vLanguage, vVoice, vAspect, vMood, vPromptStyle = "", "", "", "", "", ""
	vDuration, vStyle = 0, 0
	vRender = false
}

func TestBuildCreateAgentic(t *testing.T) {
	resetVideoFlags()
	vFrom, vPrompt, vDuration, vType = "agentic", "a cat video", 30, "ai_video"
	path, payload, err := buildCreate()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != "/api/public/video/agentic" {
		t.Fatalf("path = %q", path)
	}
	if payload["prompt"] != "a cat video" || payload["video_type"] != "ai_video" || payload["duration"] != 30 {
		t.Fatalf("payload = %#v", payload)
	}
}

func TestBuildCreateIdeaRoutesPromptToIdea(t *testing.T) {
	resetVideoFlags()
	vFrom, vPrompt = "idea", "top productivity tips"
	path, payload, err := buildCreate()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if path != "/api/public/video/idea" || payload["idea"] != "top productivity tips" {
		t.Fatalf("path=%q payload=%#v", path, payload)
	}
}

func TestBuildCreateRequiresInput(t *testing.T) {
	resetVideoFlags()
	vFrom = "agentic"
	if _, _, err := buildCreate(); err == nil {
		t.Fatal("expected an error when --prompt is empty")
	}
}

func TestBuildCreateUnknownFrom(t *testing.T) {
	resetVideoFlags()
	vFrom, vPrompt = "bogus", "x"
	if _, _, err := buildCreate(); err == nil {
		t.Fatal("expected an error for an unknown --from value")
	}
}

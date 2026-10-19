package videos

import "testing"

func TestStopsWait(t *testing.T) {
	cases := []struct {
		m         map[string]any
		untilDone bool
		stop      bool
	}{
		{map[string]any{"phase": "planning"}, false, false},
		{map[string]any{"phase": "needs_input"}, true, true},
		{map[string]any{"phase": "plan_ready"}, false, true},
		{map[string]any{"phase": "plan_ready"}, true, false},
		{map[string]any{"phase": "plan_ready", "auto_generate": true}, false, false},
		{map[string]any{"phase": "generating"}, false, false},
		{map[string]any{"phase": "rendering"}, true, false},
		{map[string]any{"phase": "completed"}, true, true},
		{map[string]any{"phase": "failed", "status": "failed"}, false, true},
		{map[string]any{"phase": "cancelled"}, false, true},
		// v1 tasks have no phase: the status decides.
		{map[string]any{"status": "processing"}, false, false},
		{map[string]any{"status": "render_failed"}, false, true},
	}
	for _, c := range cases {
		if got := StopsWait(c.m, c.untilDone); got != c.stop {
			t.Errorf("StopsWait(%v, untilDone=%v) = %v", c.m, c.untilDone, got)
		}
	}
}

func TestQuoteLine(t *testing.T) {
	q := map[string]any{"pool": "api_dollar", "credits": float64(96), "usd": "4.80", "basis": "plan", "sufficient": false, "why": "4 scenes."}
	if got := QuoteLine(q); got != "96 credits ($4.80) from your API balance — not enough funds — 4 scenes." {
		t.Fatalf("%q", got)
	}
	q = map[string]any{"pool": "account", "credits": 12.5, "basis": "estimate", "usd": nil}
	if got := QuoteLine(q); got != "12.50 credits from your plan credits (estimate)" {
		t.Fatalf("%q", got)
	}
}

func TestEditorURL(t *testing.T) {
	if u := EditorURL(map[string]any{"links": map[string]any{"editor": "https://e/1"}, "project_id": float64(2)}); u != "https://e/1" {
		t.Fatal(u)
	}
	if u := EditorURL(map[string]any{"project_id": float64(2)}); u != "https://studio.zebracat.ai/storyboard/2" {
		t.Fatal(u)
	}
}

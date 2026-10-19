package tui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/zebracatai/zebracat-cli/internal/apitest"
	"github.com/zebracatai/zebracat-cli/internal/client"
	"github.com/zebracatai/zebracat-cli/internal/config"
)

// against builds a signed-in model talking to the fake API with sk-dummy.
func against(t *testing.T) (*model, *apitest.Server) {
	t.Helper()
	srv := apitest.New(t)
	t.Setenv("ZEBRACAT_API_KEY", "sk-dummy")
	cl := client.New(srv.URL, &config.Credentials{}, "")
	return &model{in: textinput.New(), cl: cl}, srv
}

func TestWizardBodyIsAPlanOnlyV2Request(t *testing.T) {
	body := wizBody(map[string]string{"idea": "a cat video", "media_type": "ai_image_video", "seconds": "60",
		"aspect": "square", "should_render": "no"})
	opts := body["options"].(map[string]any)
	if body["brief"] != "a cat video" || body["auto_generate"] != false || body["should_render"] != false ||
		opts["media_type"] != "ai_image_video" || opts["seconds"] != 60 || opts["aspect"] != "square" {
		t.Fatalf("body = %#v", body)
	}
	for _, c := range wizSteps[1].choices {
		if !client.CheckEnum("create.options.media_type", c.value) {
			t.Fatalf("wizard offers %q, which the API does not take", c.value)
		}
	}
}

// The whole plan-first flow: submit → plan_ready → quote → "Generate?" → yes →
// generate → completed.
func TestWizardPlanThenGenerate(t *testing.T) {
	m, srv := against(t)
	srv.On("GET", "/videos/{task_id}",
		apitest.Answer{Example: "planning"}, apitest.Answer{Example: "plan_ready"},
		apitest.Answer{Example: "generating"}, apitest.Answer{Example: "completed"})

	msg := m.createCmd(wizBody(map[string]string{"idea": "why cats knock things over", "seconds": "20"}))()
	post, _ := srv.Last("POST", "/videos")
	if post.Body["auto_generate"] != false || post.Header.Get("Idempotency-Key") == "" {
		t.Fatalf("create: %v %v", post.Body, post.Header)
	}
	m.Update(msg)
	if !m.busy {
		t.Fatal("busy while planning")
	}

	// planning → keep polling; plan_ready → fetch the quote.
	pm := m.poll("7c1e9b2a-4f0d-4c55-9a1e-3d2f6b8a9c01", false).(pollMsg)
	if _, cmd := m.Update(pm); cmd == nil {
		t.Fatal("planning should schedule another poll")
	}
	pm = m.poll(pm.taskID, false).(pollMsg)
	_, cmd := m.Update(pm)
	ready, ok := cmd().(planReadyMsg)
	if !ok || ready.quote["credits"] != float64(96) {
		t.Fatalf("plan_ready should fetch the quote, got %#v", ready)
	}
	_, printCmd := m.Update(ready)
	if m.gen == nil || m.busy {
		t.Fatal("a ready plan asks Generate?")
	}
	if card := planCard(ready.task, ready.quote); !strings.Contains(card, "96 credits ($4.80)") || !strings.Contains(card, "Why cats knock things off tables — 4 scenes, 20 s · ai_video · narration · english") {
		t.Fatalf("plan card:\n%s", card)
	}
	_ = printCmd
	if v := m.View(); !strings.Contains(v, "Generate this video?") || !strings.Contains(v, "96 credits ($4.80)") {
		t.Fatalf("view:\n%s", v)
	}
	if srv.Count("POST", "/generate") != 0 {
		t.Fatal("nothing is generated before the user says yes")
	}

	// Enter on "Yes".
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.gen != nil || !m.busy || cmd == nil {
		t.Fatal("yes should start Generate")
	}
	gm := m.generateCmd("7c1e9b2a-4f0d-4c55-9a1e-3d2f6b8a9c01", true)()
	if srv.Count("POST", "/generate") != 1 {
		t.Fatal("Generate was not posted")
	}
	m.Update(gm)
	pm = m.poll("7c1e9b2a-4f0d-4c55-9a1e-3d2f6b8a9c01", true).(pollMsg) // generating
	m.Update(pm)
	pm = m.poll(pm.taskID, true).(pollMsg) // completed
	m.Update(pm)
	if m.busy {
		t.Fatal("done should clear busy")
	}
}

func TestWizardKeepPlanGeneratesNothing(t *testing.T) {
	m, srv := against(t)
	task := map[string]any{"task_id": "t1", "phase": "plan_ready", "project_id": float64(5)}
	m.Update(planReadyMsg{task: task, quote: map[string]any{"credits": float64(10)}})
	m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.gen != nil || m.busy || srv.Count("POST", "/generate") != 0 {
		t.Fatal("'Not now' must keep the plan and spend nothing")
	}
}

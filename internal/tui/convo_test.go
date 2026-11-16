package tui

import (
	"reflect"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/zebracatai/zebracat-cli/internal/apitest"
	"github.com/zebracatai/zebracat-cli/internal/client"
	"github.com/zebracatai/zebracat-cli/internal/config"
)

const taskID = "7c1e9b2a-4f0d-4c55-9a1e-3d2f6b8a9c01"

// against builds a signed-in model talking to the fake API with sk-dummy.
func against(t *testing.T) (*model, *apitest.Server) {
	t.Helper()
	srv := apitest.New(t)
	t.Setenv("ZEBRACAT_API_KEY", "sk-dummy")
	cl := client.New(srv.URL, &config.Credentials{}, "")
	m := newModel(cl, srv.URL)
	m.width = 100
	return m, srv
}

// msgs runs a command and every command it batches or sequences, returning the
// messages they produce. Commands that sleep (poll ticks) are skipped.
func msgs(cmd tea.Cmd) []tea.Msg {
	if cmd == nil {
		return nil
	}
	ch := make(chan tea.Msg, 1)
	go func() { ch <- cmd() }()
	var msg tea.Msg
	select {
	case msg = <-ch:
	case <-time.After(2 * time.Second):
		return nil // a tea.Tick: the test polls by hand instead
	}
	if v := reflect.ValueOf(msg); v.IsValid() && v.Kind() == reflect.Slice && v.Type().Elem() == reflect.TypeOf(tea.Cmd(nil)) {
		var out []tea.Msg
		for i := 0; i < v.Len(); i++ {
			if c, ok := v.Index(i).Interface().(tea.Cmd); ok {
				out = append(out, msgs(c)...)
			}
		}
		return out
	}
	return []tea.Msg{msg}
}

// find returns the first message of type T.
func find[T any](t *testing.T, ms []tea.Msg) T {
	t.Helper()
	for _, m := range ms {
		if v, ok := m.(T); ok {
			return v
		}
	}
	var zero T
	t.Fatalf("no %T among %#v", zero, ms)
	return zero
}

// say types a message and presses Enter, returning what the commands produced.
func say(m *model, text string) []tea.Msg {
	m.in.SetValue(text)
	_, cmd := m.handleEnter()
	return msgs(cmd)
}

// toPlan drives an idea to its plan card (planning → plan_ready → plan + quote).
func toPlan(t *testing.T, m *model, srv *apitest.Server, idea string) {
	t.Helper()
	srv.On("GET", "/videos/{task_id}", apitest.Answer{Example: "planning"}, apitest.Answer{Example: "plan_ready"})
	created := find[createdMsg](t, say(m, idea))
	if m.stage != stagePlanning {
		t.Fatalf("stage after an idea = %v", m.stage)
	}
	_, cmd := m.Update(created)
	pm := find[pollMsg](t, msgs(cmd)) // planning
	if _, cmd = m.Update(pm); cmd == nil || m.stage != stagePlanning {
		t.Fatal("planning keeps polling")
	}
	if !strings.Contains(m.View(), "Writing the screenplay…") || !strings.Contains(m.View(), "esc to interrupt") {
		t.Fatalf("planning progress:\n%s", m.View())
	}
	pm = m.poll(taskID, false).(pollMsg) // plan_ready
	_, cmd = m.Update(pm)
	plan := find[planMsg](t, msgs(cmd))
	m.Update(plan)
	if m.stage != stageReady {
		t.Fatalf("stage after the plan = %v", m.stage)
	}
}

// The whole conversation: idea → plan card → a change in plain words → Ctrl+G →
// "n" (nothing spent) → Ctrl+G → "y" → generate → done → Ctrl+O opens it.
func TestConversationPlanEditConfirm(t *testing.T) {
	m, srv := against(t)
	srv.On("GET", "/videos/{task_id}/quote", apitest.Answer{Status: "200", Raw: `{"pool": "api_dollar", "credits": 96,
		"usd": "4.80", "basis": "plan", "available": 25, "sufficient": true, "why": "4 AI-video scenes of 5 s at balanced quality."}`})
	toPlan(t, m, srv, "why cats knock things off tables")

	post, _ := srv.Last("POST", "/videos")
	if post.Body["brief"] != "why cats knock things off tables" || post.Body["input_kind"] != "idea" ||
		post.Body["auto_generate"] != false || post.Header.Get("Idempotency-Key") == "" || post.Body["source_video"] != nil {
		t.Fatalf("create body %v headers %v", post.Body, post.Header)
	}
	card := planCard(m.job, 80)
	for _, want := range []string{"Why cats knock things off tables", "20 s · 4 scenes · vertical · ai video · english · narration",
		"Ever wondered why your cat swipes", "96 credits ($4.80)", "Ctrl+G"} {
		if !strings.Contains(card, want) {
			t.Fatalf("plan card misses %q:\n%s", want, card)
		}
	}
	if srv.Count("POST", "/generate") != 0 {
		t.Fatal("a plan never generates by itself")
	}

	// A change in plain words goes to the plan chat.
	ms := say(m, "make it funnier")
	if m.stage != stageEditing {
		t.Fatalf("stage while changing = %v", m.stage)
	}
	chat, _ := srv.Last("POST", "/plan/chat")
	if chat.Body["message"] != "make it funnier" {
		t.Fatalf("plan chat body %v", chat.Body)
	}
	pmsg := find[planMsg](t, ms)
	if pmsg.reply != "Done: tightened to 15 s and punched up the jokes." || pmsg.quote["credits"] != float64(96) {
		t.Fatalf("plan chat answer %#v", pmsg)
	}
	m.Update(pmsg)
	if m.stage != stageReady {
		t.Fatalf("stage after a change = %v", m.stage)
	}

	// Ctrl+G asks; n keeps the plan.
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
	if m.stage != stageConfirm || !strings.Contains(m.View(), "Generate it for 96 credits ($4.80) from your API balance?") {
		t.Fatalf("confirm view:\n%s", m.View())
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("x")}) // any other key does nothing
	m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if m.stage != stageConfirm || srv.Count("POST", "/generate") != 0 {
		t.Fatal("only y or n answers the spend question")
	}
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if m.stage != stageReady || srv.Count("POST", "/generate") != 0 {
		t.Fatal("n must keep the plan and spend nothing")
	}

	// Ctrl+G, y → Generate, once, with an idempotency key.
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	gen := find[generatedMsg](t, msgs(cmd))
	if c, _ := srv.Last("POST", "/generate"); srv.Count("POST", "/generate") != 1 || c.Header.Get("Idempotency-Key") == "" {
		t.Fatalf("generate calls %d", srv.Count("POST", "/generate"))
	}
	srv.On("GET", "/videos/{task_id}", apitest.Answer{Example: "generating"}, apitest.Answer{Example: "completed"})
	_, cmd = m.Update(gen)
	if m.stage != stageMaking {
		t.Fatalf("stage after Generate = %v", m.stage)
	}
	pm := find[pollMsg](t, msgs(cmd)) // generating
	m.Update(pm)
	if !strings.Contains(m.View(), "Making the scenes") {
		t.Fatalf("progress line:\n%s", m.View())
	}
	m.Update(m.poll(taskID, true)) // completed
	if m.stage != stageDone || m.job.videoURL != "https://cdn.zebracat.ai/videos/48213/final.mp4" {
		t.Fatalf("done: stage %v job %+v", m.stage, m.job)
	}
	if !strings.Contains(doneCard(m.job, time.Minute), "final.mp4") {
		t.Fatal("the done card shows the video link")
	}

	var opened string
	old := openURL
	openURL = func(u string) error { opened = u; return nil }
	defer func() { openURL = old }()
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlO})
	if opened != "https://cdn.zebracat.ai/videos/48213/final.mp4" {
		t.Fatalf("Ctrl+O opened %q", opened)
	}
	if srv.Count("POST", "/generate") != 1 || srv.Count("POST", "/cancel") != 0 {
		t.Fatal("exactly one Generate, no cancel")
	}
}

// A pasted video post is the run's source_video, sent with the words, as the
// studio's /chat does; a lone page link is the URL door.
func TestLinksAreFirstClass(t *testing.T) {
	m, srv := against(t)
	find[createdMsg](t, say(m, "make one like this https://www.tiktok.com/@someone/video/7300000000000000000 but about tea"))
	post, _ := srv.Last("POST", "/videos")
	if post.Body["source_video"] != "https://www.tiktok.com/@someone/video/7300000000000000000" ||
		post.Body["input_kind"] != "idea" || post.Body["auto_generate"] != false ||
		!strings.Contains(post.Body["brief"].(string), "but about tea") {
		t.Fatalf("video link body %v", post.Body)
	}

	m, srv = against(t)
	find[createdMsg](t, say(m, "https://example.com/blog/how-we-roast"))
	post, _ = srv.Last("POST", "/videos")
	if post.Body["input_kind"] != "url" || post.Body["brief"] != "https://example.com/blog/how-we-roast" || post.Body["source_video"] != nil {
		t.Fatalf("page link body %v", post.Body)
	}
}

// The plan's question is answered by typing (or by the option's number).
func TestNeedsInputIsAnsweredByTyping(t *testing.T) {
	m, srv := against(t)
	srv.On("GET", "/videos/{task_id}", apitest.Answer{Example: "needs_input"})
	created := find[createdMsg](t, say(m, "a video about tea"))
	_, cmd := m.Update(created)
	m.Update(find[pollMsg](t, msgs(cmd)))
	if m.stage != stageAsking || len(m.job.options) != 2 {
		t.Fatalf("stage %v options %v", m.stage, m.job.options)
	}
	find[answeredMsg](t, say(m, "2"))
	ans, _ := srv.Last("POST", "/answer")
	if ans.Body["question_id"] != "q_audio" || ans.Body["answer"] != "People talking" {
		t.Fatalf("answer body %v", ans.Body)
	}
	if srv.Count("POST", "/videos") != 1 {
		t.Fatal("an answer never starts a second video")
	}
}

// Typing while a plan is being written does not start a second one.
func TestNoSecondPlanWhileBusy(t *testing.T) {
	m, srv := against(t)
	find[createdMsg](t, say(m, "a video about tea"))
	say(m, "and coffee")
	if srv.Count("POST", "/videos") != 1 || srv.Count("POST", "/plan/chat") != 0 {
		t.Fatal("a message while planning must wait")
	}
}

// Ctrl+C while a paid video is being made never cancels it and asks twice;
// with a plan only, it leaves at once.
func TestCtrlCNeverCancelsAPaidJob(t *testing.T) {
	m, srv := against(t)
	m.job = &job{taskID: taskID, started: time.Now(), phase: "generating"}
	m.stage = stageMaking
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	for _, x := range msgs(cmd) {
		if _, quit := x.(tea.QuitMsg); quit {
			t.Fatal("the first Ctrl+C during a paid job must not quit")
		}
	}
	if !m.leaveArmed {
		t.Fatal("the first Ctrl+C arms leaving")
	}
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	find[tea.QuitMsg](t, msgs(cmd))
	if srv.Count("POST", "/cancel") != 0 {
		t.Fatal("Ctrl+C must never cancel")
	}

	m, _ = against(t)
	m.job = &job{taskID: taskID}
	m.stage = stageReady
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	if hasQuit(msgs(cmd)) || !strings.Contains(m.View(), "Press Ctrl-C again to exit") {
		t.Fatalf("the first Ctrl-C only warns:\n%s", m.View())
	}
	_, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	find[tea.QuitMsg](t, msgs(cmd))
}

func hasQuit(ms []tea.Msg) bool {
	for _, x := range ms {
		if _, ok := x.(tea.QuitMsg); ok {
			return true
		}
	}
	return false
}

// Ctrl+C on the spend question leaves without generating.
func TestCtrlCOnTheSpendQuestion(t *testing.T) {
	m, srv := against(t)
	m.job = &job{taskID: taskID, quote: map[string]any{"credits": float64(10)}}
	m.stage = stageConfirm
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	find[tea.QuitMsg](t, msgs(cmd))
	if srv.Count("POST", "/generate") != 0 {
		t.Fatal("Ctrl+C is not a yes")
	}
}

// A quote the balance doesn't cover never reaches the spend question.
func TestShortBalanceNeverAsksToSpend(t *testing.T) {
	m, srv := against(t)
	m.job = &job{taskID: taskID, quote: map[string]any{"credits": float64(96), "sufficient": false}}
	m.stage = stageReady
	m.Update(tea.KeyMsg{Type: tea.KeyCtrlG})
	m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("y")})
	if m.stage != stageReady || srv.Count("POST", "/generate") != 0 {
		t.Fatalf("stage %v generate %d", m.stage, srv.Count("POST", "/generate"))
	}
}

// Text from the server or the plan model can't drive the terminal.
func TestServerTextCannotEscapeTheTerminal(t *testing.T) {
	evil := "Done\x1b]0;pwned\x07 \x1b[2J\u009bok\nnext"
	if got := clean(evil); strings.ContainsAny(got, "\x1b\x07\u009b") || !strings.Contains(got, "\nnext") {
		t.Fatalf("clean = %q", got)
	}
	j := &job{plan: map[string]any{"screenplay": map[string]any{"title": "T\x1b[31m", "beats": []any{
		map[string]any{"n": float64(1), "seconds": float64(5), "narration": "hi\x1b]52;c;Zm9v\x07"}}}}}
	if card := planCard(j, 80); strings.Contains(card, "\x1b]") || strings.Contains(card, "\x07") {
		t.Fatalf("the plan card carries a raw escape: %q", card)
	}
	q := questionCard(map[string]any{"question": map[string]any{"id": "q", "question": "Pick\x1b[2J", "options": []any{"a\x1b[1A"}}})
	if strings.Contains(q, "\x1b[2J") || strings.Contains(q, "\x1b[1A") {
		t.Fatalf("question card: %q", q)
	}
}

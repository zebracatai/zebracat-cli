package clierr

import (
	"encoding/json"
	"testing"
)

func TestExitCodes(t *testing.T) {
	cases := []struct {
		err  *Error
		exit int
		code string
	}{
		{API("boom"), CodeAPI, "api_error"},
		{Usage("nope"), CodeUsage, "usage_error"},
		{Auth("who"), CodeAuth, "auth_error"},
		{Timeout("slow"), CodeTimeout, "timeout"},
	}
	for _, c := range cases {
		if c.err.Exit != c.exit {
			t.Errorf("%s: exit = %d, want %d", c.code, c.err.Exit, c.exit)
		}
		if c.err.Code != c.code {
			t.Errorf("code = %q, want %q", c.err.Code, c.code)
		}
		if c.err.Error() == "" {
			t.Errorf("empty message")
		}
	}
}

func TestEnvelopeShape(t *testing.T) {
	e := &Error{Exit: CodeAPI, Code: "insufficient_funds", Message: "m", Hint: "h", Type: "insufficient_funds", RequestID: "r"}
	b, _ := json.Marshal(e.Envelope())
	want := `{"error":{"code":"insufficient_funds","message":"m","hint":"h","type":"insufficient_funds","retryable":false,"request_id":"r"}}`
	if string(b) != want {
		t.Fatalf("envelope = %s", b)
	}
	b, _ = json.Marshal(Usage("x").Envelope())
	if string(b) != `{"error":{"code":"usage_error","message":"x","type":"invalid_request","retryable":false}}` {
		t.Fatalf("plain envelope = %s", b)
	}
}

func TestExitForType(t *testing.T) {
	for typ, want := range map[string]int{
		"invalid_request": CodeUsage, "authentication": CodeAuth, "permission": CodeAuth,
		"insufficient_funds": CodeAPI, "rate_limited": CodeAPI, "engine": CodeAPI, "not_found": CodeAPI,
	} {
		if got := ExitForType(typ); got != want {
			t.Fatalf("%s → %d, want %d", typ, got, want)
		}
	}
}

// Every envelope has code, message, type and retryable, even for an error
// built by hand, and the server's extra fields pass through.
func TestEnvelopeAlwaysHasTheFourKeys(t *testing.T) {
	for name, e := range map[string]*Error{
		"api":     API("boom"),
		"network": Network("dial tcp: refused"),
		"usage":   Usage("bad"),
		"auth":    Auth("who"),
		"timeout": Timeout("slow"),
		"by hand": {Exit: CodeAuth, Code: "x", Message: "m"},
	} {
		b, _ := json.Marshal(e.Envelope())
		var got struct {
			Error map[string]any `json:"error"`
		}
		_ = json.Unmarshal(b, &got)
		for _, k := range []string{"code", "message", "type", "retryable"} {
			if _, ok := got.Error[k]; !ok {
				t.Errorf("%s: no %q in %s", name, k, b)
			}
		}
		if got.Error["type"] == "" {
			t.Errorf("%s: empty type in %s", name, b)
		}
	}
	if !Network("x").Retryable || !Timeout("x").Retryable || Usage("x").Retryable {
		t.Fatal("network and timeout are retryable, usage is not")
	}
	e := &Error{Code: "invalid_choice", Message: "m", Type: "invalid_request", Field: "mood",
		AllowedValues: []any{"happy", "sad"}, DocsURL: "https://api.zebracat.ai/docs#invalid_choice",
		Errors: []any{map[string]any{"field": "mood"}}}
	b, _ := json.Marshal(e.Envelope())
	want := `{"error":{"code":"invalid_choice","message":"m","type":"invalid_request","retryable":false,"field":"mood","allowed_values":["happy","sad"],"docs_url":"https://api.zebracat.ai/docs#invalid_choice","errors":[{"field":"mood"}]}}`
	if string(b) != want {
		t.Fatalf("envelope = %s", b)
	}
}

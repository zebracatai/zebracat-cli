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
	want := `{"error":{"code":"insufficient_funds","message":"m","hint":"h","type":"insufficient_funds","request_id":"r"}}`
	if string(b) != want {
		t.Fatalf("envelope = %s", b)
	}
	b, _ = json.Marshal(Usage("x").Envelope())
	if string(b) != `{"error":{"code":"usage_error","message":"x"}}` {
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

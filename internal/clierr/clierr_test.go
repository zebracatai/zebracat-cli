package clierr

import "testing"

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

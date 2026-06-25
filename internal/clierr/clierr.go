// Package clierr defines the CLI's structured error envelope and stable exit codes.
//
// Errors are printed to stderr as JSON: {"error":{"code","message","hint"}} so
// agents and scripts can parse failures, while exit codes give a quick signal:
//
//	0 success · 1 API/network · 2 usage · 3 auth · 4 timeout
package clierr

import "fmt"

// Exit codes.
const (
	CodeOK      = 0
	CodeAPI     = 1
	CodeUsage   = 2
	CodeAuth    = 3
	CodeTimeout = 4
)

// Error is a CLI error carrying an exit code, a machine code, and an optional hint.
type Error struct {
	Exit    int
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}

func (e *Error) Error() string { return e.Message }

func newErr(exit int, code, msg, hint string) *Error {
	return &Error{Exit: exit, Code: code, Message: msg, Hint: hint}
}

// API returns a generic API/network error (exit 1).
func API(msg string, args ...any) *Error {
	return newErr(CodeAPI, "api_error", fmt.Sprintf(msg, args...), "")
}

// APIHint is like API but with a remediation hint.
func APIHint(hint, msg string, args ...any) *Error {
	return newErr(CodeAPI, "api_error", fmt.Sprintf(msg, args...), hint)
}

// Usage returns a usage error (exit 2).
func Usage(msg string, args ...any) *Error {
	return newErr(CodeUsage, "usage_error", fmt.Sprintf(msg, args...), "")
}

// Auth returns an authentication error (exit 3).
func Auth(msg string, args ...any) *Error {
	return newErr(CodeAuth, "auth_error", fmt.Sprintf(msg, args...), "Run `zebracat auth login` or set ZEBRACAT_API_KEY.")
}

// Timeout returns a timeout error (exit 4).
func Timeout(msg string, args ...any) *Error {
	return newErr(CodeTimeout, "timeout", fmt.Sprintf(msg, args...), "")
}

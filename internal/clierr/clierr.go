// Package clierr defines the CLI's structured error envelope and stable exit codes.
//
// Errors are printed to stderr as JSON: {"error":{"code","message","hint"}} so
// agents and scripts can parse failures, while exit codes give a quick signal:
//
//	0 success · 1 API/network · 2 usage · 3 auth · 4 timeout
//
// A typed API error maps by its type: invalid_request → 2, authentication and
// permission → 3, everything else (insufficient_funds, rate_limited, engine…) → 1.
package clierr

import (
	"fmt"
	"time"
)

// Exit codes.
const (
	CodeOK      = 0
	CodeAPI     = 1
	CodeUsage   = 2
	CodeAuth    = 3
	CodeTimeout = 4
)

// Error is a CLI error carrying an exit code, a machine code, and an optional hint.
// API failures also carry the server's typed fields (type, retryable, request_id).
type Error struct {
	Exit      int    `json:"-"`
	Code      string `json:"code"`
	Message   string `json:"message"`
	Hint      string `json:"hint,omitempty"`
	Type      string `json:"type,omitempty"`
	Retryable bool   `json:"retryable,omitempty"`
	RequestID string `json:"request_id,omitempty"`
	// Status is the HTTP status (0 for local errors); RetryAfter is the server's
	// Retry-After on a 429. Neither is printed in the envelope.
	Status     int           `json:"-"`
	RetryAfter time.Duration `json:"-"`
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

// ExitForType is the exit code for a typed API error.
func ExitForType(typ string) int {
	switch typ {
	case "invalid_request":
		return CodeUsage
	case "authentication", "permission":
		return CodeAuth
	default:
		return CodeAPI
	}
}

// Envelope is the stderr JSON body: {"error":{code,message,hint,type,retryable,request_id}}.
func (e *Error) Envelope() map[string]any {
	return map[string]any{"error": e}
}

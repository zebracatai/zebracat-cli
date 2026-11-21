// Package clierr defines the CLI's structured error envelope and stable exit codes.
//
// Errors are printed to stderr as JSON so agents and scripts can parse them:
//
//	{"error":{"code","message","type","retryable", "hint"?, "request_id"?,
//	          "field"?, "allowed_values"?, "docs_url"?, "errors"?}}
//
// code, message, type and retryable are always there; the rest only when known
// (the server's own fields pass through unchanged). Exit codes give a quick signal:
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

// Error types the CLI sets itself. A typed API error keeps the server's type
// (invalid_request, authentication, permission, insufficient_funds,
// rate_limited, conflict, not_found, engine, unavailable).
const (
	TypeInvalidRequest = "invalid_request" // a usage error: fix the command
	TypeAuthentication = "authentication"  // no key, or the key was rejected
	TypePermission     = "permission"      // the key works but may not do this
	TypeUnavailable    = "unavailable"     // network, gateway or 5xx: try again
	TypeTimeout        = "timeout"         // the CLI stopped waiting
	TypeAPI            = "api_error"       // any other failure
)

// Error is a CLI error carrying an exit code, a machine code, and an optional hint.
// API failures also carry the server's typed fields.
type Error struct {
	Exit          int    `json:"-"`
	Code          string `json:"code"`
	Message       string `json:"message"`
	Hint          string `json:"hint,omitempty"`
	Type          string `json:"type"`
	Retryable     bool   `json:"retryable"`
	RequestID     string `json:"request_id,omitempty"`
	Field         string `json:"field,omitempty"`
	AllowedValues []any  `json:"allowed_values,omitempty"`
	DocsURL       string `json:"docs_url,omitempty"`
	Errors        []any  `json:"errors,omitempty"`
	// Status is the HTTP status (0 for local errors); RetryAfter is the server's
	// Retry-After on a 429. Neither is printed in the envelope.
	Status     int           `json:"-"`
	RetryAfter time.Duration `json:"-"`
}

func (e *Error) Error() string { return e.Message }

func newErr(exit int, code, typ, msg, hint string) *Error {
	return &Error{Exit: exit, Code: code, Type: typ, Message: msg, Hint: hint}
}

// API returns a generic API/network error (exit 1).
func API(msg string, args ...any) *Error {
	return newErr(CodeAPI, "api_error", TypeAPI, fmt.Sprintf(msg, args...), "")
}

// APIHint is like API but with a remediation hint.
func APIHint(hint, msg string, args ...any) *Error {
	return newErr(CodeAPI, "api_error", TypeAPI, fmt.Sprintf(msg, args...), hint)
}

// Network is a request that never got an answer (exit 1, retryable).
func Network(msg string, args ...any) *Error {
	e := newErr(CodeAPI, "network_error", TypeUnavailable, fmt.Sprintf(msg, args...), "Check your connection and try again.")
	e.Retryable = true
	return e
}

// Usage returns a usage error (exit 2).
func Usage(msg string, args ...any) *Error {
	return newErr(CodeUsage, "usage_error", TypeInvalidRequest, fmt.Sprintf(msg, args...), "")
}

// Auth returns an authentication error (exit 3).
func Auth(msg string, args ...any) *Error {
	return newErr(CodeAuth, "auth_error", TypeAuthentication, fmt.Sprintf(msg, args...), "Run `zebracat auth login` or set ZEBRACAT_API_KEY.")
}

// Timeout returns a timeout error (exit 4). The request may still have
// reached the server, so a retry must reuse its idempotency key.
func Timeout(msg string, args ...any) *Error {
	e := newErr(CodeTimeout, "timeout", TypeTimeout, fmt.Sprintf(msg, args...), "")
	e.Retryable = true
	return e
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

// Envelope is the stderr body: {"error":{code,message,type,retryable,…}}.
// An error built by hand without a type gets the one its exit code implies,
// so type is never empty.
func (e *Error) Envelope() map[string]any {
	if e.Type == "" {
		switch e.Exit {
		case CodeUsage:
			e.Type = TypeInvalidRequest
		case CodeAuth:
			e.Type = TypeAuthentication
		case CodeTimeout:
			e.Type = TypeTimeout
		default:
			e.Type = TypeAPI
		}
	}
	return map[string]any{"error": e}
}

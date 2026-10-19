package client

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestTypedErrorKeepsCodeHintAndRequestID(t *testing.T) {
	raw := []byte(`{"error":"Paid steps on an API key open soon.","code":"api_key_billing_pending","type":"insufficient_funds","retryable":false,"hint":"Use --plan-only and quote now.","request_id":"req_1"}`)
	e := apiError(402, raw, http.Header{})
	if e.Exit != 1 || e.Code != "api_key_billing_pending" || e.Hint != "Use --plan-only and quote now." || e.RequestID != "req_1" {
		t.Fatalf("%+v", e)
	}
}

func TestUntypedErrorsKeepTheirOldExitCodes(t *testing.T) {
	if e := apiError(402, []byte(`{"error":"Insufficient API balance"}`), http.Header{}); e.Exit != 1 || e.Hint == "" {
		t.Fatalf("402: %+v", e)
	}
	if e := apiError(401, []byte(`{"detail":"bad key"}`), http.Header{}); e.Exit != 3 {
		t.Fatalf("401: %+v", e)
	}
	e := apiError(400, []byte(`{"error":"mood is invalid","code":"invalid_choice","hint":"did you mean happy?"}`), http.Header{"X-Request-Id": {"rq"}})
	if e.Exit != 1 || e.Code != "invalid_choice" || e.Hint != "did you mean happy?" || e.RequestID != "rq" {
		t.Fatalf("400: %+v", e)
	}
}

func TestRetryAfterFromHeaderOrBody(t *testing.T) {
	e := apiError(429, []byte(`{"error":"slow"}`), http.Header{"Retry-After": {"12"}})
	if e.RetryAfter != 12*time.Second || !e.Retryable || e.Status != 429 {
		t.Fatalf("header: %+v", e)
	}
	e = apiError(429, []byte(`{"error":"allowance","code":"studio_allowance","type":"rate_limited","retryable":true,"retry_after_s":600}`), http.Header{})
	if e.RetryAfter != 10*time.Minute {
		t.Fatalf("body: %+v", e)
	}
}

func TestHTMLNotFoundNamesAMissingEndpoint(t *testing.T) {
	e := apiError(404, []byte("<!DOCTYPE html><title>Page not found</title>"), http.Header{})
	if e.Exit != 1 || e.Status != 404 || !strings.Contains(e.Message, "no such endpoint") {
		t.Fatalf("%+v", e)
	}
	if e := apiError(502, []byte("<html>bad gateway</html>"), http.Header{}); !strings.Contains(e.Message, "check your base URL") {
		t.Fatalf("%+v", e)
	}
}

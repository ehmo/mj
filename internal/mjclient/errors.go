package mjclient

import (
	"fmt"
	"regexp"
)

// APIError is returned for non-2xx responses from www.midjourney.com/api/*.
type APIError struct {
	Status int
	Path   string
	Body   string // truncated, token-redacted preview
}

func (e *APIError) Error() string {
	return fmt.Sprintf("midjourney api %s -> %d: %s", e.Path, e.Status, e.Body)
}

// Code classifies the error for callers (CLI exit codes / MCP tool errors).
func (e *APIError) Code() string {
	switch {
	case e.Status == 401:
		return "auth"
	case e.Status == 403:
		return "cloudflare"
	case e.Status == 429:
		return "rate_limited"
	case e.Status >= 500:
		return "server"
	default:
		return "api"
	}
}

// Retriable reports whether retrying the request might succeed.
func (e *APIError) Retriable() bool {
	return e.Status == 429 || e.Status >= 500
}

// AuthError indicates the credential chain could not establish a session.
type AuthError struct{ Reason string }

func (e *AuthError) Error() string { return "auth: " + e.Reason }

// tokenRE matches JWTs and long opaque token-like strings so they never appear
// in error messages or logs.
var tokenRE = regexp.MustCompile(`eyJ[A-Za-z0-9_\-]{8,}\.[A-Za-z0-9_\-]+\.[A-Za-z0-9_\-]+|[A-Za-z0-9_\-]{40,}`)

// Redact replaces token-shaped substrings with <redacted>.
func Redact(s string) string { return tokenRE.ReplaceAllString(s, "<redacted>") }

func preview(b []byte) string {
	const max = 300
	s := Redact(string(b))
	if len(s) > max {
		return s[:max] + "…"
	}
	return s
}

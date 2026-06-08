package mjclient

import "testing"

func TestRedact(t *testing.T) {
	cases := []struct{ in, mustNotContain string }{
		{`{"websocketToken":"eyJhbGciOiJIUzI1NiJ9.eyJ1c2VyIjoieCJ9.abcDEFghiJKL"}`, "eyJ"},
		{`{"refreshToken":"AMf-vBxQ1234567890abcdefghijklmnopqrstuvwxyz0123456789ABCD"}`, "AMf-vBxQ1234567890abcdefghijklmnopqrstuvwxyz0123456789ABCD"},
		{`{"idToken":"eyJ0eXAiOiJKV1QiLCJhbGciOiJSUzI1NiJ9.payloadpart.signaturepart"}`, "payloadpart"},
	}
	for _, c := range cases {
		got := Redact(c.in)
		if contains(got, c.mustNotContain) {
			t.Errorf("Redact(%q) leaked %q -> %q", c.in, c.mustNotContain, got)
		}
		if !contains(got, "<redacted>") {
			t.Errorf("Redact(%q) did not redact -> %q", c.in, got)
		}
	}
	// short, non-token text is preserved
	if Redact(`{"error":"Unknown submit type"}`) != `{"error":"Unknown submit type"}` {
		t.Errorf("Redact altered safe text")
	}
}

func TestAPIErrorCode(t *testing.T) {
	cases := []struct {
		status int
		code   string
		retri  bool
	}{
		{401, "auth", false},
		{403, "cloudflare", false},
		{429, "rate_limited", true},
		{500, "server", true},
		{400, "api", false},
	}
	for _, c := range cases {
		e := &APIError{Status: c.status}
		if e.Code() != c.code {
			t.Errorf("status %d code = %q, want %q", c.status, e.Code(), c.code)
		}
		if e.Retriable() != c.retri {
			t.Errorf("status %d retriable = %v, want %v", c.status, e.Retriable(), c.retri)
		}
	}
}

func contains(s, sub string) bool {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func TestURLExtAndStripQuery(t *testing.T) {
	cases := map[string]string{
		"https://cdn.midjourney.com/u/p/img.jpeg?v=1": ".jpeg",
		"https://cdn.midjourney.com/id/0_0.webp":      ".webp",
		"https://cdn.midjourney.com/id/0_0.png":       ".png",
		"https://cdn.midjourney.com/video/id/0.mp4":   ".mp4",
	}
	for u, want := range cases {
		if got := urlExt(u); got != want {
			t.Errorf("urlExt(%q)=%q want %q", u, got, want)
		}
	}
}

func TestRetryableStatus(t *testing.T) {
	for _, s := range []int{0, 404, 429, 500, 503} {
		if !retryableStatus(s) {
			t.Errorf("status %d should be retryable", s)
		}
	}
	for _, s := range []int{403, 410, 400, 200} {
		if retryableStatus(s) {
			t.Errorf("status %d should NOT be retryable", s)
		}
	}
}

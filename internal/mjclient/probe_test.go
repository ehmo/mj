package mjclient

import (
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"
)

// TestLiveProbe GETs candidate read endpoints (and search-param variants) and
// dumps status + a body sample to capture/probe-shapes.txt. MJ_LIVE=1 MJ_PROBE=1.
func TestLiveProbe(t *testing.T) {
	if os.Getenv("MJ_LIVE") != "1" || os.Getenv("MJ_PROBE") != "1" {
		t.Skip("set MJ_LIVE=1 MJ_PROBE=1")
	}
	profile := os.Getenv("MJ_PROFILE")
	if profile == "" {
		profile = "/Users/nan/Work/ai/midjourney/capture/profile"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	c, err := New(ctx, Config{ProfileDir: profile, Headless: true})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()
	if err := c.EnsureSession(ctx); err != nil {
		t.Fatalf("auth: %v", err)
	}
	acct, _ := c.Account(ctx)

	paths := []string{
		"/api/user-account",
		"/api/user-queue",
		"/api/folders",
		"/api/moodboards",
		"/api/personalized-profiles",
		"/api/model-ratings",
		"/api/following-for-user",
		"/api/spotlight-feed?username_v2=" + acct.DisplayName,
		"/api/explore?feed=top&page=0",
		// search-param candidates — compare which one filters results to "cat"
		"/api/explore?feed=top&page=0&searchText=cat",
		"/api/explore?feed=top&page=0&prompt=cat",
		"/api/explore?feed=top&page=0&q=cat",
		"/api/explore?feed=top&page=0&search=cat",
	}
	var b strings.Builder
	for _, p := range paths {
		body, err := c.Get(ctx, p)
		if err != nil {
			fmt.Fprintf(&b, "\n### GET %s\nERROR: %v\n", p, err)
			continue
		}
		s := string(body)
		if len(s) > 700 {
			s = s[:700]
		}
		fmt.Fprintf(&b, "\n### GET %s  (%d bytes)\n%s\n", p, len(body), s)
	}
	out := "/Users/nan/Work/ai/midjourney/capture/probe-shapes.txt"
	_ = os.WriteFile(out, []byte(b.String()), 0o644)
	t.Logf("wrote %s", out)
}

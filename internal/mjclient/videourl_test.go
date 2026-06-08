package mjclient

import (
	"context"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	gomoufox "github.com/ehmo/gomoufox"
	"github.com/ehmo/gomoufox/camoufoxcfg"
)

// TestLiveVideoURL opens a completed video job's page and records any media /
// .mp4 requests to find the real video URL. MJ_LIVE=1 MJ_VIDURL=1.
func TestLiveVideoURL(t *testing.T) {
	if os.Getenv("MJ_LIVE") != "1" || os.Getenv("MJ_VIDURL") != "1" {
		t.Skip("set MJ_LIVE=1 MJ_VIDURL=1")
	}
	job := os.Getenv("MJ_VIDJOB")
	if job == "" {
		job = "e4252c82-b300-4f63-8d98-0e8d4eaf9e1c"
	}
	profile := os.Getenv("MJ_PROFILE")
	if profile == "" {
		profile = "/Users/nan/Work/ai/midjourney/capture/profile"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	br, err := gomoufox.New(ctx,
		gomoufox.WithHeadless(camoufoxcfg.HeadlessTrue),
		gomoufox.WithPersistentContext(profile),
		gomoufox.WithUnsafeDirectNetwork(true),
		gomoufox.WithConnectTimeout(90*time.Second))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer br.Close()
	page, err := br.NewPage(ctx)
	if err != nil {
		t.Fatalf("NewPage: %v", err)
	}
	var mu sync.Mutex
	var hits []string
	page.OnRequest(func(r *gomoufox.Request) {
		defer func() { _ = recover() }()
		u := r.URL()
		if strings.Contains(u, ".mp4") || strings.Contains(u, "video") || strings.Contains(u, ".webm") {
			mu.Lock()
			hits = append(hits, r.Method()+" "+u)
			mu.Unlock()
		}
	})
	for _, rt := range []string{"/jobs/" + job + "?index=2", "/jobs/" + job, "/jobs/" + job + "?index=0"} {
		_, _ = page.Goto(ctx, "https://www.midjourney.com"+rt,
			gomoufox.WaitUntil("domcontentloaded"), gomoufox.WithTimeout(40*time.Second))
		time.Sleep(6 * time.Second)
	}
	// also read any <video>/<source> src in the DOM
	var srcs []string
	_ = page.EvaluateIntoJSON(ctx, `() => Array.from(document.querySelectorAll('video,source,a')).map(e=>e.src||e.href||'').filter(u=>u && (u.includes('.mp4')||u.includes('video')))`, &srcs)
	mu.Lock()
	defer mu.Unlock()
	t.Logf("=== media requests ===")
	for _, h := range dedup(hits) {
		t.Logf("  %s", h)
	}
	t.Logf("=== DOM video srcs ===")
	for _, s := range dedup(srcs) {
		t.Logf("  %s", s)
	}
}

func dedup(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

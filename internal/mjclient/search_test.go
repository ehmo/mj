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

// TestLiveSearchCapture types into the explore search box and records the
// resulting /api/* request URLs, to discover the search endpoint/params.
// MJ_LIVE=1 MJ_SEARCH=1.
func TestLiveSearchCapture(t *testing.T) {
	if os.Getenv("MJ_LIVE") != "1" || os.Getenv("MJ_SEARCH") != "1" {
		t.Skip("set MJ_LIVE=1 MJ_SEARCH=1")
	}
	profile := os.Getenv("MJ_PROFILE")
	if profile == "" {
		profile = "/Users/nan/Work/ai/midjourney/capture/profile"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	br, err := gomoufox.New(ctx,
		gomoufox.WithHeadless(camoufoxcfg.HeadlessTrue),
		gomoufox.WithPersistentContext(profile),
		gomoufox.WithUnsafeDirectNetwork(true),
		gomoufox.WithConnectTimeout(90*time.Second),
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer br.Close()
	page, err := br.NewPage(ctx)
	if err != nil {
		t.Fatalf("NewPage: %v", err)
	}

	var mu sync.Mutex
	var urls []string
	page.OnRequest(func(r *gomoufox.Request) {
		defer func() { _ = recover() }()
		u := r.URL()
		if strings.Contains(u, "midjourney.com/api/") {
			mu.Lock()
			urls = append(urls, u)
			mu.Unlock()
		}
	})

	if _, err := page.Goto(ctx, "https://www.midjourney.com/explore?tab=top",
		gomoufox.WaitUntil("domcontentloaded"), gomoufox.WithTimeout(40*time.Second)); err != nil {
		t.Fatalf("goto explore: %v", err)
	}
	time.Sleep(5 * time.Second)

	// enumerate inputs to find the search box
	var inputs []map[string]any
	_ = page.EvaluateIntoJSON(ctx, `() => Array.from(document.querySelectorAll('input,textarea')).map(e => ({type:e.type||'', ph:e.placeholder||'', al:e.getAttribute('aria-label')||'', id:e.id||''}))`, &inputs)
	t.Logf("inputs on /explore: %+v", inputs)

	// mark, then type into the first plausible search field
	mu.Lock()
	mark := len(urls)
	mu.Unlock()

	selectors := []string{`input[type="search"]`, `input[placeholder*="Search" i]`, `input[placeholder*="search" i]`, `[role="searchbox"]`, `input[name="searchText"]`}
	filled := ""
	for _, sel := range selectors {
		loc := page.Locator(sel)
		if n, _ := loc.Count(ctx); n > 0 {
			if err := loc.Fill(ctx, "cat"); err == nil {
				filled = sel
				_ = loc.Press(ctx, "Enter")
				break
			}
		}
	}
	t.Logf("filled selector: %q", filled)
	// give debounced search time to fire
	time.Sleep(5 * time.Second)

	mu.Lock()
	defer mu.Unlock()
	t.Logf("=== /api requests AFTER typing 'cat' ===")
	for _, u := range urls[mark:] {
		if i := strings.Index(u, "midjourney.com"); i >= 0 {
			t.Logf("  %s", u[i+len("midjourney.com"):])
		}
	}
}

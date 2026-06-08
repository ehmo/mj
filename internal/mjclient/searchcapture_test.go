package mjclient

import (
	"context"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	gomoufox "github.com/ehmo/gomoufox"
	"github.com/ehmo/gomoufox/camoufoxcfg"
)

// TestLiveSearchFilters opens a HEADFUL browser so a human can drive the MJ web
// search/explore UI (apply filters, change sort, try search-by-image) while we
// record the full query string of every explore/search/vector request. Output:
// capture/search-requests.txt. MJ_LIVE=1 MJ_SEARCH_CAPTURE=1.
func TestLiveSearchFilters(t *testing.T) {
	if os.Getenv("MJ_LIVE") != "1" || os.Getenv("MJ_SEARCH_CAPTURE") != "1" {
		t.Skip("set MJ_LIVE=1 MJ_SEARCH_CAPTURE=1")
	}
	profile := os.Getenv("MJ_PROFILE")
	if profile == "" {
		profile = "/Users/nan/Work/ai/midjourney/capture/profile"
	}
	dur := 10 * time.Minute
	if v := os.Getenv("MJ_CAPTURE_MINUTES"); v != "" {
		if n, err := time.ParseDuration(v + "m"); err == nil {
			dur = n
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), dur+time.Minute)
	defer cancel()

	br, err := gomoufox.New(ctx,
		gomoufox.WithHeadless(camoufoxcfg.HeadlessFalse),
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
	seen := map[string]bool{}
	var lines []string

	page.OnRequest(func(r *gomoufox.Request) {
		defer func() { _ = recover() }()
		u := r.URL()
		if !strings.Contains(u, "midjourney.com/api/") {
			return
		}
		// search/explore-family endpoints + any storage-upload (search-by-image)
		if !(strings.Contains(u, "/api/explore") || strings.Contains(u, "/api/search") ||
			strings.Contains(u, "vector") || strings.Contains(u, "/api/storage-upload")) {
			return
		}
		path := u
		if i := strings.Index(u, "midjourney.com"); i >= 0 {
			path = u[i+len("midjourney.com"):]
		}
		key := r.Method() + " " + path
		mu.Lock()
		if !seen[key] {
			seen[key] = true
			body := r.PostData()
			if len(body) > 300 {
				body = body[:300] + "…"
			}
			line := fmt.Sprintf("%s %s", r.Method(), path)
			if body != "" {
				line += "\n    body: " + body
			}
			lines = append(lines, line)
			fmt.Fprintf(os.Stderr, "\n[search-capture #%d] %s\n", len(lines), line)
		}
		mu.Unlock()
	})

	if _, err := page.Goto(ctx, "https://www.midjourney.com/explore",
		gomoufox.WaitUntil("domcontentloaded"), gomoufox.WithTimeout(40*time.Second)); err != nil {
		t.Logf("goto warn: %v", err)
	}

	fmt.Fprintf(os.Stderr, `
========================================================================
 SEARCH CAPTURE — browser is open. In the Midjourney web app:
   1. Use the SEARCH box (type a query).
   2. Apply any FILTERS / SORT options you can find (aspect ratio, version,
      media type image/video, date, etc.).
   3. If there's a "search by image" / visual-similarity option, try it.
 Each distinct request's query string prints below.
 Capturing for %s. Ctrl-C when done.
========================================================================
`, dur)

	deadline := time.Now().Add(dur)
	for time.Now().Before(deadline) {
		select {
		case <-time.After(2 * time.Second):
		case <-ctx.Done():
			goto dump
		}
	}
dump:
	mu.Lock()
	defer mu.Unlock()
	out := "/Users/nan/Work/ai/midjourney/capture/search-requests.txt"
	_ = os.WriteFile(out, []byte(strings.Join(lines, "\n")+"\n"), 0o644)
	t.Logf("captured %d distinct search/explore requests -> %s", len(lines), out)
}

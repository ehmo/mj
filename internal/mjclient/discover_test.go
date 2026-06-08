package mjclient

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	gomoufox "github.com/ehmo/gomoufox"
	"github.com/ehmo/gomoufox/camoufoxcfg"
)

// TestLiveDiscover drives the web app through its feature pages and records every
// midjourney.com/api/* call (method, path+query-keys, status, req/resp shape) to
// capture/discovered-endpoints.txt. Read-only navigation; MJ_LIVE=1 MJ_DISCOVER=1.
func TestLiveDiscover(t *testing.T) {
	if os.Getenv("MJ_LIVE") != "1" || os.Getenv("MJ_DISCOVER") != "1" {
		t.Skip("set MJ_LIVE=1 MJ_DISCOVER=1")
	}
	profile := os.Getenv("MJ_PROFILE")
	if profile == "" {
		profile = "/Users/nan/Work/ai/midjourney/capture/profile"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
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

	type rec struct {
		method, path string
		status       int
	}
	var mu sync.Mutex
	seen := map[string]*rec{}

	// IMPORTANT: only touch synchronous cached fields here. Calling Text()/
	// PostData() inside an OnResponse handler re-enters playwright's single
	// dispatch goroutine and deadlocks. Response shapes are probed separately.
	page.OnResponse(func(r *gomoufox.Response) {
		defer func() { _ = recover() }()
		u := r.URL()
		if !strings.Contains(u, "midjourney.com/api/") {
			return
		}
		m := r.Request().Method()
		key := m + " " + pathWithQueryKeys(u)
		mu.Lock()
		if seen[key] == nil {
			seen[key] = &rec{method: m, path: pathWithQueryKeys(u), status: r.Status()}
		}
		mu.Unlock()
	})

	routes := []string{
		"/imagine", "/explore?tab=top", "/explore?tab=likes", "/explore?tab=random",
		"/organize", "/personalize", "/moodboards", "/account", "/archive",
		"/explore?searchText=cat", "/search?q=cat",
	}
	for _, rt := range routes {
		_, _ = page.Goto(ctx, "https://www.midjourney.com"+rt,
			gomoufox.WaitUntil("domcontentloaded"), gomoufox.WithTimeout(40*time.Second))
		select {
		case <-time.After(4 * time.Second):
		case <-ctx.Done():
		}
		t.Logf("visited %s", rt)
	}

	mu.Lock()
	defer mu.Unlock()
	keys := make([]string, 0, len(seen))
	for k := range seen {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		r := seen[k]
		fmt.Fprintf(&b, "%-5s %-3d %s\n", r.method, r.status, r.path)
	}
	out := "/Users/nan/Work/ai/midjourney/capture/discovered-endpoints.txt"
	_ = os.WriteFile(out, []byte(b.String()), 0o644)
	t.Logf("discovered %d endpoints -> %s\n%s", len(keys), out, b.String())
}

func pathWithQueryKeys(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	p := u.Path
	if q := u.Query(); len(q) > 0 {
		ks := make([]string, 0, len(q))
		for k := range q {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		p += "?" + strings.Join(ks, "&")
	}
	return p
}

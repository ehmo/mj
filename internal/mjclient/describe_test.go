package mjclient

import (
	"context"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	gomoufox "github.com/ehmo/gomoufox"
	"github.com/ehmo/gomoufox/camoufoxcfg"
	playwright "github.com/playwright-community/playwright-go"
)

// TestLiveDescribeCapture uploads an image on /imagine and records the resulting
// /api/* calls + post-upload UI controls, to discover describe/blend flows.
// MJ_LIVE=1 MJ_DESC=1 MJ_IMG=/path/to/image.png
func TestLiveDescribeCapture(t *testing.T) {
	if os.Getenv("MJ_LIVE") != "1" || os.Getenv("MJ_DESC") != "1" {
		t.Skip("set MJ_LIVE=1 MJ_DESC=1")
	}
	img := os.Getenv("MJ_IMG")
	if img == "" {
		img = "/Users/nan/Work/ai/midjourney/testout/3364be4b-52d0-4284-8404-5f2b6fc0e119_0.png"
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
	type rq struct{ method, url, body string }
	var reqs []rq
	page.OnRequest(func(r *gomoufox.Request) {
		defer func() { _ = recover() }()
		u := r.URL()
		if !strings.Contains(u, "midjourney.com/api/") {
			return
		}
		body := ""
		if r.Method() != "GET" {
			body = r.PostData()
			if len(body) > 300 {
				body = body[:300]
			}
		}
		mu.Lock()
		reqs = append(reqs, rq{r.Method(), u, body})
		mu.Unlock()
	})

	if _, err := page.Goto(ctx, "https://www.midjourney.com/imagine",
		gomoufox.WaitUntil("domcontentloaded"), gomoufox.WithTimeout(40*time.Second)); err != nil {
		t.Fatalf("goto: %v", err)
	}
	time.Sleep(5 * time.Second)

	pw := page.Raw().(playwright.Page)

	// Try a direct hidden file input first; else use the Add Images file chooser.
	uploaded := false
	if loc := pw.Locator(`input[type="file"]`); loc != nil {
		if n, _ := loc.Count(); n > 0 {
			if err := pw.SetInputFiles(`input[type="file"]`, []string{img}); err == nil {
				uploaded = true
				t.Logf("uploaded via input[type=file]")
			}
		}
	}
	if !uploaded {
		fc, err := pw.ExpectFileChooser(func() error {
			// click an "Add Images" / image upload control
			for _, sel := range []string{`button[aria-label*="image" i]`, `text=Add Images`, `[aria-label*="Add" i]`} {
				if e := pw.Locator(sel).First(); e != nil {
					if n, _ := e.Count(); n > 0 {
						_ = e.Click()
						return nil
					}
				}
			}
			return nil
		})
		if err == nil && fc != nil {
			if err := fc.SetFiles(img); err == nil {
				uploaded = true
				t.Logf("uploaded via file chooser")
			}
		} else {
			t.Logf("file chooser err: %v", err)
		}
	}

	time.Sleep(6 * time.Second)

	// Dump clickable controls (to locate Describe/Blend triggers).
	var ctrls []string
	_ = page.EvaluateIntoJSON(ctx, `() => {
		const out = [];
		for (const e of document.querySelectorAll('button,[role="button"],a,[role="menuitem"]')) {
			const t = (e.innerText||e.getAttribute('aria-label')||'').trim();
			if (t && t.length < 40) out.push(t);
		}
		return Array.from(new Set(out));
	}`, &ctrls)
	sort.Strings(ctrls)
	t.Logf("uploaded=%v; controls after upload: %v", uploaded, ctrls)

	mu.Lock()
	defer mu.Unlock()
	t.Logf("=== /api requests during upload ===")
	for _, r := range reqs {
		p := r.url
		if i := strings.Index(p, "midjourney.com"); i >= 0 {
			p = p[i+len("midjourney.com"):]
		}
		if r.body != "" {
			t.Logf("  %s %s  body=%s", r.method, p, r.body)
		} else {
			t.Logf("  %s %s", r.method, p)
		}
	}
}

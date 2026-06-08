package mjclient

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	gomoufox "github.com/ehmo/gomoufox"
	"github.com/ehmo/gomoufox/camoufoxcfg"
)

// TestLiveEditorCapture opens a HEADFUL browser so a human can drive the MJ web
// editor (pan / zoom-out / vary-region) while we record the POST bodies of every
// /api/submit-jobs and /api/editor-* request. Output: capture/editor-submits.jsonl
// (one JSON object per captured request). MJ_LIVE=1 MJ_EDITOR_CAPTURE=1.
//
// The goal is to learn the exact submit-jobs payload shape for pan/outpaint/
// inpaint, which earlier probes confirmed are real verbs but whose params we
// never captured.
func TestLiveEditorCapture(t *testing.T) {
	if os.Getenv("MJ_LIVE") != "1" || os.Getenv("MJ_EDITOR_CAPTURE") != "1" {
		t.Skip("set MJ_LIVE=1 MJ_EDITOR_CAPTURE=1")
	}
	profile := os.Getenv("MJ_PROFILE")
	if profile == "" {
		profile = "/Users/nan/Work/ai/midjourney/capture/profile"
	}
	dur := 12 * time.Minute
	if v := os.Getenv("MJ_CAPTURE_MINUTES"); v != "" {
		if n, err := time.ParseDuration(v + "m"); err == nil {
			dur = n
		}
	}
	ctx, cancel := context.WithTimeout(context.Background(), dur+time.Minute)
	defer cancel()

	br, err := gomoufox.New(ctx,
		gomoufox.WithHeadless(camoufoxcfg.HeadlessFalse), // visible — the human interacts
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

	type capture struct {
		At     string `json:"at"`
		Method string `json:"method"`
		URL    string `json:"url"`
		Body   string `json:"body"`
	}
	var mu sync.Mutex
	var caps []capture
	tick := 0

	// PostData() is cached on the request event (sent at request time), so it is
	// safe to read synchronously here — unlike Response.Text(), which deadlocks.
	page.OnRequest(func(r *gomoufox.Request) {
		defer func() { _ = recover() }()
		u := r.URL()
		if !strings.Contains(u, "midjourney.com/api/") {
			return
		}
		isSubmit := strings.Contains(u, "/api/submit-jobs")
		isEditor := strings.Contains(u, "/api/editor")
		if !isSubmit && !isEditor {
			return
		}
		if r.Method() != "POST" {
			return
		}
		c := capture{
			At:     fmt.Sprintf("t+%ds", tick),
			Method: r.Method(),
			URL:    u,
			Body:   r.PostData(),
		}
		mu.Lock()
		caps = append(caps, c)
		n := len(caps)
		mu.Unlock()
		tag := "EDITOR"
		if isSubmit {
			tag = "SUBMIT"
		}
		body := c.Body
		if len(body) > 600 {
			body = body[:600] + "…"
		}
		fmt.Fprintf(os.Stderr, "\n[capture #%d %s] %s\n  %s\n", n, tag, u, body)
	})

	if _, err := page.Goto(ctx, "https://www.midjourney.com/imagine",
		gomoufox.WaitUntil("domcontentloaded"), gomoufox.WithTimeout(40*time.Second)); err != nil {
		t.Logf("goto warn: %v", err)
	}

	fmt.Fprintf(os.Stderr, `
========================================================================
 EDITOR CAPTURE — browser is open. In the Midjourney web app:
   1. Open an existing image in the EDITOR.
   2. Do a PAN (arrow / drag the canvas edge) -> generate.
   3. Do a ZOOM OUT (1.5x or 2x) -> generate.
   4. Do a VARY REGION: select an area, type a prompt -> generate.
 Each "generate" fires a /api/submit-jobs POST that prints below.
 Capturing for %s. Ctrl-C early once you've done all three.
========================================================================
`, dur)

	deadline := time.Now().Add(dur)
	for time.Now().Before(deadline) {
		select {
		case <-time.After(2 * time.Second):
		case <-ctx.Done():
			goto dump
		}
		tick += 2
	}
dump:
	mu.Lock()
	defer mu.Unlock()
	var b strings.Builder
	for _, c := range caps {
		line, _ := json.Marshal(c)
		b.Write(line)
		b.WriteByte('\n')
	}
	out := "/Users/nan/Work/ai/midjourney/capture/editor-submits.jsonl"
	_ = os.WriteFile(out, []byte(b.String()), 0o644)
	t.Logf("captured %d submit/editor POSTs -> %s", len(caps), out)
}

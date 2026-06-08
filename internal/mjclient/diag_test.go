package mjclient

import (
	"context"
	"os"
	"testing"
	"time"

	gomoufox "github.com/ehmo/gomoufox"
	"github.com/ehmo/gomoufox/camoufoxcfg"
)

// TestLiveDiag navigates to /imagine and screenshots whatever Camoufox renders,
// to diagnose Cloudflare/challenge/login state. MJ_LIVE=1 [MJ_HEADFUL=1].
func TestLiveDiag(t *testing.T) {
	if os.Getenv("MJ_LIVE") != "1" {
		t.Skip("set MJ_LIVE=1")
	}
	profile := os.Getenv("MJ_PROFILE")
	if profile == "" {
		profile = "/Users/nan/Work/ai/midjourney/capture/profile"
	}
	hl := camoufoxcfg.HeadlessTrue
	if os.Getenv("MJ_HEADFUL") == "1" {
		hl = camoufoxcfg.HeadlessFalse
	}
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()

	opts := []gomoufox.Option{
		gomoufox.WithHeadless(hl),
		gomoufox.WithConnectTimeout(90 * time.Second),
	}
	if os.Getenv("MJ_DIRECT") == "1" {
		opts = append(opts, gomoufox.WithUnsafeDirectNetwork(true))
	}
	if os.Getenv("MJ_NOPROFILE") != "1" {
		opts = append(opts, gomoufox.WithPersistentContext(profile))
	}
	br, err := gomoufox.New(ctx, opts...)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer br.Close()
	page, err := br.NewPage(ctx)
	if err != nil {
		t.Fatalf("NewPage: %v", err)
	}
	// neutral site first to isolate gomoufox-wide vs MJ-specific navigation
	_, e1 := page.Goto(ctx, "https://example.com",
		gomoufox.WaitUntil("domcontentloaded"), gomoufox.WithTimeout(20*time.Second))
	t.Logf("goto(example.com) err=%v url=%s", e1, page.URL())

	target := "https://www.midjourney.com/imagine"
	if u := os.Getenv("MJ_URL"); u != "" {
		target = u
	}
	_, gerr := page.Goto(ctx, target,
		gomoufox.WaitUntil("commit"), gomoufox.WithTimeout(30*time.Second))
	t.Logf("goto(commit) err=%v url=%s", gerr, page.URL())

	// Let any challenge/redirect settle, then capture state.
	select {
	case <-time.After(12 * time.Second):
	case <-ctx.Done():
	}
	title, _ := page.Title(ctx)
	t.Logf("after settle: url=%s title=%q", page.URL(), title)
	if png, err := page.Screenshot(ctx); err == nil {
		_ = os.WriteFile("/Users/nan/Work/ai/midjourney/capture/park.png", png, 0o644)
		t.Logf("screenshot -> capture/park.png (%d bytes)", len(png))
	} else {
		t.Logf("screenshot err: %v", err)
	}
}

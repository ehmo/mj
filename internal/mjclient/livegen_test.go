package mjclient

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/ehmo/mj/internal/mjapi"
)

// TestLiveGenerate submits ONE real imagine, waits, and downloads — validating
// the submit→poll→assets→download loop end-to-end. Costs a real generation, so
// it is double-gated: MJ_LIVE=1 AND MJ_LIVE_GEN=1.
func TestLiveGenerate(t *testing.T) {
	if os.Getenv("MJ_LIVE") != "1" || os.Getenv("MJ_LIVE_GEN") != "1" {
		t.Skip("set MJ_LIVE=1 MJ_LIVE_GEN=1 to run (submits a real generation)")
	}
	profile := os.Getenv("MJ_PROFILE")
	if profile == "" {
		profile = "/Users/nan/Work/ai/midjourney/capture/profile"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()

	c, err := New(ctx, Config{ProfileDir: profile, Headless: true})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()

	ar := "1:1"
	job, err := c.Imagine(ctx, ImagineReq{
		Prompt: "a single red maple leaf on a plain white background, minimal",
		Params: mjapi.Params{AR: ar, Version: "7"},
		Mode:   mjapi.ModeFast,
	})
	if err != nil {
		t.Fatalf("Imagine: %v", err)
	}
	t.Logf("submitted job=%s batch=%d %dx%d", job.ID, job.BatchSize, job.Width, job.Height)
	if job.ID == "" {
		t.Fatal("empty job id")
	}

	final, err := c.Wait(ctx, job.ID, WaitOpts{
		Timeout:  6 * time.Minute,
		OnUpdate: func(j mjapi.Job) { t.Logf("  status=%s", j.Status) },
	})
	if err != nil {
		t.Fatalf("Wait: %v", err)
	}
	t.Logf("completed: status=%s", final.Status)
	if final.BatchSize == 0 {
		final.BatchSize = job.BatchSize
	}

	dir := t.TempDir()
	files, err := c.Download(ctx, final, dir, SelAll)
	if err != nil {
		t.Fatalf("Download: %v", err)
	}
	t.Logf("downloaded %d files into %s", len(files), dir)
	for _, f := range files {
		if fi, err := os.Stat(f); err != nil || fi.Size() < 1000 {
			t.Errorf("bad file %s: %v", f, err)
		}
	}
}

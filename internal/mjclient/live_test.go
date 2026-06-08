package mjclient

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/ehmo/mj/internal/mjapi"
)

// TestLiveSmoke validates the full auth chain + job-status + CDN against the
// real site. Opt-in only: MJ_LIVE=1. Reuses a persistent profile (MJ_PROFILE,
// default the capture profile). Non-destructive — submits nothing.
func TestLiveSmoke(t *testing.T) {
	if os.Getenv("MJ_LIVE") != "1" {
		t.Skip("set MJ_LIVE=1 to run the live smoke test")
	}
	profile := os.Getenv("MJ_PROFILE")
	if profile == "" {
		profile = "/Users/nan/Work/ai/midjourney/capture/profile"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	c, err := New(ctx, Config{ProfileDir: profile, Headless: true})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()

	acct, err := c.Account(ctx)
	if err != nil {
		t.Fatalf("Account (auth chain): %v", err)
	}
	t.Logf("authenticated: user=%s plan=%s relax=%v private=%v jobs=%d",
		acct.UserID, acct.PlanType, acct.CanRelax, acct.CanPrivate, acct.TotalJobs)
	if acct.UserID == "" {
		t.Fatal("empty user id after auth")
	}

	// Job-status on a known completed job from the capture run.
	jobID := mjapi.JobID(os.Getenv("MJ_JOB"))
	if jobID == "" {
		jobID = "786ca9a7-380f-49bf-a3af-877d3ce97b1e"
	}
	jobs, err := c.Status(ctx, jobID)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if len(jobs) == 0 {
		t.Fatalf("job %s not returned by status", jobID)
	}
	j := jobs[0]
	t.Logf("job %s status=%s type=%s batch=%d %dx%d", j.ID, j.Status, j.JobType, j.BatchSize, j.Width, j.Height)

	// CDN download through the browser (Cloudflare-gated; plain HTTP is 403).
	if j.Status.OK() && !j.IsVideo() {
		if j.BatchSize == 0 {
			j.BatchSize = 4
		}
		dir := t.TempDir()
		files, err := c.Download(ctx, j, dir, SelCells)
		if err != nil {
			t.Errorf("Download: %v", err)
		} else {
			t.Logf("downloaded %d files (e.g. %s)", len(files), files[0])
			if fi, err := os.Stat(files[0]); err != nil || fi.Size() < 1000 {
				t.Errorf("downloaded file looks wrong: %v size=%v", err, fi)
			}
		}
	}
}

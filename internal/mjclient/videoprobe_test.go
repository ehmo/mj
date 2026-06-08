package mjclient

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"
)

// TestLiveVideoProbe HEADs candidate CDN URLs for a completed video job to find
// the .mp4 asset pattern. MJ_LIVE=1 MJ_VIDPROBE=1 [MJ_VIDJOB=...].
func TestLiveVideoProbe(t *testing.T) {
	if os.Getenv("MJ_LIVE") != "1" || os.Getenv("MJ_VIDPROBE") != "1" {
		t.Skip("set MJ_LIVE=1 MJ_VIDPROBE=1")
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
	c, err := New(ctx, Config{ProfileDir: profile, Headless: true})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()

	candidates := []string{
		"/%s/0_0.mp4", "/%s/0_1.mp4", "/%s/0_2.mp4", "/%s/0_3.mp4",
		"/%s/video_0.mp4", "/%s/0_0_360.mp4", "/%s/0.mp4",
		"/%s/0_0.png", "/%s/grid_0.mp4",
	}
	const headJS = `async ({url}) => {
	  try { const r = await fetch(url, {method:'HEAD'}); return {status:r.status, type:r.headers.get('content-type')||'', len:r.headers.get('content-length')||''}; }
	  catch(e){ return {status:0, type:String(e), len:''}; }
	}`
	for _, tmpl := range candidates {
		u := "https://cdn.midjourney.com" + fmt.Sprintf(tmpl, job)
		var out struct {
			Status int    `json:"status"`
			Type   string `json:"type"`
			Len    string `json:"len"`
		}
		raw, err := c.conn.Eval(ctx, headJS, map[string]any{"url": u})
		if err != nil {
			t.Logf("%-40s ERR %v", tmpl, err)
			continue
		}
		_ = json.Unmarshal(raw, &out)
		t.Logf("%-22s -> %d  %s  %s", tmpl[3:], out.Status, out.Type, out.Len)
	}
}

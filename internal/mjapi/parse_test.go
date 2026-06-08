package mjapi

import "testing"

// Fixtures mirror the live-captured shapes (spec §18), redacted.

const authUserJSON = `{"status":true,"authUser":{
  "id":"00000000-1111-2222-3333-444444444444","email":"r@example.com","emailVerified":true,
  "system":"firebase","websocketToken":"eyJhbGciOiJIUzI1NiJ9.payload.sig",
  "abilities":{"can_private":false,"can_relax":false,"total_jobs":4,
    "subscription":{"type":"subscription","plan_type":"basic","recurring":"month"}},
  "profile":{"user_id":"00000000-1111-2222-3333-444444444444","display_name":"testuser","username_v2":"testuser"},
  "userFlags":{"settings":{"speed":"fast","visibility":"public"}}},"experimentVariants":{}}`

func TestParseAuthUser(t *testing.T) {
	a, err := ParseAuthUser([]byte(authUserJSON))
	if err != nil {
		t.Fatal(err)
	}
	if a.UserID != "00000000-1111-2222-3333-444444444444" {
		t.Errorf("user id = %q", a.UserID)
	}
	if a.PlanType != "basic" {
		t.Errorf("plan = %q", a.PlanType)
	}
	if a.CanRelax || a.CanPrivate {
		t.Errorf("basic plan should not have relax/private")
	}
	if a.DefaultSpeed != "fast" {
		t.Errorf("speed = %q", a.DefaultSpeed)
	}
	if a.WSToken == "" {
		t.Error("ws token missing")
	}
}

func TestParseAuthUserMissing(t *testing.T) {
	if _, err := ParseAuthUser([]byte(`{"status":false}`)); err == nil {
		t.Error("expected error on missing authUser")
	}
}

const submitOKJSON = `{"success":[{"job_id":"786ca9a7-380f-49bf-a3af-877d3ce97b1e",
  "prompt":"a vintage teal bicycle --chaos 12 --ar 3:2 --v 7.0","is_queued":false,"softban":false,
  "event_type":"diffusion","job_type":"v7_diffusion","flags":{"mode":"fast","visibility":"public"},
  "meta":{"height":896,"width":1344,"batch_size":4,"parent_id":null,"parent_grid":null},
  "optimisticJobIndex":0,"personalization_codes":null}],"failure":[]}`

func TestParseSubmitOK(t *testing.T) {
	jobs, err := ParseSubmitResponse([]byte(submitOKJSON))
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 {
		t.Fatalf("want 1 job, got %d", len(jobs))
	}
	j := jobs[0]
	if j.ID != "786ca9a7-380f-49bf-a3af-877d3ce97b1e" {
		t.Errorf("id = %q", j.ID)
	}
	if j.BatchSize != 4 || j.Width != 1344 || j.Height != 896 {
		t.Errorf("meta wrong: %+v", j)
	}
}

func TestParseSubmitFailure(t *testing.T) {
	if _, err := ParseSubmitResponse([]byte(`{"success":[],"failure":[{"reason":"banned word"}]}`)); err == nil {
		t.Error("expected error on failure[]")
	}
	if _, err := ParseSubmitResponse([]byte(`{"success":[],"failure":[]}`)); err == nil {
		t.Error("expected error on empty success")
	}
	softban := `{"success":[{"job_id":"x","softban":true}],"failure":[]}`
	if _, err := ParseSubmitResponse([]byte(softban)); err == nil {
		t.Error("expected error on softban")
	}
}

const statusJSON = `[{"id":"786ca9a7-380f-49bf-a3af-877d3ce97b1e","job_type":"v7_diffusion",
  "event_type":"diffusion","full_command":"a vintage teal bicycle --v 7.0",
  "enqueue_time":"2026-06-02 08:17:34.830862+00:00","width":1344,"height":896,"batch_size":4,
  "published":true,"liked_by_user":false,"user_id":"00000000","current_status":"completed",
  "video_segments":null,"parent_grid":null,"parent_id":null}]`

func TestParseJobStatus(t *testing.T) {
	jobs, err := ParseJobStatus([]byte(statusJSON))
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 1 {
		t.Fatalf("want 1, got %d", len(jobs))
	}
	if !jobs[0].Status.OK() || !jobs[0].Status.Terminal() {
		t.Errorf("status = %q, want completed/terminal", jobs[0].Status)
	}
}

func TestStatusSemantics(t *testing.T) {
	if !StatusCompleted.OK() {
		t.Error("completed should be OK")
	}
	if StatusFailed.OK() {
		t.Error("failed not OK")
	}
	if !StatusFailed.Terminal() || !StatusModerated.Terminal() {
		t.Error("failed/moderated terminal")
	}
	if StatusRunning.Terminal() || StatusQueued.Terminal() {
		t.Error("running/queued not terminal")
	}
}

const feedJSON = `{"data":[
  {"id":"786ca9a7","job_type":"v7_diffusion","event_type":"diffusion","full_command":"a cat","batch_size":4,"width":1344,"height":896,"parent_id":null,"parent_grid":null},
  {"id":"095b55af","job_type":"v7_diffusion","event_type":"variation","full_command":"a dog","batch_size":4,"width":1344,"height":896,"parent_id":"786ca9a7","parent_grid":2}
],"cursor":"gAAAAABnext","checkpoint":null}`

func TestParseFeed(t *testing.T) {
	jobs, cursor, err := ParseFeed([]byte(feedJSON))
	if err != nil {
		t.Fatal(err)
	}
	if len(jobs) != 2 {
		t.Fatalf("want 2, got %d", len(jobs))
	}
	if cursor != "gAAAAABnext" {
		t.Errorf("cursor = %q", cursor)
	}
	if jobs[1].ParentGrid == nil || *jobs[1].ParentGrid != 2 {
		t.Errorf("parent_grid not parsed: %+v", jobs[1])
	}
}

func TestParseFeedNullCursor(t *testing.T) {
	_, cursor, err := ParseFeed([]byte(`{"data":[],"cursor":null,"checkpoint":null}`))
	if err != nil {
		t.Fatal(err)
	}
	if cursor != "" {
		t.Errorf("want empty cursor, got %q", cursor)
	}
}

func TestAssetsGrid(t *testing.T) {
	j := Job{ID: "786ca9a7", BatchSize: 4, JobType: "v7_diffusion", EventType: "diffusion"}
	a := Assets(j)
	if len(a) != 5 { // 4 cells + 1 grid
		t.Fatalf("want 5 assets, got %d", len(a))
	}
	if a[0].URL != "https://cdn.midjourney.com/786ca9a7/0_0.png" {
		t.Errorf("cell0 = %q", a[0].URL)
	}
	if a[4].Kind != AssetGrid || a[4].URL != "https://cdn.midjourney.com/786ca9a7/grid_0.png" {
		t.Errorf("grid = %+v", a[4])
	}
}

func TestAssetsUpscale(t *testing.T) {
	j := Job{ID: "676ef3aa", BatchSize: 1, JobType: "v7_upscaler_2x_creative", EventType: "diffusion_upsample_v7_2x_creative"}
	a := Assets(j)
	if len(a) != 1 {
		t.Fatalf("upscale want 1 asset, got %d", len(a))
	}
	if a[0].URL != "https://cdn.midjourney.com/676ef3aa/0_0.png" {
		t.Errorf("got %q", a[0].URL)
	}
}

func TestAssetsVideo(t *testing.T) {
	j := Job{ID: "e4252c82", BatchSize: 4, JobType: "vid_1.1_i2v_render", EventType: "video_diffusion"}
	a := Assets(j)
	if len(a) != 4 {
		t.Fatalf("video want 4 assets, got %d", len(a))
	}
	if a[0].Kind != AssetVideo || a[0].URL != "https://cdn.midjourney.com/video/e4252c82/0.mp4" {
		t.Errorf("video asset0 = %+v", a[0])
	}
	if a[3].URL != "https://cdn.midjourney.com/video/e4252c82/3.mp4" {
		t.Errorf("video asset3 = %+v", a[3])
	}
}

func TestParseDescribe(t *testing.T) {
	js := `{"success":true,"data":{"descriptions":["a golden origami fox","a paper fox figurine","an origami fox on wood","a small folded fox"]}}`
	d, err := ParseDescribe([]byte(js))
	if err != nil {
		t.Fatal(err)
	}
	if len(d) != 4 || d[0] != "a golden origami fox" {
		t.Errorf("describe parse = %v", d)
	}
	if _, err := ParseDescribe([]byte(`{"success":false}`)); err == nil {
		t.Error("expected error on empty descriptions")
	}
}

func TestAssetsOptsVariants(t *testing.T) {
	j := Job{ID: "JID", BatchSize: 4}
	// full-res webp cells + grid
	got := AssetsOpts(j, AssetOpts{Format: "webp"})
	if got[0].URL != CDNBase+"/JID/0_0.webp" || got[0].Kind != AssetCell {
		t.Errorf("webp cell0 = %+v", got[0])
	}
	if got[4].URL != CDNBase+"/JID/grid_0.webp" || got[4].Kind != AssetGrid {
		t.Errorf("webp grid = %+v", got[4])
	}
	// sized thumbnail -> AssetThumb, _N suffix
	th := AssetsOpts(j, AssetOpts{Format: "webp", Size: 384})
	if th[0].URL != CDNBase+"/JID/0_0_384_N.webp" || th[0].Kind != AssetThumb {
		t.Errorf("thumb cell0 = %+v", th[0])
	}
	if th[4].URL != CDNBase+"/JID/grid_0_384_N.webp" {
		t.Errorf("thumb grid = %+v", th[4])
	}
	// default = full png (back-compat)
	d := Assets(j)
	if d[0].URL != CDNBase+"/JID/0_0.png" {
		t.Errorf("default cell0 = %+v", d[0])
	}
	// video unaffected by opts
	v := AssetsOpts(Job{ID: "VID", BatchSize: 2, JobType: "vid_1.1_i2v_render"}, AssetOpts{Format: "webp", Size: 384})
	if len(v) != 2 || v[0].Kind != AssetVideo || v[0].URL != CDNBase+"/video/VID/0.mp4" {
		t.Errorf("video = %+v", v)
	}
}

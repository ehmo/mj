package mjapi

import "testing"

const exploreJSON = `[
 {"isFeedJob":true,"id":"faa458f2-29ae-4129-9e12-dbd0f7dd4b8c","job_type":"v8-1_hd_diffusion",
  "event_type":"diffusion","width":1856,"height":2592,"username_v2":"sparklepup","display_name":"sparklepup",
  "user_id":"dee2e6ce","prompt":{"decodedPrompt":[{"content":"A grid of different dogs dressed as humans","weight":1}],
  "version":"8.1","ar":{"w":5,"h":7},"tile":true}},
 {"isFeedJob":true,"id":"abc","job_type":"v7_diffusion","width":1024,"height":1024,"username_v2":"bob",
  "display_name":"Bob","user_id":"u2","prompt":"a plain string prompt"}
]`

func TestParseExplore(t *testing.T) {
	items, err := ParseExplore([]byte(exploreJSON))
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("want 2, got %d", len(items))
	}
	a := items[0]
	if a.ID != "faa458f2-29ae-4129-9e12-dbd0f7dd4b8c" || a.Username != "sparklepup" {
		t.Errorf("item0 wrong: %+v", a)
	}
	if a.Prompt != "A grid of different dogs dressed as humans" {
		t.Errorf("decoded prompt = %q", a.Prompt)
	}
	if a.Version != "8.1" || a.AR != "5:7" {
		t.Errorf("version/ar = %q/%q", a.Version, a.AR)
	}
	if a.ImageURL != "https://cdn.midjourney.com/faa458f2-29ae-4129-9e12-dbd0f7dd4b8c/0_0.png" {
		t.Errorf("image url = %q", a.ImageURL)
	}
	// string-prompt fallback
	if items[1].Prompt != "a plain string prompt" {
		t.Errorf("string prompt fallback = %q", items[1].Prompt)
	}
}

func TestParseMoodboards(t *testing.T) {
	js := `[{"moodboard_id":"7460","user_id":"u","title":"My First Moodboard","personalize":false,"created":"2026-06-02 08:16:11","images":[]}]`
	mb, err := ParseMoodboards([]byte(js))
	if err != nil {
		t.Fatal(err)
	}
	if len(mb) != 1 || mb[0].ID != "7460" || mb[0].Title != "My First Moodboard" || mb[0].ImageCount != 0 {
		t.Errorf("moodboard parse wrong: %+v", mb)
	}
}

func TestParseQueue(t *testing.T) {
	js := `{"running":[{"id":"r1","current_status":"running","job_type":"v7_diffusion","batch_size":4}],"waiting":[]}`
	q, err := ParseQueue([]byte(js))
	if err != nil {
		t.Fatal(err)
	}
	if len(q.Running) != 1 || q.Running[0].ID != "r1" || q.Running[0].Status != StatusRunning {
		t.Errorf("queue parse wrong: %+v", q)
	}
	if len(q.Waiting) != 0 {
		t.Errorf("waiting should be empty")
	}
}

func TestParseExploreRich(t *testing.T) {
	raw := []byte(`[{
	  "id":"abc","user_id":"u1","username_v2":"alice","display_name":"Alice",
	  "job_type":"v8-1_diffusion","type":"image","parent_id":"p1","parent_grid":"g1",
	  "enqueue_time":1780673713270,"published":true,
	  "items":[{"liked_by_user":false},{"liked_by_user":true}],
	  "width":816,"height":1456,
	  "prompt":{"decodedPrompt":[{"content":"a koi fish"}],"version":"8.1",
	    "ar":{"w":9,"h":16},"no":["plastic","cgi"],"styleRaw":true,
	    "stylize":250,"chaos":12,"seed":42,
	    "styleRef":[{"t":"url","content":"https://s.mj.run/aGH3w0c01Jo","weight":1}],"sw":100}
	}]`)
	items, err := ParseExplore(raw)
	if err != nil {
		t.Fatal(err)
	}
	it := items[0]
	if it.Type != "image" || it.ParentID != "p1" || it.ParentGrid != "g1" ||
		it.EnqueueTime != 1780673713270 || !it.Published || !it.LikedByUser {
		t.Errorf("meta = %+v", it)
	}
	if it.Prompt != "a koi fish" || it.Version != "8.1" || it.AR != "9:16" {
		t.Errorf("prompt/version/ar = %q %q %q", it.Prompt, it.Version, it.AR)
	}
	want := "a koi fish --ar 9:16 --v 8.1 --s 250 --chaos 12 --seed 42 --no plastic,cgi --sref https://s.mj.run/aGH3w0c01Jo --sw 100 --style raw"
	if it.Command != want {
		t.Errorf("command mismatch:\n got %q\nwant %q", it.Command, want)
	}
}

func TestParseExploreParamsOnlyPrompt(t *testing.T) {
	// object prompt with params but NO decodedPrompt and NO version — must not drop
	raw := []byte(`[{"id":"x","prompt":{"no":["foo"],"seed":7,"ar":{"w":2,"h":3},"chaos":15},"items":[{}]}]`)
	items, err := ParseExplore(raw)
	if err != nil || len(items) != 1 {
		t.Fatalf("parse: %v len=%d", err, len(items))
	}
	if items[0].AR != "2:3" || items[0].Command != "--ar 2:3 --chaos 15 --seed 7 --no foo" {
		t.Errorf("params-only command = %q ar=%q", items[0].Command, items[0].AR)
	}
}

func TestParseExploreVideoItem(t *testing.T) {
	raw := []byte(`[{"id":"vid1","type":"video","prompt":{"decodedPrompt":[{"content":"a cat"}],"video":true},"items":[{},{}]}]`)
	items, _ := ParseExplore(raw)
	it := items[0]
	if !it.Video || it.ImageURL != CDNBase+"/video/vid1/0.mp4" || it.BatchSize != 2 {
		t.Errorf("video item = %+v", it)
	}
}

func TestParseExploreBatchFallback(t *testing.T) {
	// items[] absent but top-level batch_size present
	raw := []byte(`[{"id":"g","batch_size":4,"prompt":"plain string prompt"}]`)
	items, _ := ParseExplore(raw)
	if items[0].BatchSize != 4 || items[0].Prompt != "plain string prompt" {
		t.Errorf("batch fallback = %+v", items[0])
	}
}

package mjapi

import (
	"encoding/json"
	"fmt"
)

// CDNBase is the public, unauthenticated result CDN.
const CDNBase = "https://cdn.midjourney.com"

// ---- firebase-login authUser bootstrap ----

type authUserEnvelope struct {
	Status   bool `json:"status"`
	AuthUser struct {
		ID             string `json:"id"`
		Email          string `json:"email"`
		WebsocketToken string `json:"websocketToken"`
		Abilities      struct {
			CanPrivate   bool `json:"can_private"`
			CanRelax     bool `json:"can_relax"`
			TotalJobs    int  `json:"total_jobs"`
			Subscription struct {
				PlanType string `json:"plan_type"`
			} `json:"subscription"`
		} `json:"abilities"`
		Profile struct {
			UserID      string `json:"user_id"`
			DisplayName string `json:"display_name"`
		} `json:"profile"`
		UserFlags struct {
			Settings struct {
				Speed      string `json:"speed"`
				Visibility string `json:"visibility"`
			} `json:"settings"`
		} `json:"userFlags"`
	} `json:"authUser"`
}

// ParseAuthUser maps a /api/firebase-login response to an Account.
func ParseAuthUser(raw []byte) (Account, error) {
	var e authUserEnvelope
	if err := json.Unmarshal(raw, &e); err != nil {
		return Account{}, fmt.Errorf("parse authUser: %w", err)
	}
	au := e.AuthUser
	if au.ID == "" {
		return Account{}, fmt.Errorf("parse authUser: missing user id (status=%v)", e.Status)
	}
	return Account{
		UserID:       au.ID,
		Email:        au.Email,
		DisplayName:  au.Profile.DisplayName,
		PlanType:     au.Abilities.Subscription.PlanType,
		CanRelax:     au.Abilities.CanRelax,
		CanPrivate:   au.Abilities.CanPrivate,
		TotalJobs:    au.Abilities.TotalJobs,
		DefaultSpeed: au.UserFlags.Settings.Speed,
		Visibility:   au.UserFlags.Settings.Visibility,
		WSToken:      au.WebsocketToken,
	}, nil
}

// ---- submit-jobs ----

type submitEnvelope struct {
	Success []submitJob       `json:"success"`
	Failure []json.RawMessage `json:"failure"`
}

type submitJob struct {
	JobID     string `json:"job_id"`
	Prompt    string `json:"prompt"`
	IsQueued  bool   `json:"is_queued"`
	Softban   bool   `json:"softban"`
	EventType string `json:"event_type"`
	JobType   string `json:"job_type"`
	Meta      struct {
		Height     int    `json:"height"`
		Width      int    `json:"width"`
		BatchSize  int    `json:"batch_size"`
		ParentID   string `json:"parent_id"`
		ParentGrid *int   `json:"parent_grid"`
	} `json:"meta"`
}

// ParseSubmitResponse maps a /api/submit-jobs response to jobs. It returns an
// error if the server reported any failure, or if a job is softbanned.
func ParseSubmitResponse(raw []byte) ([]Job, error) {
	var e submitEnvelope
	if err := json.Unmarshal(raw, &e); err != nil {
		return nil, fmt.Errorf("parse submit-jobs: %w", err)
	}
	if len(e.Failure) > 0 {
		return nil, fmt.Errorf("submit-jobs failed: %s", string(e.Failure[0]))
	}
	if len(e.Success) == 0 {
		return nil, fmt.Errorf("submit-jobs returned no jobs")
	}
	jobs := make([]Job, 0, len(e.Success))
	for _, s := range e.Success {
		if s.Softban {
			return nil, fmt.Errorf("submit-jobs softban for job %s (prompt rejected)", s.JobID)
		}
		jobs = append(jobs, Job{
			ID:         JobID(s.JobID),
			Prompt:     s.Prompt,
			EventType:  s.EventType,
			JobType:    s.JobType,
			BatchSize:  s.Meta.BatchSize,
			Width:      s.Meta.Width,
			Height:     s.Meta.Height,
			ParentID:   s.Meta.ParentID,
			ParentGrid: s.Meta.ParentGrid,
			IsQueued:   s.IsQueued,
		})
	}
	return jobs, nil
}

// ---- job-status ----

type statusJob struct {
	ID            string `json:"id"`
	JobType       string `json:"job_type"`
	EventType     string `json:"event_type"`
	FullCommand   string `json:"full_command"`
	Width         int    `json:"width"`
	Height        int    `json:"height"`
	BatchSize     int    `json:"batch_size"`
	CurrentStatus string `json:"current_status"`
	ParentID      string `json:"parent_id"`
	ParentGrid    *int   `json:"parent_grid"`
}

// ParseJobStatus maps a /api/job-status response (a JSON array) to jobs.
func ParseJobStatus(raw []byte) ([]Job, error) {
	var arr []statusJob
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil, fmt.Errorf("parse job-status: %w", err)
	}
	jobs := make([]Job, 0, len(arr))
	for _, s := range arr {
		jobs = append(jobs, Job{
			ID:          JobID(s.ID),
			Status:      Status(s.CurrentStatus),
			JobType:     s.JobType,
			EventType:   s.EventType,
			FullCommand: s.FullCommand,
			Width:       s.Width,
			Height:      s.Height,
			BatchSize:   s.BatchSize,
			ParentID:    s.ParentID,
			ParentGrid:  s.ParentGrid,
		})
	}
	return jobs, nil
}

// ---- /api/imagine feed (history; metadata only, no status) ----

type feedEnvelope struct {
	Data   []feedJob `json:"data"`
	Cursor *string   `json:"cursor"`
}

type feedJob struct {
	ID          string `json:"id"`
	JobType     string `json:"job_type"`
	EventType   string `json:"event_type"`
	FullCommand string `json:"full_command"`
	BatchSize   int    `json:"batch_size"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	ParentID    string `json:"parent_id"`
	ParentGrid  *int   `json:"parent_grid"`
}

// ParseFeed maps a /api/imagine response to jobs plus the next cursor ("" if none).
func ParseFeed(raw []byte) ([]Job, string, error) {
	var e feedEnvelope
	if err := json.Unmarshal(raw, &e); err != nil {
		return nil, "", fmt.Errorf("parse feed: %w", err)
	}
	jobs := make([]Job, 0, len(e.Data))
	for _, f := range e.Data {
		jobs = append(jobs, Job{
			ID:          JobID(f.ID),
			JobType:     f.JobType,
			EventType:   f.EventType,
			FullCommand: f.FullCommand,
			BatchSize:   f.BatchSize,
			Width:       f.Width,
			Height:      f.Height,
			ParentID:    f.ParentID,
			ParentGrid:  f.ParentGrid,
		})
	}
	cursor := ""
	if e.Cursor != nil {
		cursor = *e.Cursor
	}
	return jobs, cursor, nil
}

// ---- CDN asset derivation (pure) ----

// AssetOpts selects a CDN asset variant. Format is "png" (default) or "webp";
// webp full-res is the same resolution at ~5x smaller. Size is the thumbnail
// edge in px (e.g. 384, 640); 0 means full resolution. These apply to image
// cells and grids only — videos are always the single .mp4 variant.
type AssetOpts struct {
	Format string // "png" | "webp" (default "png")
	Size   int    // thumbnail px edge; 0 = full-res
}

func (o AssetOpts) ext() string {
	if o.Format == "webp" {
		return "webp"
	}
	return "png"
}

// cellURL builds the CDN URL for grid cell i (full-res or a sized thumbnail).
func (o AssetOpts) cellURL(id JobID, i int) string {
	if o.Size > 0 {
		return fmt.Sprintf("%s/%s/0_%d_%d_N.%s", CDNBase, id, i, o.Size, o.ext())
	}
	return fmt.Sprintf("%s/%s/0_%d.%s", CDNBase, id, i, o.ext())
}

func (o AssetOpts) gridURL(id JobID) string {
	if o.Size > 0 {
		return fmt.Sprintf("%s/%s/grid_0_%d_N.%s", CDNBase, id, o.Size, o.ext())
	}
	return fmt.Sprintf("%s/%s/grid_0.%s", CDNBase, id, o.ext())
}

// Assets returns the full-res PNG CDN URLs for a completed job (back-compat).
func Assets(j Job) []AssetURL { return AssetsOpts(j, AssetOpts{}) }

// AssetsOpts returns the CDN URLs for a completed job in the requested format/
// size. For image jobs: each grid cell, plus the composite grid for a multi-
// image batch. For video jobs: one .mp4 per batch item
// (cdn.midjourney.com/video/{id}/{i}.mp4, confirmed live; no format/size).
func AssetsOpts(j Job, opts AssetOpts) []AssetURL {
	batch := j.BatchSize
	if batch <= 0 {
		batch = 1
	}
	if j.IsVideo() {
		out := make([]AssetURL, 0, batch)
		for i := 0; i < batch; i++ {
			out = append(out, AssetURL{
				Kind:  AssetVideo,
				Index: i,
				URL:   fmt.Sprintf("%s/video/%s/%d.mp4", CDNBase, j.ID, i),
			})
		}
		return out
	}
	cellKind := AssetCell
	if opts.Size > 0 {
		cellKind = AssetThumb
	}
	out := make([]AssetURL, 0, batch+1)
	for i := 0; i < batch; i++ {
		out = append(out, AssetURL{Kind: cellKind, Index: i, URL: opts.cellURL(j.ID, i)})
	}
	if batch > 1 {
		out = append(out, AssetURL{Kind: AssetGrid, URL: opts.gridURL(j.ID)})
	}
	return out
}

// ThumbURL returns the webp thumbnail URL for grid cell i at the given size px.
func ThumbURL(id JobID, i, size int) string {
	return AssetOpts{Format: "webp", Size: size}.cellURL(id, i)
}

// ---- describe (/api/describe) ----

type describeResp struct {
	Success bool `json:"success"`
	Data    struct {
		Descriptions []string `json:"descriptions"`
	} `json:"data"`
}

// ParseDescribe parses a /api/describe response into the suggested prompts.
func ParseDescribe(raw []byte) ([]string, error) {
	var d describeResp
	if err := json.Unmarshal(raw, &d); err != nil {
		return nil, fmt.Errorf("parse describe: %w", err)
	}
	if !d.Success || len(d.Data.Descriptions) == 0 {
		return nil, fmt.Errorf("describe returned no descriptions")
	}
	return d.Data.Descriptions, nil
}

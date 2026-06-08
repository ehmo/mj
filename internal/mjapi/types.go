// Package mjapi holds the Midjourney web-API data model: typed requests,
// parameter serialization, and response parsers. It has no transport
// dependency (no gomoufox, no network) so it is fully unit-testable offline.
package mjapi

// JobID is a Midjourney job UUID.
type JobID string

// Mode is the GPU queue mode, serialized into the submit-jobs `f.mode` field.
type Mode string

const (
	ModeFast  Mode = "fast"
	ModeRelax Mode = "relax"
	ModeTurbo Mode = "turbo"
)

// Valid reports whether m is a recognized mode.
func (m Mode) Valid() bool {
	switch m {
	case ModeFast, ModeRelax, ModeTurbo:
		return true
	}
	return false
}

// UpscaleKind selects the upscaler; serialized into the submit `type` field.
type UpscaleKind string

const (
	UpscaleCreative UpscaleKind = "v7_2x_creative"
	UpscaleSubtle   UpscaleKind = "v7_2x_subtle"
)

// Status is a job's current_status value. "completed" is confirmed live; the
// other values are inferred and treated leniently by the poller (anything not
// terminal is in-progress).
type Status string

const (
	StatusCompleted Status = "completed"
	StatusFailed    Status = "failed"
	StatusModerated Status = "moderated"
	StatusQueued    Status = "queued"
	StatusRunning   Status = "running"
)

// Terminal reports whether the status is a final state.
func (s Status) Terminal() bool {
	switch s {
	case StatusCompleted, StatusFailed, StatusModerated:
		return true
	}
	return false
}

// OK reports whether the status is a successful terminal state.
func (s Status) OK() bool { return s == StatusCompleted }

// Job is the normalized view of a Midjourney job, populated from submit-jobs,
// job-status, or the /api/imagine feed.
type Job struct {
	ID          JobID  `json:"id"`
	Status      Status `json:"status,omitempty"`
	EventType   string `json:"event_type,omitempty"`
	JobType     string `json:"job_type,omitempty"`
	FullCommand string `json:"full_command,omitempty"`
	Prompt      string `json:"prompt,omitempty"`
	BatchSize   int    `json:"batch_size,omitempty"`
	Width       int    `json:"width,omitempty"`
	Height      int    `json:"height,omitempty"`
	ParentID    string `json:"parent_id,omitempty"`
	ParentGrid  *int   `json:"parent_grid,omitempty"`
	IsQueued    bool   `json:"is_queued,omitempty"`
	Softban     bool   `json:"softban,omitempty"`
}

// IsVideo reports whether the job is an image-to-video render.
func (j Job) IsVideo() bool {
	return j.EventType == "video_diffusion" ||
		(len(j.JobType) >= 3 && j.JobType[:3] == "vid")
}

// Account is the subset of the firebase-login authUser bootstrap mj needs.
type Account struct {
	UserID       string `json:"user_id"`
	Email        string `json:"email"`
	DisplayName  string `json:"display_name"`
	PlanType     string `json:"plan_type"` // basic|standard|pro|mega|""
	CanRelax     bool   `json:"can_relax"`
	CanPrivate   bool   `json:"can_private"` // stealth eligibility
	TotalJobs    int    `json:"total_jobs"`
	DefaultSpeed string `json:"default_speed"` // fast|relax|turbo
	Visibility   string `json:"visibility"`    // public|stealth
	WSToken      string `json:"-"`             // websocketToken; never serialized
}

// AssetKind identifies a CDN asset variant for a completed job.
type AssetKind string

const (
	AssetCell  AssetKind = "cell"  // 0_{i}.png full-res grid cell
	AssetGrid  AssetKind = "grid"  // grid_0.png composite
	AssetThumb AssetKind = "thumb" // {i}_{size}_N.webp thumbnail
	AssetVideo AssetKind = "video" // video/{job}/{i}.mp4
)

// AssetURL is a derived CDN URL for a job result.
type AssetURL struct {
	Kind  AssetKind
	Index int // grid cell index (0-based); 0 for grid/upscale
	URL   string
}

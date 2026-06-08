package mjclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/ehmo/mj/internal/mjapi"
)

// ErrJobFailed is returned by Wait when a job reaches a failed/moderated state.
var ErrJobFailed = errors.New("job failed or was moderated")

// ErrWaitTimeout is returned by Wait when the deadline elapses before terminal.
var ErrWaitTimeout = errors.New("timed out waiting for job")

// ImagineReq is an imagine request.
type ImagineReq struct {
	Prompt  string
	Params  mjapi.Params
	Mode    mjapi.Mode // default fast
	Private bool       // stealth (requires can_private)
}

// VideoReq is an image-to-video request.
type VideoReq struct {
	Parent         mjapi.JobID
	Index          int    // grid image (1..4)
	Motion         string // "high" | "low" (default low)
	Mode           mjapi.Mode
	Private        bool
	NewPromptExtra string // optional extra prompt text; defaults to parent prompt server-side
}

// Imagine submits a text→image generation (one job = one 4-image batch).
func (c *Client) Imagine(ctx context.Context, req ImagineReq) (mjapi.Job, error) {
	if _, err := req.Params.Validate(); err != nil {
		return mjapi.Job{}, err
	}
	if err := c.preflight(ctx, req.Mode, req.Private); err != nil {
		return mjapi.Job{}, err
	}
	prompt := req.Params.BuildPrompt(req.Prompt)
	if prompt == "" {
		return mjapi.Job{}, fmt.Errorf("imagine: empty prompt")
	}
	return c.submitOne(ctx, req.Mode, req.Private, "imagine", map[string]any{"prompt": prompt})
}

// Vary requests a variation of a grid cell.
func (c *Client) Vary(ctx context.Context, parent mjapi.JobID, index int, strong bool) (mjapi.Job, error) {
	if index < 1 || index > 4 {
		return mjapi.Job{}, fmt.Errorf("vary: index %d must be 1..4", index)
	}
	return c.submitOne(ctx, "", false, "vary", map[string]any{
		"strong": strong, "id": string(parent), "index": index,
	})
}

// Upscale upscales a grid cell.
func (c *Client) Upscale(ctx context.Context, parent mjapi.JobID, index int, kind mjapi.UpscaleKind) (mjapi.Job, error) {
	if index < 1 || index > 4 {
		return mjapi.Job{}, fmt.Errorf("upscale: index %d must be 1..4", index)
	}
	if kind == "" {
		kind = mjapi.UpscaleCreative
	}
	return c.submitOne(ctx, "", false, "upscale", map[string]any{
		"type": string(kind), "id": string(parent), "index": index,
	})
}

// Reroll re-runs a grid's prompt. [INFER] payload — confirm in Step 0.
func (c *Client) Reroll(ctx context.Context, parent mjapi.JobID) (mjapi.Job, error) {
	return c.submitOne(ctx, "", false, "reroll", map[string]any{"id": string(parent)})
}

// Video submits an image-to-video render from a grid cell.
func (c *Client) Video(ctx context.Context, req VideoReq) (mjapi.Job, error) {
	if req.Index < 1 || req.Index > 4 {
		return mjapi.Job{}, fmt.Errorf("video: index %d must be 1..4", req.Index)
	}
	motion := req.Motion
	if motion == "" {
		motion = "low"
	}
	newPrompt := req.NewPromptExtra
	// Server fills prompt from parent if empty; pass motion/video flags inline.
	suffix := fmt.Sprintf("--motion %s --video 1", motion)
	if newPrompt == "" {
		newPrompt = suffix
	} else {
		newPrompt = newPrompt + " " + suffix
	}
	return c.submitOne(ctx, req.Mode, req.Private, "video", map[string]any{
		"videoType":   "vid_1.1_i2v_480",
		"newPrompt":   newPrompt,
		"parentJob":   map[string]any{"job_id": string(req.Parent), "image_num": req.Index},
		"animateMode": "auto",
	})
}

// preflight enforces plan eligibility for relax/stealth before submitting.
func (c *Client) preflight(ctx context.Context, mode mjapi.Mode, private bool) error {
	if mode != "" && !mode.Valid() {
		return fmt.Errorf("invalid mode %q", mode)
	}
	if mode != mjapi.ModeRelax && !private {
		return nil // no plan-gated feature requested
	}
	acct, err := c.Account(ctx)
	if err != nil {
		return err
	}
	if mode == mjapi.ModeRelax && !acct.CanRelax {
		return fmt.Errorf("relax mode requires a plan with relax (current: %s)", planName(acct.PlanType))
	}
	if private && !acct.CanPrivate {
		return fmt.Errorf("stealth requires Pro/Mega (current: %s)", planName(acct.PlanType))
	}
	return nil
}

func planName(p string) string {
	if p == "" {
		return "none"
	}
	return p
}

// submitOne builds the envelope, throttles, posts, and returns the single job.
func (c *Client) submitOne(ctx context.Context, mode mjapi.Mode, private bool, verb string, extra map[string]any) (mjapi.Job, error) {
	acct, err := c.Account(ctx)
	if err != nil {
		return mjapi.Job{}, err
	}
	if mode == "" {
		mode = mjapi.ModeFast
	}
	env := map[string]any{
		"f":         map[string]any{"mode": string(mode), "private": private},
		"channelId": "singleplayer_" + acct.UserID,
		"metadata":  map[string]any{},
		"t":         verb,
	}
	for k, v := range extra {
		env[k] = v
	}
	// Throttle is enforced inside call() at the /api/submit-jobs choke point.
	body, _ := json.Marshal(env)
	b, err := c.call(ctx, http.MethodPost, "/api/submit-jobs", body)
	if err != nil {
		return mjapi.Job{}, err
	}
	jobs, err := mjapi.ParseSubmitResponse(b)
	if err != nil {
		return mjapi.Job{}, err
	}
	return jobs[0], nil
}

func (c *Client) throttleSubmit(ctx context.Context) error {
	// Hold submitMu across the whole read-wait-stamp so two concurrent submits
	// can't both observe the same gap and fire together.
	c.submitMu.Lock()
	defer c.submitMu.Unlock()
	c.mu.Lock()
	last, gap := c.lastSubmit, c.cfg.MinSubmitGap+c.submitPenalty
	c.mu.Unlock()
	if !last.IsZero() {
		if wait := gap - time.Since(last); wait > 0 {
			select {
			case <-time.After(wait):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	c.mu.Lock()
	c.lastSubmit = time.Now()
	c.mu.Unlock()
	return nil
}

// Status fetches current_status (and metadata) for the given jobs.
func (c *Client) Status(ctx context.Context, ids ...mjapi.JobID) ([]mjapi.Job, error) {
	if len(ids) == 0 {
		return nil, fmt.Errorf("status: no job ids")
	}
	strs := make([]string, len(ids))
	for i, id := range ids {
		strs[i] = string(id)
	}
	body, _ := json.Marshal(map[string]any{"jobIds": strs})
	b, err := c.call(ctx, http.MethodPost, "/api/job-status", body)
	if err != nil {
		return nil, err
	}
	return mjapi.ParseJobStatus(b)
}

// WaitOpts configures Wait.
type WaitOpts struct {
	Timeout  time.Duration     // default 5m
	OnUpdate func(j mjapi.Job) // optional progress callback per poll
}

var pollSchedule = []time.Duration{2 * time.Second, 2 * time.Second, 3 * time.Second, 3 * time.Second, 5 * time.Second}

// Wait polls job-status until the job reaches a terminal state or the timeout
// elapses. Returns ErrJobFailed for failed/moderated, ErrWaitTimeout on deadline.
func (c *Client) Wait(ctx context.Context, id mjapi.JobID, opts WaitOpts) (mjapi.Job, error) {
	timeout := opts.Timeout
	if timeout == 0 {
		timeout = 5 * time.Minute
	}
	deadline := time.Now().Add(timeout)
	for i := 0; ; i++ {
		jobs, err := c.Status(ctx, id)
		if err != nil {
			return mjapi.Job{}, err
		}
		if len(jobs) > 0 {
			j := jobs[0]
			if opts.OnUpdate != nil {
				opts.OnUpdate(j)
			}
			if j.Status.Terminal() {
				if !j.Status.OK() {
					return j, fmt.Errorf("%w: %s (%s)", ErrJobFailed, j.Status, j.ID)
				}
				return j, nil
			}
		}
		if time.Now().After(deadline) {
			return mjapi.Job{}, fmt.Errorf("%w: %s after %s", ErrWaitTimeout, id, timeout)
		}
		d := 5 * time.Second
		if i < len(pollSchedule) {
			d = pollSchedule[i]
		}
		select {
		case <-time.After(d):
		case <-ctx.Done():
			return mjapi.Job{}, ctx.Err()
		}
	}
}

// History returns the user's recent jobs (metadata only) and the next cursor.
func (c *Client) History(ctx context.Context, pageSize int, cursor string) ([]mjapi.Job, string, error) {
	acct, err := c.Account(ctx)
	if err != nil {
		return nil, "", err
	}
	if pageSize <= 0 {
		pageSize = 50
	}
	path := fmt.Sprintf("/api/imagine?user_id=%s&page_size=%d", acct.UserID, pageSize)
	if cursor != "" {
		path += "&cursor=" + cursor
	}
	b, err := c.call(ctx, http.MethodGet, path, nil)
	if err != nil {
		return nil, "", err
	}
	return mjapi.ParseFeed(b)
}

package mjapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
)

// ExploreItem is a published community image from the explore feed or vector
// search (`/api/explore` and `/api/explore-vector-search`).
type ExploreItem struct {
	ID          JobID  `json:"id"`
	UserID      string `json:"user_id"`
	Username    string `json:"username"`
	DisplayName string `json:"display_name"`
	JobType     string `json:"job_type"`
	Type        string `json:"type,omitempty"` // "image" | "video"
	Video       bool   `json:"video,omitempty"`
	Sref        string `json:"sref,omitempty"` // style-reference code (explore-srefs)
	Version     string `json:"version"`
	AR          string `json:"ar,omitempty"`
	Width       int    `json:"width"`
	Height      int    `json:"height"`
	BatchSize   int    `json:"batch_size,omitempty"` // number of grid cells
	Prompt      string `json:"prompt"`               // human-readable text only
	Command     string `json:"command,omitempty"`    // full reproducible prompt + flags
	LikedByUser bool   `json:"liked_by_user"`
	ParentID    string `json:"parent_id,omitempty"`
	ParentGrid  string `json:"parent_grid,omitempty"`
	EnqueueTime int64  `json:"enqueue_time,omitempty"` // unix ms
	Published   bool   `json:"published"`
	ImageURL    string `json:"image_url"`
}

type feedJobRaw struct {
	ID          string          `json:"id"`
	JobType     string          `json:"job_type"`
	UserID      string          `json:"user_id"`
	Username    string          `json:"username_v2"`
	DisplayName string          `json:"display_name"`
	Width       int             `json:"width"`
	Height      int             `json:"height"`
	Type        string          `json:"type"`
	ParentID    flexStr         `json:"parent_id"`
	ParentGrid  flexStr         `json:"parent_grid"`
	EnqueueTime int64           `json:"enqueue_time"`
	Published   bool            `json:"published"`
	Sref        string          `json:"formatted_sref"`
	BatchSize   int             `json:"batch_size"`
	Items       []feedItem      `json:"items"`
	Prompt      json.RawMessage `json:"prompt"`
}

type feedItem struct {
	LikedByUser bool `json:"liked_by_user"`
}

// flexStr accepts a JSON string, number, or null and stores it as a string.
// Several feed fields (e.g. parent_grid) vary between string and numeric forms.
type flexStr string

func (f *flexStr) UnmarshalJSON(b []byte) error {
	s := strings.TrimSpace(string(b))
	if s == "null" || s == "" {
		*f = ""
		return nil
	}
	if len(s) >= 2 && s[0] == '"' {
		var str string
		if err := json.Unmarshal(b, &str); err != nil {
			return err
		}
		*f = flexStr(str)
		return nil
	}
	*f = flexStr(strings.Trim(s, `"`)) // number or bare token
	return nil
}

type feedStyleRef struct {
	Content string `json:"content"`
}

type feedPromptObj struct {
	DecodedPrompt []struct {
		Content string `json:"content"`
	} `json:"decodedPrompt"`
	Version  string         `json:"version"`
	No       []string       `json:"no"`
	StyleRef []feedStyleRef `json:"styleRef"`
	StyleRaw bool           `json:"styleRaw"`
	Tile     bool           `json:"tile"`
	Video    bool           `json:"video"`
	Stylize  *int           `json:"stylize"`
	Chaos    *int           `json:"chaos"`
	Weird    *int           `json:"weird"`
	Quality  *float64       `json:"quality"`
	Seed     *int64         `json:"seed"`
	Stop     *int           `json:"stop"`
	Sw       *int           `json:"sw"`
	Sv       *int           `json:"sv"`
	AR       struct {
		W int `json:"w"`
		H int `json:"h"`
	} `json:"ar"`
}

// ParseExplore parses an explore / vector-search response (a JSON array of feed
// jobs) into flat ExploreItems.
func ParseExplore(raw []byte) ([]ExploreItem, error) {
	var arr []feedJobRaw
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil, fmt.Errorf("parse explore: %w", err)
	}
	out := make([]ExploreItem, 0, len(arr))
	for _, f := range arr {
		it := ExploreItem{
			ID: JobID(f.ID), UserID: f.UserID, Username: f.Username,
			DisplayName: f.DisplayName, JobType: f.JobType, Type: f.Type,
			Width: f.Width, Height: f.Height, ParentID: string(f.ParentID), ParentGrid: string(f.ParentGrid),
			EnqueueTime: f.EnqueueTime, Published: f.Published, Sref: f.Sref,
			ImageURL: fmt.Sprintf("%s/%s/0_0.png", CDNBase, f.ID),
		}
		it.BatchSize = len(f.Items)
		if it.BatchSize == 0 {
			it.BatchSize = f.BatchSize // fall back to a top-level count when items[] is absent
		}
		for _, item := range f.Items {
			if item.LikedByUser {
				it.LikedByUser = true
				break
			}
		}
		text, params, version, isVideo := decodePrompt(f.Prompt)
		it.Prompt = text
		it.Version = version
		it.AR = params.AR
		it.Command = params.BuildPrompt(text)
		if f.Type == "video" || isVideo {
			it.Video = true
			it.ImageURL = fmt.Sprintf("%s/video/%s/0.mp4", CDNBase, f.ID)
		}
		out = append(out, it)
	}
	return out, nil
}

// decodePrompt handles the explore `prompt` field, which is usually an object
// ({decodedPrompt, version, ar, ...params}) but may be a plain string. It
// returns the human text, the reconstructed generation Params, the version, and
// whether the job is a video.
func decodePrompt(raw json.RawMessage) (text string, params Params, version string, isVideo bool) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return "", Params{}, "", false
	}
	// Object form: any JSON object is treated as the structured prompt, even when
	// it carries only params (no decodedPrompt/version) — so we never drop data.
	if raw[0] == '{' {
		var obj feedPromptObj
		if err := json.Unmarshal(raw, &obj); err == nil {
			parts := make([]string, 0, len(obj.DecodedPrompt))
			for _, d := range obj.DecodedPrompt {
				if d.Content != "" {
					parts = append(parts, d.Content)
				}
			}
			p := Params{
				Version: obj.Version, No: obj.No, StyleRaw: obj.StyleRaw, Tile: obj.Tile,
				Stylize: obj.Stylize, Chaos: obj.Chaos, Weird: obj.Weird,
				Quality: obj.Quality, Seed: obj.Seed, Sw: obj.Sw, Sv: obj.Sv, Stop: obj.Stop,
			}
			if obj.AR.W > 0 && obj.AR.H > 0 {
				p.AR = fmt.Sprintf("%d:%d", obj.AR.W, obj.AR.H)
			}
			for _, s := range obj.StyleRef {
				if s.Content != "" {
					p.Sref = append(p.Sref, s.Content)
				}
			}
			return strings.Join(parts, " "), p, obj.Version, obj.Video
		}
	}
	var s string
	if err := json.Unmarshal(raw, &s); err == nil {
		return s, Params{}, "", false
	}
	return "", Params{}, "", false
}

// Moodboard is a personalization moodboard (`/api/moodboards`).
type Moodboard struct {
	ID          string `json:"moodboard_id"`
	Title       string `json:"title"`
	Personalize bool   `json:"personalize"`
	Created     string `json:"created"`
	ImageCount  int    `json:"image_count"`
}

type moodboardRaw struct {
	ID          string            `json:"moodboard_id"`
	Title       string            `json:"title"`
	Personalize bool              `json:"personalize"`
	Created     string            `json:"created"`
	Images      []json.RawMessage `json:"images"`
}

// ParseMoodboards parses `/api/moodboards`.
func ParseMoodboards(raw []byte) ([]Moodboard, error) {
	var arr []moodboardRaw
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil, fmt.Errorf("parse moodboards: %w", err)
	}
	out := make([]Moodboard, 0, len(arr))
	for _, m := range arr {
		out = append(out, Moodboard{
			ID: m.ID, Title: m.Title, Personalize: m.Personalize,
			Created: m.Created, ImageCount: len(m.Images),
		})
	}
	return out, nil
}

// Upload is a user-uploaded asset in personal storage (`/api/storage`).
type Upload struct {
	BucketPathname string `json:"bucket_pathname"`
	ContentType    string `json:"content_type,omitempty"`
	State          string `json:"state,omitempty"`
	TimeCreated    int64  `json:"time_created,omitempty"` // unix ms
	Hidden         bool   `json:"hidden,omitempty"`
	URL            string `json:"url"` // hosted cdn/u/ URL
}

type uploadRaw struct {
	State          string `json:"state"`
	BucketPathname string `json:"bucketPathname"`
	ShortURL       string `json:"shortUrl"`
	TimeCreated    int64  `json:"timeCreated"`
	Hidden         bool   `json:"hidden"`
	ContentType    string `json:"cleanedContentType"`
}

// ParseStorage parses `/api/storage` into the user's uploaded assets.
func ParseStorage(raw []byte) ([]Upload, error) {
	var arr []uploadRaw
	if err := json.Unmarshal(raw, &arr); err != nil {
		return nil, fmt.Errorf("parse storage: %w", err)
	}
	out := make([]Upload, 0, len(arr))
	for _, u := range arr {
		up := Upload{
			BucketPathname: u.BucketPathname, ContentType: u.ContentType,
			State: u.State, TimeCreated: u.TimeCreated, Hidden: u.Hidden,
			URL: u.ShortURL,
		}
		if up.URL == "" && u.BucketPathname != "" {
			up.URL = fmt.Sprintf("%s/u/%s", CDNBase, u.BucketPathname)
		}
		out = append(out, up)
	}
	return out, nil
}

// Queue is the user's current job queue (`/api/user-queue`).
type Queue struct {
	Running []Job `json:"running"`
	Waiting []Job `json:"waiting"`
}

type queueRaw struct {
	Running []queueJob `json:"running"`
	Waiting []queueJob `json:"waiting"`
}

type queueJob struct {
	ID            string `json:"id"`
	JobType       string `json:"job_type"`
	EventType     string `json:"event_type"`
	CurrentStatus string `json:"current_status"`
	FullCommand   string `json:"full_command"`
	BatchSize     int    `json:"batch_size"`
}

// ParseQueue parses `/api/user-queue`.
func ParseQueue(raw []byte) (Queue, error) {
	var q queueRaw
	if err := json.Unmarshal(raw, &q); err != nil {
		return Queue{}, fmt.Errorf("parse queue: %w", err)
	}
	conv := func(in []queueJob) []Job {
		out := make([]Job, 0, len(in))
		for _, j := range in {
			out = append(out, Job{
				ID: JobID(j.ID), Status: Status(j.CurrentStatus), JobType: j.JobType,
				EventType: j.EventType, FullCommand: j.FullCommand, BatchSize: j.BatchSize,
			})
		}
		return out
	}
	return Queue{Running: conv(q.Running), Waiting: conv(q.Waiting)}, nil
}

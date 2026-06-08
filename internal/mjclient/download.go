package mjclient

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/ehmo/mj/internal/mjapi"
)

// AssetSel selects which CDN assets to download.
type AssetSel string

const (
	SelCells AssetSel = "cells" // individual grid cells 0_0.png..0_N.png
	SelGrid  AssetSel = "grid"  // composite grid_0.png only
	SelAll   AssetSel = "all"   // cells + grid
)

// Assets returns the derived CDN URLs for a completed job (see mjapi.Assets).
func (c *Client) Assets(j mjapi.Job) []mjapi.AssetURL { return mjapi.Assets(j) }

// cdnFetchJS fetches a URL inside the browser and returns the body base64. The
// CDN is Cloudflare-JA3-gated, so a plain Go client gets 403; the browser's
// genuine fingerprint passes. Default (non-credentialed) fetch so the CDN's
// wildcard CORS applies.
const cdnFetchJS = `async ({url}) => {
  try {
    const r = await fetch(url, {method: 'GET'});
    if (!r.ok) return {status: r.status, b64: '', error: 'status ' + r.status};
    const blob = await r.blob();
    const dataUrl = await new Promise((resolve, reject) => {
      const fr = new FileReader();
      fr.onloadend = () => resolve(fr.result);
      fr.onerror = () => reject(fr.error);
      fr.readAsDataURL(blob);
    });
    const comma = dataUrl.indexOf(',');
    return {status: r.status, b64: comma >= 0 ? dataUrl.slice(comma + 1) : '', error: ''};
  } catch (e) { return {status: 0, b64: '', error: String(e)}; }
}`

// cdnFetchRangeJS fetches a byte range [start,end] of url inside the browser and
// returns the chunk base64 plus the total size (from Content-Range). A 200 means
// the CDN ignored the Range header and returned the whole body in one shot.
const cdnFetchRangeJS = `async ({url, start, end}) => {
  try {
    const r = await fetch(url, {method: 'GET', headers: {'Range': 'bytes=' + start + '-' + end}});
    if (r.status !== 200 && r.status !== 206) return {status: r.status, total: 0, b64: '', error: 'status ' + r.status};
    let total = 0;
    const cr = r.headers.get('Content-Range');
    if (cr) { const m = cr.match(/\/(\d+)\s*$/); if (m) total = parseInt(m[1], 10); }
    if (!total) { const cl = r.headers.get('Content-Length'); if (cl) total = parseInt(cl, 10); }
    const blob = await r.blob();
    const dataUrl = await new Promise((resolve, reject) => {
      const fr = new FileReader();
      fr.onloadend = () => resolve(fr.result);
      fr.onerror = () => reject(fr.error);
      fr.readAsDataURL(blob);
    });
    const comma = dataUrl.indexOf(',');
    return {status: r.status, total: total, b64: comma >= 0 ? dataUrl.slice(comma + 1) : '', error: ''};
  } catch (e) { return {status: 0, total: 0, b64: '', error: String(e)}; }
}`

// browserGet fetches url through the browser and returns the decoded bytes.
func (c *Client) browserGet(ctx context.Context, url string) ([]byte, int, error) {
	var out struct {
		Status int    `json:"status"`
		B64    string `json:"b64"`
		Error  string `json:"error"`
	}
	raw, err := c.conn.Eval(ctx, cdnFetchJS, map[string]any{"url": url})
	if err != nil {
		return nil, 0, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, 0, err
	}
	if out.Status < 200 || out.Status >= 300 {
		reason := out.Error
		if reason == "" {
			reason = fmt.Sprintf("status %d", out.Status)
		}
		return nil, out.Status, fmt.Errorf("cdn fetch %s: %s", url, reason)
	}
	b, err := base64.StdEncoding.DecodeString(out.B64)
	return b, out.Status, err
}

// CDNHead reports whether a job's first asset is available on the CDN yet
// (propagation check), fetched through the browser.
func (c *Client) CDNHead(ctx context.Context, j mjapi.Job) bool {
	assets := mjapi.Assets(j)
	if len(assets) == 0 {
		return false
	}
	_, status, err := c.browserGet(ctx, assets[0].URL)
	return err == nil && status == 200
}

// Download fetches selected full-res PNG CDN assets (back-compat wrapper).
func (c *Client) Download(ctx context.Context, j mjapi.Job, destDir string, which AssetSel) ([]string, error) {
	return c.DownloadOpts(ctx, j, destDir, which, mjapi.AssetOpts{})
}

// DownloadOpts fetches selected CDN assets for a completed job through the
// browser in the requested format/size and writes them to destDir. Returns the
// written file paths. Missing assets are retried briefly to absorb CDN
// propagation lag.
func (c *Client) DownloadOpts(ctx context.Context, j mjapi.Job, destDir string, which AssetSel, opts mjapi.AssetOpts) ([]string, error) {
	if which == "" {
		which = SelAll
	}
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return nil, err
	}
	var want []mjapi.AssetURL
	for _, a := range mjapi.AssetsOpts(j, opts) {
		switch which {
		case SelCells:
			if a.Kind == mjapi.AssetCell || a.Kind == mjapi.AssetThumb || a.Kind == mjapi.AssetVideo {
				want = append(want, a)
			}
		case SelGrid:
			if a.Kind == mjapi.AssetGrid {
				want = append(want, a)
			}
		default:
			want = append(want, a)
		}
	}
	if len(want) == 0 {
		return nil, fmt.Errorf("download: no assets for job %s", j.ID)
	}
	var paths []string
	for _, a := range want {
		dest := filepath.Join(destDir, assetFilename(j.ID, a))
		// Videos can be large; stream them to disk in bounded-memory chunks.
		// Images stay on the simple whole-body path.
		if a.Kind == mjapi.AssetVideo {
			if err := c.streamURLToFile(ctx, a.URL, dest, streamChunkSize); err != nil {
				return paths, err
			}
			paths = append(paths, dest)
			continue
		}
		b, err := c.fetchWithRetry(ctx, a.URL)
		if err != nil {
			return paths, err
		}
		if err := os.WriteFile(dest, b, 0o644); err != nil {
			return paths, err
		}
		paths = append(paths, dest)
	}
	return paths, nil
}

// streamChunkSize is the per-range byte window for streamed (video) downloads —
// large enough to amortize the eval round-trip, small enough to bound memory.
const streamChunkSize = 4 << 20 // 4 MiB

// streamMaxChunks bounds the loop against a misbehaving server (4 MiB * 4096 =
// 16 GiB, far beyond any Midjourney asset).
const streamMaxChunks = 4096

// browserGetRange fetches a byte range of url in-browser, returning the chunk,
// the HTTP status (200 = Range ignored, 206 = partial), and the total size.
func (c *Client) browserGetRange(ctx context.Context, url string, start, end int64) (data []byte, status int, total int64, err error) {
	var out struct {
		Status int    `json:"status"`
		Total  int64  `json:"total"`
		B64    string `json:"b64"`
		Error  string `json:"error"`
	}
	raw, err := c.conn.Eval(ctx, cdnFetchRangeJS, map[string]any{"url": url, "start": start, "end": end})
	if err != nil {
		return nil, 0, 0, err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil, 0, 0, err
	}
	if out.Status != 200 && out.Status != 206 {
		reason := out.Error
		if reason == "" {
			reason = fmt.Sprintf("status %d", out.Status)
		}
		return nil, out.Status, 0, fmt.Errorf("cdn range %s: %s", url, reason)
	}
	b, derr := base64.StdEncoding.DecodeString(out.B64)
	return b, out.Status, out.Total, derr
}

func (c *Client) fetchRangeWithRetry(ctx context.Context, url string, start, end int64) (data []byte, status int, total int64, err error) {
	const maxAttempts = 5
	backoff := time.Second
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return nil, 0, 0, ctx.Err()
			}
			backoff *= 2
		}
		data, status, total, err = c.browserGetRange(ctx, url, start, end)
		if err == nil {
			return data, status, total, nil
		}
		if !retryableStatus(status) {
			return nil, status, 0, err
		}
	}
	return nil, status, 0, err
}

// streamURLToFile downloads url to dest in bounded-memory chunks via HTTP Range
// requests so peak memory is one chunk rather than the whole file (used for
// videos). It first probes with a single ranged request: the Midjourney CDN is
// cross-origin and its CORS policy rejects the Range header (the preflight
// fails), so in practice this falls back to the proven whole-body download. The
// streaming path is taken only when the CDN actually serves a 206. A partial
// file is removed on error.
func (c *Client) streamURLToFile(ctx context.Context, url, dest string, chunkSize int) error {
	if chunkSize <= 0 {
		chunkSize = streamChunkSize
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	// Single probe (no retry): a CORS/network failure here means ranged fetches
	// aren't allowed — fall back to the whole-body path rather than retry-storming.
	data, status, total, err := c.browserGetRange(ctx, url, 0, int64(chunkSize)-1)
	if err != nil {
		return c.saveWholeBody(ctx, url, dest)
	}
	f, err := os.Create(dest)
	if err != nil {
		return err
	}
	complete := false
	defer func() {
		f.Close()
		if !complete {
			os.Remove(dest) // don't leave a truncated/corrupt file behind
		}
	}()
	off := int64(0)
	for i := 0; i < streamMaxChunks; i++ {
		if i > 0 {
			data, status, total, err = c.fetchRangeWithRetry(ctx, url, off, off+int64(chunkSize)-1)
			if err != nil {
				return err
			}
		}
		if _, err := f.Write(data); err != nil {
			return err
		}
		off += int64(len(data))
		switch {
		case status == 200: // Range ignored: whole body delivered in one shot
			complete = true
			return nil
		case len(data) == 0:
			complete = true
			return nil
		case total > 0 && off >= total: // reached the declared size
			complete = true
			return nil
		case total == 0 && int64(len(data)) < int64(chunkSize): // short read, no total
			complete = true
			return nil
		}
	}
	return fmt.Errorf("stream %s: exceeded %d chunks", url, streamMaxChunks)
}

// saveWholeBody downloads url through the browser (whole body, no Range) and
// writes it to dest — the fallback when ranged streaming isn't permitted.
func (c *Client) saveWholeBody(ctx context.Context, url, dest string) error {
	b, err := c.fetchWithRetry(ctx, url)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dest, b, 0o644)
}

// SaveURL fetches an arbitrary CDN URL through the browser and writes it to
// destPath (creating parent dirs). Used for assets without a job (uploads).
func (c *Client) SaveURL(ctx context.Context, url, destPath string) error {
	b, err := c.fetchWithRetry(ctx, url)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(destPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(destPath, b, 0o644)
}

// fetchWithRetry fetches with exponential backoff, retrying only transient
// conditions: a fresh job's asset 404s until the CDN propagates (~seconds), and
// network blips / 5xx are temporary. A 403/410/400 is permanent for that URL
// (wrong path, private/moderated job) and fails immediately rather than burning
// the full backoff window.
func (c *Client) fetchWithRetry(ctx context.Context, url string) ([]byte, error) {
	const maxAttempts = 5
	var lastErr error
	backoff := time.Second
	for attempt := 0; attempt < maxAttempts; attempt++ {
		if attempt > 0 {
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			backoff *= 2
		}
		b, status, err := c.browserGet(ctx, url)
		if err == nil {
			return b, nil
		}
		lastErr = err
		if !retryableStatus(status) {
			return nil, err
		}
	}
	return nil, lastErr
}

// retryableStatus reports whether an HTTP status warrants a download retry.
// status 0 means a network-level failure (no response).
func retryableStatus(status int) bool {
	switch status {
	case 0, 404, 408, 425, 429, 500, 502, 503, 504:
		return true
	}
	return false
}

func assetFilename(id mjapi.JobID, a mjapi.AssetURL) string {
	base := safeIDBase(id)
	ext := urlExt(a.URL) // ".png" | ".webp" | ".mp4"
	if ext == "" {
		ext = ".png"
	}
	switch a.Kind {
	case mjapi.AssetGrid:
		return base + "_grid" + ext
	case mjapi.AssetCell, mjapi.AssetThumb:
		return fmt.Sprintf("%s_%d%s", base, a.Index, ext)
	case mjapi.AssetVideo:
		return fmt.Sprintf("%s_%d.mp4", base, a.Index)
	default:
		return base + "_" + path.Base(stripQuery(a.URL))
	}
}

// safeIDBase makes a job id safe to use as a filename component. Ids come from
// Midjourney (UUID-shaped), but explore/search/profile results are other users'
// content, so this is defense in depth: keep [A-Za-z0-9._-], map anything else
// (path separators included) to '_', and forbid leading dots so a crafted id
// can't write outside the destination dir or create a dotfile.
func safeIDBase(id mjapi.JobID) string {
	s := strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_', r == '.':
			return r
		default:
			return '_'
		}
	}, string(id))
	s = strings.TrimLeft(s, ".")
	if s == "" {
		return "job"
	}
	return s
}

// urlExt returns the file extension of a URL path, ignoring any query string
// (filepath.Ext would return e.g. ".jpg?v=1" for a versioned CDN URL).
func urlExt(u string) string { return path.Ext(stripQuery(u)) }

func stripQuery(u string) string {
	if i := strings.IndexByte(u, '?'); i >= 0 {
		return u[:i]
	}
	return u
}

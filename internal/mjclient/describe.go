package mjclient

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/ehmo/mj/internal/mjapi"
)

// Describe returns ~4 suggested prompts for an image. imageURL must be a public
// URL Midjourney can fetch (e.g. a cdn.midjourney.com image or any web image).
// /api/describe is synchronous — no job polling. Confirmed live 2026-06-05.
func (c *Client) Describe(ctx context.Context, imageURL string) ([]string, error) {
	body, _ := json.Marshal(map[string]string{"image_url": imageURL})
	b, err := c.PostJSON(ctx, "/api/describe", body)
	if err != nil {
		return nil, err
	}
	return mjapi.ParseDescribe(b)
}

// DescribeImage describes a local file or a URL. URLs are described directly;
// local files are uploaded first.
func (c *Client) DescribeImage(ctx context.Context, pathOrURL string) ([]string, error) {
	if isURL(pathOrURL) {
		return c.Describe(ctx, pathOrURL)
	}
	url, err := c.UploadImage(ctx, pathOrURL)
	if err != nil {
		return nil, fmt.Errorf("upload: %w", err)
	}
	return c.Describe(ctx, url)
}

func isURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

const uploadJS = `async ({b64, name, mime, field}) => {
  try {
    const bin = atob(b64);
    const bytes = new Uint8Array(bin.length);
    for (let i = 0; i < bin.length; i++) bytes[i] = bin.charCodeAt(i);
    const blob = new Blob([bytes], {type: mime});
    const fd = new FormData();
    fd.append(field, blob, name);
    const r = await fetch('/api/storage-upload-file', {
      method: 'POST', credentials: 'include', headers: {'x-csrf-protection': '1'}, body: fd});
    let j = {};
    try { j = await r.json(); } catch (e) {}
    return {status: r.status, shortUrl: j.shortUrl || '', bucketPathname: j.bucketPathname || '', error: j.error || ''};
  } catch (e) { return {status: 0, error: String(e)}; }
}`

// UploadImage uploads a local image to Midjourney's user storage and returns a
// public URL usable as an image_url / image prompt. Uses an in-browser multipart
// POST to /api/storage-upload-file.
func (c *Client) UploadImage(ctx context.Context, localPath string) (string, error) {
	data, err := os.ReadFile(localPath)
	if err != nil {
		return "", err
	}
	// Ensure the MJ session cookie exists before the credentialed in-browser
	// upload (uploadJS uses credentials:include).
	if err := c.ensureAuthed(ctx); err != nil {
		return "", err
	}
	mime := mimeForExt(localPath)
	args := map[string]any{
		"b64":   base64.StdEncoding.EncodeToString(data),
		"name":  filepath.Base(localPath),
		"mime":  mime,
		"field": "file",
	}
	var out struct {
		Status         int    `json:"status"`
		ShortURL       string `json:"shortUrl"`
		BucketPathname string `json:"bucketPathname"`
		Error          string `json:"error"`
	}
	raw, err := c.conn.Eval(ctx, uploadJS, args)
	if err != nil {
		return "", err
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", err
	}
	if out.Status != http.StatusOK || out.BucketPathname == "" {
		reason := out.Error
		if reason == "" {
			reason = fmt.Sprintf("status %d", out.Status)
		}
		return "", fmt.Errorf("storage-upload-file: %s", reason)
	}
	return fmt.Sprintf("%s/u/%s", mjapi.CDNBase, out.BucketPathname), nil
}

func mimeForExt(p string) string {
	switch strings.ToLower(filepath.Ext(p)) {
	case ".png":
		return "image/png"
	case ".webp":
		return "image/webp"
	case ".gif":
		return "image/gif"
	default:
		return "image/jpeg"
	}
}

// UploadIfLocal returns pathOrURL unchanged if it is a URL, otherwise uploads
// the local file and returns its hosted URL.
func (c *Client) UploadIfLocal(ctx context.Context, pathOrURL string) (string, error) {
	if isURL(pathOrURL) {
		return pathOrURL, nil
	}
	return c.UploadImage(ctx, pathOrURL)
}

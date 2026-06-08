package mjclient

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/jpeg" // decode JPEG sources
	"image/png"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"

	"github.com/ehmo/mj/internal/mjapi"
)

// Retexture re-renders an image's texture/style from a text instruction while
// keeping its structure, using the source as a depth reference (--dref).
// Confirmed live: t:"retexture" with the source uploaded and referenced as
// --dref in the prompt. source may be a local file or a URL.
func (c *Client) Retexture(ctx context.Context, source, prompt, ar string, mode mjapi.Mode) (mjapi.Job, error) {
	url, err := c.UploadIfLocal(ctx, source)
	if err != nil {
		return mjapi.Job{}, fmt.Errorf("upload source: %w", err)
	}
	parts := []string{strings.TrimSpace(prompt)}
	if ar != "" {
		parts = append(parts, "--ar", ar)
	}
	parts = append(parts, "--dref", url, "--v", "7")
	full := strings.TrimSpace(strings.Join(parts, " "))
	return c.submitOne(ctx, mode, false, "retexture", map[string]any{"prompt": full})
}

// --- canvas editor (outpaint / pan / zoom-out / vary-region) ---
//
// Captured live: pan, zoom-out and vary-region all post the SAME verb,
// t:"uploaded", with frame == image == the full composite size and an imageUrl
// pointing at a pre-composited PNG. Midjourney bakes the operation into that
// upload client-side: the source photo is drawn into a transparent canvas
// (extended margins for pan/zoom, erased holes for vary-region) and the server
// fills every transparent pixel from the prompt. We replicate that compositing
// here, then submit the uploaded frame.

// Outpaint extends the canvas around a source image by the given pixel margins
// and regenerates the new (transparent) area from prompt. Pan = margin on one
// side; zoom-out = margins on all sides. source is a local file or a URL.
func (c *Client) Outpaint(ctx context.Context, source, prompt string, left, top, right, bottom int, mode mjapi.Mode) (mjapi.Job, error) {
	if left < 0 || top < 0 || right < 0 || bottom < 0 || (left+top+right+bottom) == 0 {
		return mjapi.Job{}, fmt.Errorf("outpaint margins must be non-negative and sum > 0")
	}
	src, err := c.loadImage(ctx, source)
	if err != nil {
		return mjapi.Job{}, err
	}
	b := src.Bounds()
	w, h := b.Dx()+left+right, b.Dy()+top+bottom
	canvas := image.NewNRGBA(image.Rect(0, 0, w, h))
	draw.Draw(canvas, image.Rect(left, top, left+b.Dx(), top+b.Dy()), src, b.Min, draw.Src)
	return c.submitCanvas(ctx, canvas, prompt, mode)
}

// VaryRegion erases a rectangular region of the source (making it transparent)
// and regenerates just that region from prompt, keeping the rest. source is a
// local file or a URL; the rect is in source pixels.
func (c *Client) VaryRegion(ctx context.Context, source, prompt string, x, y, rw, rh int, mode mjapi.Mode) (mjapi.Job, error) {
	if rw <= 0 || rh <= 0 {
		return mjapi.Job{}, fmt.Errorf("region width and height must be > 0")
	}
	src, err := c.loadImage(ctx, source)
	if err != nil {
		return mjapi.Job{}, err
	}
	b := src.Bounds()
	canvas := image.NewNRGBA(image.Rect(0, 0, b.Dx(), b.Dy()))
	draw.Draw(canvas, canvas.Bounds(), src, b.Min, draw.Src)
	// punch a transparent hole (clamped to bounds) for the server to fill
	transparent := color.NRGBA{}
	hole := image.Rect(x, y, x+rw, y+rh).Intersect(canvas.Bounds())
	if hole.Empty() {
		return mjapi.Job{}, fmt.Errorf("region is outside the image bounds")
	}
	for py := hole.Min.Y; py < hole.Max.Y; py++ {
		for px := hole.Min.X; px < hole.Max.X; px++ {
			canvas.SetNRGBA(px, py, transparent)
		}
	}
	return c.submitCanvas(ctx, canvas, prompt, mode)
}

// dimHintRE extracts the canonical canvas size the server requires for an AR,
// e.g. `Invalid frame dimensions ... Should be 1024x1024`.
var dimHintRE = regexp.MustCompile(`[Ss]hould be (\d+)\s*[x×]\s*(\d+)`)

// submitCanvas encodes the composite as PNG, uploads it, and submits the
// "uploaded" editor verb (frame == image == canvas size). Midjourney requires
// canonical per-AR pixel dimensions; if the server rejects ours it reports the
// exact size, so we scale the composite to match and resubmit once.
func (c *Client) submitCanvas(ctx context.Context, canvas image.Image, prompt string, mode mjapi.Mode) (mjapi.Job, error) {
	job, err := c.submitUploaded(ctx, canvas, prompt, mode)
	if err == nil {
		return job, nil
	}
	// The server reports invalid editor frame sizes inside a 200-OK failure body
	// (a plain error string here), and names the canonical size it wants.
	m := dimHintRE.FindStringSubmatch(err.Error())
	if m == nil {
		return mjapi.Job{}, err
	}
	w, _ := strconv.Atoi(m[1])
	h, _ := strconv.Atoi(m[2])
	if w <= 0 || h <= 0 {
		return mjapi.Job{}, err
	}
	return c.submitUploaded(ctx, scaleImage(canvas, w, h), prompt, mode)
}

func (c *Client) submitUploaded(ctx context.Context, canvas image.Image, prompt string, mode mjapi.Mode) (mjapi.Job, error) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, canvas); err != nil {
		return mjapi.Job{}, fmt.Errorf("encode composite: %w", err)
	}
	url, err := c.UploadBytes(ctx, buf.Bytes(), "edit.png")
	if err != nil {
		return mjapi.Job{}, fmt.Errorf("upload composite: %w", err)
	}
	w, h := canvas.Bounds().Dx(), canvas.Bounds().Dy()
	full := strings.TrimSpace(strings.TrimSpace(prompt) + fmt.Sprintf(" --ar %s --v 7", mjapi.AspectRatio(w, h)))
	return c.submitOne(ctx, mode, false, "uploaded", map[string]any{
		"prompt":   full,
		"frame":    map[string]any{"width": w, "height": h},
		"image":    map[string]any{"x": 0, "y": 0, "width": w, "height": h},
		"imageUrl": url,
	})
}

// scaleImage resizes an image to dstW x dstH with bilinear interpolation,
// preserving the alpha channel (transparent regions stay transparent).
func scaleImage(src image.Image, dstW, dstH int) image.Image {
	sb := src.Bounds()
	sw, sh := sb.Dx(), sb.Dy()
	if sw == dstW && sh == dstH {
		return src
	}
	dst := image.NewNRGBA(image.Rect(0, 0, dstW, dstH))
	if sw == 0 || sh == 0 {
		return dst
	}
	for y := 0; y < dstH; y++ {
		fy := (float64(y) + 0.5) * float64(sh) / float64(dstH)
		y0 := int(fy - 0.5)
		dy := fy - 0.5 - float64(y0)
		y1 := y0 + 1
		y0 = clampInt(y0, 0, sh-1)
		y1 = clampInt(y1, 0, sh-1)
		for x := 0; x < dstW; x++ {
			fx := (float64(x) + 0.5) * float64(sw) / float64(dstW)
			x0 := int(fx - 0.5)
			dx := fx - 0.5 - float64(x0)
			x1 := x0 + 1
			x0 = clampInt(x0, 0, sw-1)
			x1 = clampInt(x1, 0, sw-1)
			c00 := nrgbaAt(src, sb.Min.X+x0, sb.Min.Y+y0)
			c10 := nrgbaAt(src, sb.Min.X+x1, sb.Min.Y+y0)
			c01 := nrgbaAt(src, sb.Min.X+x0, sb.Min.Y+y1)
			c11 := nrgbaAt(src, sb.Min.X+x1, sb.Min.Y+y1)
			dst.SetNRGBA(x, y, bilerp(c00, c10, c01, c11, dx, dy))
		}
	}
	return dst
}

func nrgbaAt(img image.Image, x, y int) color.NRGBA {
	c := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
	return c
}

func bilerp(c00, c10, c01, c11 color.NRGBA, dx, dy float64) color.NRGBA {
	lerp := func(a, b uint8, t float64) float64 { return float64(a) + (float64(b)-float64(a))*t }
	top := func(i func(color.NRGBA) uint8) float64 { return lerp(i(c00), i(c10), dx) }
	bot := func(i func(color.NRGBA) uint8) float64 { return lerp(i(c01), i(c11), dx) }
	mix := func(i func(color.NRGBA) uint8) uint8 {
		return uint8(top(i) + (bot(i)-top(i))*dy + 0.5)
	}
	return color.NRGBA{
		R: mix(func(c color.NRGBA) uint8 { return c.R }),
		G: mix(func(c color.NRGBA) uint8 { return c.G }),
		B: mix(func(c color.NRGBA) uint8 { return c.B }),
		A: mix(func(c color.NRGBA) uint8 { return c.A }),
	}
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// ImageSize returns the width and height of a source image (local file or URL).
func (c *Client) ImageSize(ctx context.Context, source string) (int, int, error) {
	img, err := c.loadImage(ctx, source)
	if err != nil {
		return 0, 0, err
	}
	b := img.Bounds()
	return b.Dx(), b.Dy(), nil
}

// loadImage fetches a source (local path or URL) and decodes it (PNG/JPEG).
func (c *Client) loadImage(ctx context.Context, source string) (image.Image, error) {
	var data []byte
	if isURL(source) {
		b, status, err := c.browserGet(ctx, source)
		if err != nil {
			return nil, fmt.Errorf("fetch source: %w", err)
		}
		if status != http.StatusOK {
			return nil, fmt.Errorf("fetch source: status %d", status)
		}
		data = b
	} else {
		b, err := os.ReadFile(source)
		if err != nil {
			return nil, err
		}
		data = b
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode source (png/jpeg only): %w", err)
	}
	return img, nil
}

// UploadBytes uploads raw image bytes to Midjourney storage and returns a hosted
// URL (the short s.mj.run URL when available, else the cdn/u/ form).
func (c *Client) UploadBytes(ctx context.Context, data []byte, name string) (string, error) {
	args := map[string]any{
		"b64":   base64.StdEncoding.EncodeToString(data),
		"name":  name,
		"mime":  mimeForExt(name),
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
	if out.Status != http.StatusOK {
		reason := out.Error
		if reason == "" {
			reason = fmt.Sprintf("status %d", out.Status)
		}
		return "", fmt.Errorf("storage-upload-file: %s", reason)
	}
	if out.ShortURL != "" {
		return out.ShortURL, nil
	}
	if out.BucketPathname != "" {
		return fmt.Sprintf("%s/u/%s", mjapi.CDNBase, out.BucketPathname), nil
	}
	return "", fmt.Errorf("storage-upload-file: empty response")
}

package mjclient

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

type rangeOut struct {
	Status int    `json:"status"`
	Total  int64  `json:"total"`
	B64    string `json:"b64"`
	Error  string `json:"error"`
}

// rangeConn is a fake pageConn that serves HTTP-Range-style responses for the
// streaming download test. supportRange=false simulates a CDN that ignores the
// Range header (returns the whole body, status 200). failAt>0 makes the Nth
// range call return a permanent error.
type rangeConn struct {
	data         []byte
	supportRange bool
	failAt       int
	calls        int
}

func (r *rangeConn) Fetch(ctx context.Context, req FetchReq) (FetchRes, error) {
	return FetchRes{}, nil
}
func (r *rangeConn) Close() error { return nil }
func (r *rangeConn) Eval(ctx context.Context, expr string, args ...any) (json.RawMessage, error) {
	r.calls++
	if r.failAt > 0 && r.calls >= r.failAt {
		return json.Marshal(rangeOut{Status: 403, Error: "forbidden"}) // non-retryable
	}
	m, _ := args[0].(map[string]any)
	start, end := asInt64(m["start"]), asInt64(m["end"])
	total := int64(len(r.data))
	if !r.supportRange {
		return json.Marshal(rangeOut{Status: 200, Total: total, B64: b64(r.data)})
	}
	if start >= total {
		return json.Marshal(rangeOut{Status: 416, Error: "range not satisfiable"})
	}
	if end >= total {
		end = total - 1
	}
	return json.Marshal(rangeOut{Status: 206, Total: total, B64: b64(r.data[start : end+1])})
}

func asInt64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case float64:
		return int64(n)
	}
	return 0
}
func b64(b []byte) string { return base64.StdEncoding.EncodeToString(b) }

func TestStreamURLToFileChunked(t *testing.T) {
	data := bytes.Repeat([]byte("abcdefgh"), 1000) // 8000 bytes
	rc := &rangeConn{data: data, supportRange: true}
	c := &Client{conn: rc}
	dest := filepath.Join(t.TempDir(), "v.mp4")
	if err := c.streamURLToFile(context.Background(), "https://cdn/x.mp4", dest, 1024); err != nil {
		t.Fatalf("stream: %v", err)
	}
	got, _ := os.ReadFile(dest)
	if !bytes.Equal(got, data) {
		t.Fatalf("content mismatch: got %d want %d bytes", len(got), len(data))
	}
	if rc.calls < 7 {
		t.Errorf("expected chunked range calls, got %d", rc.calls)
	}
}

func TestStreamURLToFileNoRangeSupport(t *testing.T) {
	data := bytes.Repeat([]byte("z"), 5000)
	rc := &rangeConn{data: data, supportRange: false}
	c := &Client{conn: rc}
	dest := filepath.Join(t.TempDir(), "v.mp4")
	if err := c.streamURLToFile(context.Background(), "u", dest, 1024); err != nil {
		t.Fatalf("stream: %v", err)
	}
	got, _ := os.ReadFile(dest)
	if !bytes.Equal(got, data) {
		t.Fatalf("whole-body fallback mismatch: %d vs %d", len(got), len(data))
	}
	if rc.calls != 1 {
		t.Errorf("no-range path should be a single call, got %d", rc.calls)
	}
}

// corsBlockedConn simulates the real Midjourney CDN: ranged fetches fail (CORS
// preflight rejection -> NetworkError, status 0), but the plain whole-body fetch
// succeeds. streamURLToFile must transparently fall back and still write the file.
type corsBlockedConn struct {
	data       []byte
	rangeCalls int
	wholeCalls int
}

func (c *corsBlockedConn) Fetch(ctx context.Context, req FetchReq) (FetchRes, error) {
	return FetchRes{}, nil
}
func (c *corsBlockedConn) Close() error { return nil }
func (c *corsBlockedConn) Eval(ctx context.Context, expr string, args ...any) (json.RawMessage, error) {
	if expr == cdnFetchRangeJS {
		c.rangeCalls++
		return json.Marshal(rangeOut{Status: 0, Error: "NetworkError when attempting to fetch resource"})
	}
	c.wholeCalls++
	return json.Marshal(struct {
		Status int    `json:"status"`
		B64    string `json:"b64"`
		Error  string `json:"error"`
	}{Status: 200, B64: b64(c.data)})
}

func TestStreamURLToFileFallsBackOnCORS(t *testing.T) {
	data := bytes.Repeat([]byte("mp4"), 4000)
	cc := &corsBlockedConn{data: data}
	c := &Client{conn: cc}
	dest := filepath.Join(t.TempDir(), "v.mp4")
	if err := c.streamURLToFile(context.Background(), "https://cdn/video/x/0.mp4", dest, 1024); err != nil {
		t.Fatalf("stream (with fallback): %v", err)
	}
	got, _ := os.ReadFile(dest)
	if !bytes.Equal(got, data) {
		t.Fatalf("fallback content mismatch: %d vs %d", len(got), len(data))
	}
	if cc.rangeCalls != 1 {
		t.Errorf("expected a single range probe (no retry storm), got %d", cc.rangeCalls)
	}
	if cc.wholeCalls < 1 {
		t.Errorf("whole-body fallback was not used")
	}
}

func TestStreamURLToFileErrorRemovesPartial(t *testing.T) {
	data := bytes.Repeat([]byte("q"), 8000)
	rc := &rangeConn{data: data, supportRange: true, failAt: 2} // first chunk ok, second fails
	c := &Client{conn: rc}
	dest := filepath.Join(t.TempDir(), "v.mp4")
	if err := c.streamURLToFile(context.Background(), "u", dest, 1024); err == nil {
		t.Fatal("expected an error from the failing range")
	}
	if _, err := os.Stat(dest); !os.IsNotExist(err) {
		t.Errorf("partial file should have been removed, stat err = %v", err)
	}
}

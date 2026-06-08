package mjclient

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"github.com/ehmo/mj/internal/mjapi"
)

// recordConn is a fake pageConn that records the time of each Fetch.
type recordConn struct {
	mu    sync.Mutex
	times []time.Time
}

func (f *recordConn) Fetch(ctx context.Context, req FetchReq) (FetchRes, error) {
	f.mu.Lock()
	f.times = append(f.times, time.Now())
	f.mu.Unlock()
	return FetchRes{StatusCode: 200, Body: []byte("{}")}, nil
}
func (f *recordConn) Eval(ctx context.Context, expr string, args ...any) (json.RawMessage, error) {
	return json.RawMessage("null"), nil
}
func (f *recordConn) Close() error { return nil }

func authedClient(conn pageConn, gap time.Duration) *Client {
	c := &Client{conn: conn, cfg: Config{MinSubmitGap: gap}}
	c.acct = &mjapi.Account{UserID: "u"}
	c.authedAt = time.Now()
	return c
}

// The throttle must apply to ANY submit-jobs POST (typed or raw api/PostJSON),
// because it is enforced at the call() choke point, not in submitOne.
func TestThrottleAtCallChokePoint(t *testing.T) {
	f := &recordConn{}
	c := authedClient(f, 120*time.Millisecond)
	ctx := context.Background()
	for i := 0; i < 2; i++ {
		if _, err := c.call(ctx, "POST", "/api/submit-jobs", []byte("{}")); err != nil {
			t.Fatalf("call %d: %v", i, err)
		}
	}
	if len(f.times) != 2 {
		t.Fatalf("want 2 fetches, got %d", len(f.times))
	}
	if gap := f.times[1].Sub(f.times[0]); gap < 100*time.Millisecond {
		t.Fatalf("submit-jobs not throttled: gap %v < 100ms", gap)
	}
}

// Non-submit endpoints must not be throttled, even right after a submit.
func TestThrottleSkipsNonSubmit(t *testing.T) {
	f := &recordConn{}
	c := authedClient(f, 2*time.Second)
	c.lastSubmit = time.Now() // pretend a submit just happened
	start := time.Now()
	if _, err := c.call(context.Background(), "POST", "/api/job-status", []byte("{}")); err != nil {
		t.Fatalf("call: %v", err)
	}
	if d := time.Since(start); d > 200*time.Millisecond {
		t.Fatalf("non-submit path was throttled: %v", d)
	}
}

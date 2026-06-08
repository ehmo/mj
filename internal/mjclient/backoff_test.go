package mjclient

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/ehmo/mj/internal/mjapi"
)

// statusConn returns a fixed HTTP status for every fetch.
type statusConn struct{ status int }

func (s *statusConn) Fetch(ctx context.Context, req FetchReq) (FetchRes, error) {
	return FetchRes{StatusCode: s.status, Body: []byte("{}")}, nil
}
func (s *statusConn) Eval(ctx context.Context, expr string, args ...any) (json.RawMessage, error) {
	return json.RawMessage("null"), nil
}
func (s *statusConn) Close() error { return nil }

// A 429 on submit-jobs grows the adaptive penalty; a success decays it; the
// penalty is capped.
func TestClientAdaptivePenalty(t *testing.T) {
	c := &Client{conn: &statusConn{status: 429}, cfg: Config{}}
	c.acct = &mjapi.Account{UserID: "u"}
	c.authedAt = time.Now()
	ctx := context.Background()

	// One real submit call returning 429 records the first penalty step.
	// (lastSubmit is zero, so the throttle doesn't actually wait here.)
	_, _ = c.call(ctx, "POST", "/api/submit-jobs", []byte("{}")) // returns APIError(429)
	if c.submitPenalty != submitPenaltyStep {
		t.Fatalf("penalty after 1x429 = %v, want %v", c.submitPenalty, submitPenaltyStep)
	}

	// Growth + cap (record directly to avoid the throttle's real-time waits).
	for i := 0; i < 100; i++ {
		c.recordSubmitStatus(429)
	}
	if c.submitPenalty != submitPenaltyMax {
		t.Fatalf("penalty not capped: %v", c.submitPenalty)
	}

	// Success decays it.
	c.recordSubmitStatus(200)
	if c.submitPenalty != submitPenaltyMax/2 {
		t.Fatalf("penalty after success = %v, want %v", c.submitPenalty, submitPenaltyMax/2)
	}

	// A non-submit path must not touch the penalty.
	before := c.submitPenalty
	_, _ = c.call(ctx, "POST", "/api/job-status", []byte("{}"))
	if c.submitPenalty != before {
		t.Errorf("non-submit path changed penalty: %v -> %v", before, c.submitPenalty)
	}
}

// The throttle gap must include the adaptive penalty.
func TestClientThrottleUsesPenalty(t *testing.T) {
	c := &Client{conn: &statusConn{status: 200}, cfg: Config{MinSubmitGap: 50 * time.Millisecond}}
	c.acct = &mjapi.Account{UserID: "u"}
	c.authedAt = time.Now()
	c.submitPenalty = 150 * time.Millisecond
	c.lastSubmit = time.Now()
	start := time.Now()
	if err := c.throttleSubmit(context.Background()); err != nil {
		t.Fatal(err)
	}
	// Expect to have waited ~ gap (200ms), well above the bare 50ms MinSubmitGap.
	if waited := time.Since(start); waited < 150*time.Millisecond {
		t.Fatalf("throttle ignored penalty: waited %v", waited)
	}
}

func TestDaemonAdaptivePenalty(t *testing.T) {
	s := &daemonState{submitGap: 0}
	s.recordSubmitStatus(429)
	s.recordSubmitStatus(429)
	if s.penalty != 2*submitPenaltyStep {
		t.Fatalf("daemon penalty = %v, want %v", s.penalty, 2*submitPenaltyStep)
	}
	s.recordSubmitStatus(200)
	if s.penalty != submitPenaltyStep {
		t.Fatalf("daemon penalty after success = %v, want %v", s.penalty, submitPenaltyStep)
	}
}

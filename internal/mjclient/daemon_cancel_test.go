package mjclient

import (
	"context"
	"encoding/json"
	"net"
	"sync"
	"testing"
	"time"
)

// blockingConn is a pageConn whose Eval blocks until its context is cancelled,
// so a test can verify the daemon cancels in-flight ops when a client vanishes.
type blockingConn struct {
	started   chan struct{}
	cancelled chan struct{}
	once      sync.Once
}

func (b *blockingConn) Fetch(ctx context.Context, req FetchReq) (FetchRes, error) {
	return FetchRes{}, nil
}
func (b *blockingConn) Eval(ctx context.Context, expr string, args ...any) (json.RawMessage, error) {
	b.once.Do(func() { close(b.started) })
	<-ctx.Done()
	close(b.cancelled)
	return nil, ctx.Err()
}
func (b *blockingConn) Close() error { return nil }

// A client that disconnects mid-op must have its in-flight browser op cancelled
// (rather than leaving it pinned for the full duration).
func TestDaemonCancelsInflightOnDisconnect(t *testing.T) {
	bc := &blockingConn{started: make(chan struct{}), cancelled: make(chan struct{})}
	st := &daemonState{c: &Client{conn: bc, cfg: Config{}}}

	cli, srv := net.Pipe()
	serverCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { serveConn(serverCtx, srv, st, cancel); close(done) }()

	// Send an eval request that will block inside the fake conn.
	if err := json.NewEncoder(cli).Encode(dReq{Op: "eval", Expr: "x"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	select {
	case <-bc.started:
	case <-time.After(2 * time.Second):
		t.Fatal("eval never started")
	}

	// Client disconnects while the op is in flight.
	cli.Close()

	select {
	case <-bc.cancelled:
	case <-time.After(2 * time.Second):
		t.Fatal("in-flight op was not cancelled on client disconnect")
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("serveConn did not return after disconnect")
	}
}

// A normal request/response round-trip must still work with async dispatch.
func TestDaemonRoundtripStillWorks(t *testing.T) {
	st := &daemonState{c: &Client{conn: &recordConn{}, cfg: Config{}}}
	cli, srv := net.Pipe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go serveConn(ctx, srv, st, cancel)

	enc := json.NewEncoder(cli)
	dec := json.NewDecoder(cli)
	if err := enc.Encode(dReq{Op: "ping"}); err != nil {
		t.Fatalf("send: %v", err)
	}
	var resp dResp
	cli.SetReadDeadline(time.Now().Add(2 * time.Second))
	if err := dec.Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Error != "" {
		t.Fatalf("ping error: %s", resp.Error)
	}
	cli.Close()
}

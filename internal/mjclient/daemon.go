package mjclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ehmo/mj/internal/mjapi"
)

// DaemonSocketPath returns the per-user daemon socket path, creating its dir.
func DaemonSocketPath() string {
	base := os.Getenv("XDG_RUNTIME_DIR")
	if base == "" {
		if c, err := os.UserCacheDir(); err == nil {
			base = c
		} else {
			base = os.TempDir()
		}
	}
	dir := filepath.Join(base, "mj")
	_ = os.MkdirAll(dir, 0o700)
	return filepath.Join(dir, "daemon.sock")
}

// DaemonAvailable reports whether a daemon is accepting connections at socket.
func DaemonAvailable(socket string) bool {
	c, err := net.DialTimeout("unix", socket, 500*time.Millisecond)
	if err != nil {
		return false
	}
	c.Close()
	return true
}

// ---- wire protocol (newline-delimited JSON over a unix socket) ----

type dReq struct {
	Op    string            `json:"op"` // "fetch" | "eval" | "shutdown" | "ping"
	Fetch *FetchReq         `json:"fetch,omitempty"`
	Expr  string            `json:"expr,omitempty"`
	Args  []json.RawMessage `json:"args,omitempty"`
}

type dResp struct {
	Error string          `json:"error,omitempty"`
	Fetch *FetchRes       `json:"fetch,omitempty"`
	Eval  json.RawMessage `json:"eval,omitempty"`
	Acct  json.RawMessage `json:"acct,omitempty"`
}

// ---- remote transport (client side) ----

type remotePageConn struct {
	mu  sync.Mutex
	c   net.Conn
	enc *json.Encoder
	dec *json.Decoder
}

func (r *remotePageConn) roundtrip(req dReq) (dResp, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err := r.enc.Encode(req); err != nil {
		return dResp{}, err
	}
	var resp dResp
	if err := r.dec.Decode(&resp); err != nil {
		return dResp{}, err
	}
	return resp, nil
}

func (r *remotePageConn) Fetch(ctx context.Context, req FetchReq) (FetchRes, error) {
	resp, err := r.roundtrip(dReq{Op: "fetch", Fetch: &req})
	if err != nil {
		return FetchRes{}, err
	}
	var res FetchRes
	if resp.Fetch != nil {
		res = *resp.Fetch
	}
	if resp.Error != "" {
		return res, fmt.Errorf("%s", resp.Error)
	}
	return res, nil
}

func (r *remotePageConn) Eval(ctx context.Context, expr string, args ...any) (json.RawMessage, error) {
	raws := make([]json.RawMessage, 0, len(args))
	for _, a := range args {
		b, err := json.Marshal(a)
		if err != nil {
			return nil, err
		}
		raws = append(raws, b)
	}
	resp, err := r.roundtrip(dReq{Op: "eval", Expr: expr, Args: raws})
	if err != nil {
		return nil, err
	}
	if resp.Error != "" {
		return resp.Eval, fmt.Errorf("%s", resp.Error)
	}
	return resp.Eval, nil
}

func (r *remotePageConn) Close() error { return r.c.Close() }

// Account returns the daemon's cached, authenticated account (so remote clients
// skip the per-command auth handshake).
func (r *remotePageConn) Account(ctx context.Context) (mjapi.Account, error) {
	resp, err := r.roundtrip(dReq{Op: "account"})
	if err != nil {
		return mjapi.Account{}, err
	}
	if resp.Error != "" {
		return mjapi.Account{}, fmt.Errorf("%s", resp.Error)
	}
	var a mjapi.Account
	if err := json.Unmarshal(resp.Acct, &a); err != nil {
		return mjapi.Account{}, err
	}
	return a, nil
}

// Open returns a Client routed through a running daemon when one is available
// (headless commands), otherwise a fresh local browser. Headful configs always
// get a local browser (the interactive login flow). This is the single entry
// point both the CLI and the MCP server use, so they share one warm browser +
// session when a daemon is running.
func Open(ctx context.Context, cfg Config) (*Client, error) {
	if cfg.Headless {
		if sock := DaemonSocketPath(); DaemonAvailable(sock) {
			if c, err := DialDaemon(sock, cfg); err == nil {
				return c, nil
			}
		}
	}
	return New(ctx, cfg)
}

// DialDaemon connects to a running daemon and returns a Client whose browser
// primitives execute on the daemon's warm browser.
func DialDaemon(socket string, cfg Config) (*Client, error) {
	c, err := net.DialTimeout("unix", socket, 2*time.Second)
	if err != nil {
		return nil, err
	}
	rc := &remotePageConn{c: c, enc: json.NewEncoder(c), dec: json.NewDecoder(c)}
	return NewWithConn(rc, cfg), nil
}

// StopDaemon asks a running daemon to shut down.
func StopDaemon(socket string) error {
	c, err := net.DialTimeout("unix", socket, 2*time.Second)
	if err != nil {
		return err
	}
	defer c.Close()
	return json.NewEncoder(c).Encode(dReq{Op: "shutdown"})
}

// ---- daemon server ----

// daemonState is the shared per-daemon runtime: serialized browser access, a
// cross-client submit throttle (ban-safety: one funnel for CLI + MCP), and
// last-activity tracking for idle shutdown.
type daemonState struct {
	c         *Client
	browserMu sync.Mutex // serialize all page ops

	submitMu   sync.Mutex // serialize + throttle submit-jobs across all clients
	lastSubmit time.Time
	submitGap  time.Duration
	penalty    time.Duration // adaptive extra gap after a 429 (decays on success)

	activity atomic.Int64 // unixnano of last request
	inflight atomic.Int64 // requests currently being handled
}

func (s *daemonState) touch() { s.activity.Store(time.Now().UnixNano()) }

// ServeDaemon listens on socket and serves browser primitives from c's warm
// browser. Browser ops are serialized; submit-jobs are throttled across all
// clients by c's MinSubmitGap. If idle > 0, the daemon shuts down after that
// much inactivity. Returns when ctx is cancelled, a shutdown request arrives,
// idle expires, or the listener errors.
func ServeDaemon(ctx context.Context, socket string, c *Client, idle time.Duration) error {
	_ = os.Remove(socket)
	ln, err := net.Listen("unix", socket)
	if err != nil {
		return err
	}
	defer ln.Close()
	defer os.Remove(socket)

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() {
		<-ctx.Done()
		ln.Close()
	}()

	st := &daemonState{c: c, submitGap: c.cfg.MinSubmitGap}
	st.touch()
	if idle > 0 {
		go idleWatchdog(ctx, st, idle, cancel)
	}
	for {
		nc, err := ln.Accept()
		if err != nil {
			select {
			case <-ctx.Done():
				return nil
			default:
				return err
			}
		}
		go serveConn(ctx, nc, st, cancel)
	}
}

func idleWatchdog(ctx context.Context, st *daemonState, idle time.Duration, shutdown context.CancelFunc) {
	t := time.NewTicker(idle / 4)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			// Never shut down while a request is in flight (avoids cancelling a
			// client's fetch mid-call).
			if st.inflight.Load() > 0 {
				continue
			}
			last := time.Unix(0, st.activity.Load())
			if time.Since(last) >= idle {
				shutdown()
				return
			}
		}
	}
}

func serveConn(ctx context.Context, nc net.Conn, st *daemonState, shutdown context.CancelFunc) {
	defer nc.Close()
	// Per-connection context: when this client disconnects (the decoder errors)
	// cancel any of its in-flight browser ops, so a killed client can't pin
	// browserMu for the duration of a generation and block every other client.
	connCtx, connCancel := context.WithCancel(ctx)
	defer connCancel()
	dec := json.NewDecoder(nc)
	enc := json.NewEncoder(nc)
	var wmu sync.Mutex    // serialize response writes
	var wg sync.WaitGroup // in-flight handlers on this connection
	defer wg.Wait()
	for {
		var req dReq
		if err := dec.Decode(&req); err != nil {
			connCancel() // client gone (or a bad frame): abort in-flight ops
			return
		}
		st.touch()
		if req.Op == "shutdown" {
			shutdown()
			return
		}
		// Handle off the read loop so the loop stays responsive to disconnect
		// while a long op runs. Our clients are synchronous (one request in
		// flight), so responses stay ordered; browserMu still serializes work.
		st.inflight.Add(1)
		wg.Add(1)
		go func(req dReq) {
			defer wg.Done()
			defer st.inflight.Add(-1)
			resp := handleReq(connCtx, st, req)
			st.touch()
			wmu.Lock()
			err := enc.Encode(resp)
			wmu.Unlock()
			if err != nil {
				connCancel()
			}
		}(req)
	}
}

// throttleSubmit enforces the cross-client min gap (plus any adaptive penalty)
// between submit-jobs calls.
func (s *daemonState) throttleSubmit(ctx context.Context) error {
	s.submitMu.Lock()
	defer s.submitMu.Unlock()
	gap := s.submitGap + s.penalty
	if gap <= 0 {
		s.lastSubmit = time.Now()
		return nil
	}
	if !s.lastSubmit.IsZero() {
		if wait := gap - time.Since(s.lastSubmit); wait > 0 {
			select {
			case <-time.After(wait):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	s.lastSubmit = time.Now()
	return nil
}

// recordSubmitStatus adapts the cross-client backoff to rate-limit signals from
// the submit-jobs response: a 429 grows the penalty (capped), a success decays it.
func (s *daemonState) recordSubmitStatus(status int) {
	s.submitMu.Lock()
	defer s.submitMu.Unlock()
	switch {
	case status == 429:
		s.penalty += submitPenaltyStep
		if s.penalty > submitPenaltyMax {
			s.penalty = submitPenaltyMax
		}
	case status >= 200 && status < 300:
		s.penalty /= 2
	}
}

func handleReq(ctx context.Context, st *daemonState, req dReq) dResp {
	switch req.Op {
	case "ping":
		return dResp{}
	case "account":
		st.browserMu.Lock()
		defer st.browserMu.Unlock()
		a, err := st.c.Account(ctx)
		if err != nil {
			return dResp{Error: err.Error()}
		}
		b, _ := json.Marshal(a)
		return dResp{Acct: b}
	case "fetch":
		if req.Fetch == nil {
			return dResp{Error: "missing fetch"}
		}
		// Cross-client submit throttle, applied BEFORE taking the browser lock
		// so status polls aren't blocked during the wait.
		isSubmit := strings.HasSuffix(req.Fetch.URL, "/api/submit-jobs")
		if isSubmit {
			if err := st.throttleSubmit(ctx); err != nil {
				return dResp{Error: err.Error()}
			}
		}
		st.browserMu.Lock()
		defer st.browserMu.Unlock()
		fr, err := st.c.conn.Fetch(ctx, *req.Fetch)
		if isSubmit && err == nil {
			st.recordSubmitStatus(fr.StatusCode) // adapt backoff to 429/success
		}
		resp := dResp{Fetch: &fr}
		if err != nil {
			resp.Error = err.Error()
		}
		return resp
	case "eval":
		st.browserMu.Lock()
		defer st.browserMu.Unlock()
		args := make([]any, 0, len(req.Args))
		for _, raw := range req.Args {
			var a any
			_ = json.Unmarshal(raw, &a)
			args = append(args, a)
		}
		ev, err := st.c.conn.Eval(ctx, req.Expr, args...)
		resp := dResp{Eval: ev}
		if err != nil {
			resp.Error = err.Error()
		}
		return resp
	default:
		return dResp{Error: "unknown op: " + req.Op}
	}
}

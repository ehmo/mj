// Package mjclient is the Midjourney web client: it drives a Camoufox browser
// (via github.com/ehmo/gomoufox) to make authenticated, Cloudflare-passing
// calls to www.midjourney.com/api/*, and uses plain Go HTTP for the
// non-gated securetoken exchange and CDN downloads.
package mjclient

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	gomoufox "github.com/ehmo/gomoufox"
	"github.com/ehmo/gomoufox/camoufoxcfg"
	"github.com/ehmo/mj/internal/mjapi"
)

const apiBase = "https://www.midjourney.com"

// apiDebug, when MJ_DEBUG=1, logs each API request (method, path, status,
// duration) to stderr with token redaction — a diagnostic aid for throttle/ban
// investigation.
var apiDebug = os.Getenv("MJ_DEBUG") == "1"

// Adaptive rate-limit backoff: after an HTTP 429 on submit-jobs the effective
// submit gap grows by submitPenaltyStep (capped at submitPenaltyMax) and decays
// on each subsequent success, so the tool eases off when the account is being
// rate-limited rather than hammering toward a ban.
const (
	submitPenaltyStep = 30 * time.Second
	submitPenaltyMax  = 5 * time.Minute
)

// CredStore persists the durable Firebase refresh token (e.g. hasp-backed).
type CredStore interface {
	Get(ctx context.Context) (string, error)
	Set(ctx context.Context, refreshToken string) error
}

// Config configures a Client.
type Config struct {
	ProfileDir     string        // persistent Camoufox profile (session reuse); required for login persistence
	Headless       bool          // false = headful (login flow)
	RefreshToken   string        // optional explicit refresh token (overrides CredStore/IndexedDB)
	Proxy          string        // optional "http://host:port"
	ConnectTimeout time.Duration // browser connect timeout (default 60s)
	MinSubmitGap   time.Duration // throttle between submit-jobs (default 12s)
	Creds          CredStore     // optional credential store (hasp)
}

// Client is a connected Midjourney web client. Not safe for concurrent submits
// (callers serialize); read methods may interleave.
type Client struct {
	conn pageConn
	cfg  Config

	mu            sync.Mutex
	acct          *mjapi.Account
	authedAt      time.Time
	curRefresh    string // the refresh token last validated by EnsureSession
	lastSubmit    time.Time
	submitPenalty time.Duration // adaptive extra gap after a 429 (decays on success)

	authMu   sync.Mutex // serializes EnsureSession (avoids concurrent token exchange)
	submitMu sync.Mutex // serializes the submit throttle wait
}

// NewWithConn builds a Client over an existing transport (used by the daemon
// client to route browser primitives to a warm remote browser).
func NewWithConn(conn pageConn, cfg Config) *Client {
	if cfg.MinSubmitGap == 0 {
		cfg.MinSubmitGap = 12 * time.Second
	}
	return &Client{conn: conn, cfg: cfg}
}

// New launches Camoufox, opens a page, and parks it on the midjourney.com
// origin so in-browser fetches send same-origin session cookies. It does not
// authenticate; call EnsureSession (or any API method, which auto-auths on 401).
func New(ctx context.Context, cfg Config) (*Client, error) {
	if cfg.ConnectTimeout == 0 {
		cfg.ConnectTimeout = 60 * time.Second
	}
	if cfg.MinSubmitGap == 0 {
		cfg.MinSubmitGap = 12 * time.Second
	}
	headless := camoufoxcfg.HeadlessTrue
	if !cfg.Headless {
		headless = camoufoxcfg.HeadlessFalse
	}
	opts := []gomoufox.Option{
		gomoufox.WithHeadless(headless),
		gomoufox.WithConnectTimeout(cfg.ConnectTimeout),
		// Bypass gomoufox's local filtering proxy: the browser dials MJ/Google/CDN
		// directly (the validated path). The proxy otherwise stalls navigation in
		// some environments, and we only ever talk to the user's own MJ account.
		gomoufox.WithUnsafeDirectNetwork(true),
	}
	if cfg.ProfileDir != "" {
		opts = append(opts, gomoufox.WithPersistentContext(cfg.ProfileDir))
	}
	if cfg.Proxy != "" {
		opts = append(opts, gomoufox.WithProxy(camoufoxcfg.ProxyConfig{Server: cfg.Proxy}))
	}
	br, err := gomoufox.New(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("launch camoufox: %w", err)
	}
	page, err := br.NewPage(ctx)
	if err != nil {
		br.Close()
		return nil, fmt.Errorf("open page: %w", err)
	}
	// Park on the origin so in-browser fetch() is same-origin (sends session
	// cookies) and Firebase IndexedDB is reachable. Use /imagine (validated)
	// and wait only for the DOM; fall back to "commit" (origin established)
	// since midjourney.com holds long-lived connections that stall "load".
	parkURL := apiBase + "/imagine"
	if _, err := page.Goto(ctx, parkURL,
		gomoufox.WaitUntil("domcontentloaded"),
		gomoufox.WithTimeout(60*time.Second)); err != nil {
		if _, err2 := page.Goto(ctx, parkURL,
			gomoufox.WaitUntil("commit"),
			gomoufox.WithTimeout(30*time.Second)); err2 != nil {
			br.Close()
			return nil, fmt.Errorf("park page on origin: %w", err)
		}
	}
	return &Client{conn: &localPageConn{br: br, page: page}, cfg: cfg}, nil
}

// Close shuts down the browser and sidecar.
func (c *Client) Close() error {
	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}

// api performs a single in-browser fetch to a midjourney.com/api path. It
// returns the HTTP status and body without treating non-2xx as a Go error
// (so callers can react to 401). The response cap is disabled — our API
// responses are server-bounded.
func (c *Client) api(ctx context.Context, method, path string, body []byte) (int, []byte, error) {
	headers := map[string]string{"x-csrf-protection": "1"}
	if body != nil {
		headers["content-type"] = "application/json"
	}
	start := time.Now()
	res, err := c.conn.Fetch(ctx, FetchReq{
		URL: apiBase + path, Method: method, Headers: headers, Body: body, MaxBytes: -1})
	if apiDebug {
		if err != nil {
			fmt.Fprintln(os.Stderr, Redact(fmt.Sprintf("[mj] %s %s -> error: %v (%s)", method, path, err, time.Since(start).Round(time.Millisecond))))
		} else {
			fmt.Fprintln(os.Stderr, Redact(fmt.Sprintf("[mj] %s %s -> %d (%s)", method, path, res.StatusCode, time.Since(start).Round(time.Millisecond))))
		}
	}
	return res.StatusCode, res.Body, err
}

// call wraps api with one automatic re-auth on 401 and turns non-2xx into an
// *APIError. Submit-jobs is throttled HERE (the single choke point) so the
// ban-safety gap applies uniformly to typed submits AND the raw api/PostJSON
// escape hatches — not just submitOne.
func (c *Client) call(ctx context.Context, method, path string, body []byte) ([]byte, error) {
	if path == "/api/submit-jobs" {
		if err := c.throttleSubmit(ctx); err != nil {
			return nil, err
		}
	}
	status, b, err := c.api(ctx, method, path, body)
	if err != nil {
		return nil, err
	}
	if status == 401 {
		if err := c.EnsureSession(ctx); err != nil {
			c.invalidateSession()
			return nil, fmt.Errorf("re-auth after 401: %w", err)
		}
		if status, b, err = c.api(ctx, method, path, body); err != nil {
			return nil, err
		}
		if status == 401 {
			// re-auth succeeded but the request is still unauthorized — drop the
			// cached session so the next call starts a clean handshake.
			c.invalidateSession()
		}
	}
	if path == "/api/submit-jobs" {
		c.recordSubmitStatus(status)
	}
	if status < 200 || status >= 300 {
		return nil, &APIError{Status: status, Path: path, Body: preview(b)}
	}
	return b, nil
}

// recordSubmitStatus adapts the submit backoff to rate-limit signals: a 429
// grows the penalty (capped), a success decays it. Other statuses leave it.
func (c *Client) recordSubmitStatus(status int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	switch {
	case status == 429:
		c.submitPenalty += submitPenaltyStep
		if c.submitPenalty > submitPenaltyMax {
			c.submitPenalty = submitPenaltyMax
		}
	case status >= 200 && status < 300:
		c.submitPenalty /= 2
	}
}

// Get performs an authenticated GET on a midjourney.com/api path (e.g.
// "/api/folders") and returns the raw body. Exposes the in-browser transport
// for read endpoints beyond the typed methods.
func (c *Client) Get(ctx context.Context, path string) ([]byte, error) {
	if err := c.ensureAuthed(ctx); err != nil {
		return nil, err
	}
	return c.call(ctx, "GET", path, nil)
}

// PostJSON performs an authenticated POST with a JSON body on a midjourney.com
// /api path and returns the raw body.
func (c *Client) PostJSON(ctx context.Context, path string, body []byte) ([]byte, error) {
	if err := c.ensureAuthed(ctx); err != nil {
		return nil, err
	}
	return c.call(ctx, "POST", path, body)
}

// accountConn is implemented by transports that can serve a warm, authenticated
// account (the daemon), letting clients skip the auth handshake.
type accountConn interface {
	Account(ctx context.Context) (mjapi.Account, error)
}

// accountTTL bounds how long a locally-authed account is served before a
// proactive session refresh — comfortably under the ~1h Firebase id-token expiry
// so a long-lived client (the daemon) never hits the expiry cliff mid-request.
const accountTTL = 30 * time.Minute

// Account returns the cached authUser, authenticating first if needed. When the
// transport is a daemon, it serves the daemon's warm account (no handshake).
// A locally-authed account older than accountTTL is refreshed proactively; if
// that refresh fails, the still-cached account is served (the next API call's
// 401 retry is the backstop).
func (c *Client) Account(ctx context.Context) (mjapi.Account, error) {
	c.mu.Lock()
	a, at := c.acct, c.authedAt
	c.mu.Unlock()
	if a != nil {
		// at.IsZero() ⇒ served by the daemon (it manages its own freshness).
		if at.IsZero() || time.Since(at) < accountTTL {
			return *a, nil
		}
		if err := c.EnsureSession(ctx); err != nil {
			return *a, nil // serve stale rather than fail; 401 retry will recover
		}
		c.mu.Lock()
		defer c.mu.Unlock()
		return *c.acct, nil
	}
	if ac, ok := c.conn.(accountConn); ok {
		if acct, err := ac.Account(ctx); err == nil {
			c.mu.Lock()
			c.acct = &acct
			c.mu.Unlock()
			return acct, nil
		}
	}
	if err := c.EnsureSession(ctx); err != nil {
		return mjapi.Account{}, err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return *c.acct, nil
}

// ensureAuthed makes sure a session exists before a read call (the 401 retry in
// call() also covers expiry mid-session).
func (c *Client) ensureAuthed(ctx context.Context) error {
	c.mu.Lock()
	a := c.acct
	c.mu.Unlock()
	if a != nil {
		return nil
	}
	// Account() uses the daemon's warm session for remote clients, or runs the
	// full handshake for a local client.
	_, err := c.Account(ctx)
	return err
}

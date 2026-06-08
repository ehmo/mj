package mjclient

import (
	"context"
	"encoding/json"

	gomoufox "github.com/ehmo/gomoufox"
)

// FetchReq is an in-browser fetch request (to www.midjourney.com/api/* or CDN).
type FetchReq struct {
	URL      string            `json:"url"`
	Method   string            `json:"method"`
	Headers  map[string]string `json:"headers"`
	Body     []byte            `json:"body"`
	MaxBytes int               `json:"max_bytes"`
}

// FetchRes is the result of an in-browser fetch.
type FetchRes struct {
	StatusCode int    `json:"status_code"`
	Body       []byte `json:"body"`
	Truncated  bool   `json:"truncated"`
}

// pageConn is the minimal browser transport the Client needs. It is satisfied by
// a local gomoufox page (own-browser) or a remote daemon page (warm shared
// browser over a socket). All Cloudflare-gated traffic flows through it.
type pageConn interface {
	// Fetch runs fetch() inside the browser page context.
	Fetch(ctx context.Context, req FetchReq) (FetchRes, error)
	// Eval runs a JS function inside the page and returns its JSON result.
	Eval(ctx context.Context, expr string, args ...any) (json.RawMessage, error)
	// Close releases the transport (and, for local, the browser).
	Close() error
}

// localPageConn drives a gomoufox page in this process.
type localPageConn struct {
	br   *gomoufox.Browser
	page *gomoufox.Page
}

func (l *localPageConn) Fetch(ctx context.Context, req FetchReq) (FetchRes, error) {
	r, err := l.page.FetchBytesWithOptions(ctx, req.URL, req.Method, req.Headers, req.Body,
		gomoufox.FetchBytesOptions{MaxBytes: req.MaxBytes})
	return FetchRes{StatusCode: r.StatusCode, Body: r.Body, Truncated: r.Truncated}, err
}

func (l *localPageConn) Eval(ctx context.Context, expr string, args ...any) (json.RawMessage, error) {
	var raw json.RawMessage
	if err := l.page.EvaluateIntoJSON(ctx, expr, &raw, args...); err != nil {
		return nil, err
	}
	return raw, nil
}

func (l *localPageConn) Close() error {
	if l.br != nil {
		l.br.Close()
	}
	return nil
}

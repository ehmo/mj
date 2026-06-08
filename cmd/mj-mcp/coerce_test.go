package main

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestGetInt64(t *testing.T) {
	cases := []struct {
		v    any
		want int64
	}{
		{float64(250), 250},
		{"250", 250},
		{" 17 ", 17},
		{"nope", 0},
		{nil, 0},
		{true, 0},
	}
	for _, tc := range cases {
		m := map[string]any{"k": tc.v}
		if got := getInt64(m, "k"); got != tc.want {
			t.Errorf("getInt64(%v)=%d want %d", tc.v, got, tc.want)
		}
	}
	if got := getInt64(map[string]any{}, "missing"); got != 0 {
		t.Errorf("missing key: got %d", got)
	}
}

func TestGetBool(t *testing.T) {
	cases := []struct {
		v    any
		want bool
	}{
		{true, true}, {false, false}, {"true", true}, {"1", true},
		{"false", false}, {"0", false}, {"", false}, {nil, false},
	}
	for _, tc := range cases {
		if got := getBool(map[string]any{"k": tc.v}, "k"); got != tc.want {
			t.Errorf("getBool(%v)=%v want %v", tc.v, got, tc.want)
		}
	}
}

func TestGetFloatDef(t *testing.T) {
	if got := getFloatDef(map[string]any{"k": "1.5"}, "k", 2); got != 1.5 {
		t.Errorf("string float: %v", got)
	}
	if got := getFloatDef(map[string]any{}, "k", 2); got != 2 {
		t.Errorf("default: %v", got)
	}
	if got := getFloatDef(map[string]any{"k": "x"}, "k", 2); got != 2 {
		t.Errorf("bad string falls back: %v", got)
	}
}

func TestArgsToParams(t *testing.T) {
	m := map[string]any{"ar": "3:2", "version": "7", "stylize": "250", "chaos": float64(10), "no": "a,b", "seed": "42"}
	p := argsToParams(m)
	if p.AR != "3:2" || p.Version != "7" {
		t.Fatalf("ar/version: %+v", p)
	}
	if p.Stylize == nil || *p.Stylize != 250 {
		t.Errorf("stylize coercion failed: %v", p.Stylize)
	}
	if p.Chaos == nil || *p.Chaos != 10 {
		t.Errorf("chaos: %v", p.Chaos)
	}
	if p.Seed == nil || *p.Seed != 42 {
		t.Errorf("seed: %v", p.Seed)
	}
	if len(p.No) != 2 || p.No[0] != "a" || p.No[1] != "b" {
		t.Errorf("no: %v", p.No)
	}
	// Unset numeric must stay nil (not a zero pointer).
	if argsToParams(map[string]any{}).Stylize != nil {
		t.Errorf("unset stylize should be nil")
	}
}

func TestDispatchNotificationHandling(t *testing.T) {
	s := &server{}
	// id present -> response with matching id, not a notification.
	resp, isNotif := s.dispatch(&rpcReq{JSONRPC: "2.0", ID: json.RawMessage("5"), Method: "ping"})
	if isNotif || resp == nil || string(resp.ID) != "5" {
		t.Errorf("ping with id: notif=%v resp=%+v", isNotif, resp)
	}
	// explicit null id -> notification.
	if _, isNotif := s.dispatch(&rpcReq{JSONRPC: "2.0", ID: json.RawMessage("null"), Method: "ping"}); !isNotif {
		t.Errorf("null id should be a notification")
	}
	// no id -> notification.
	if _, isNotif := s.dispatch(&rpcReq{JSONRPC: "2.0", Method: "ping"}); !isNotif {
		t.Errorf("missing id should be a notification")
	}
	// unknown method with id -> method-not-found error.
	resp, _ = s.dispatch(&rpcReq{JSONRPC: "2.0", ID: json.RawMessage("9"), Method: "bogus"})
	if resp.Error == nil || resp.Error.Code != -32601 {
		t.Errorf("unknown method: %+v", resp.Error)
	}
}

func TestCancelRequest(t *testing.T) {
	s := &server{cancels: map[string]context.CancelFunc{}}
	called := false
	s.registerCancel("7", func() { called = true })
	s.cancelRequest(json.RawMessage(`{"requestId":7}`))
	if !called {
		t.Fatalf("matching requestId did not cancel")
	}
	// Unknown id is a no-op (must not panic).
	s.cancelRequest(json.RawMessage(`{"requestId":999}`))
	s.unregisterCancel("7")
	if _, ok := s.cancels["7"]; ok {
		t.Errorf("unregister left the entry")
	}

	// String ids must match too (request id "abc" -> raw `"abc"`).
	strCalled := false
	s.registerCancel(`"abc"`, func() { strCalled = true })
	s.cancelRequest(json.RawMessage(`{"requestId":"abc"}`))
	if !strCalled {
		t.Fatalf("string requestId did not cancel")
	}

	// Malformed params must not panic.
	s.cancelRequest(json.RawMessage(`not-json`))
	s.cancelRequest(nil)
}

func TestValidatePublicURL(t *testing.T) {
	bad := []string{
		"http://127.0.0.1/x", "https://10.0.0.5/x", "http://169.254.169.254/latest",
		"https://localhost/x", "file:///etc/passwd", "ftp://h/x", "https://[::1]/x",
		"http://192.168.1.1/x", "https://metadata.google.internal/x",
	}
	for _, u := range bad {
		if err := validatePublicURL(u); err == nil {
			t.Errorf("expected %q to be rejected", u)
		}
	}
	// A normal public hostname must pass (offline: DNS lookup fails -> not blocked).
	if err := validatePublicURL("https://cdn.midjourney.com/abc/0_0.png"); err != nil {
		t.Errorf("public url rejected: %v", err)
	}
}

func TestValidateLocalPath(t *testing.T) {
	os.Unsetenv("MJ_MCP_FILE_ROOT")
	if _, err := validateLocalPath("/etc/hosts"); err == nil {
		t.Fatalf("local paths must be disabled without MJ_MCP_FILE_ROOT")
	}
	root := t.TempDir()
	inside := filepath.Join(root, "img.png")
	if err := os.WriteFile(inside, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("MJ_MCP_FILE_ROOT", root)
	if got, err := validateLocalPath(inside); err != nil || got == "" {
		t.Errorf("in-root path rejected: %v", err)
	}
	if _, err := validateLocalPath(filepath.Join(root, "..", "escape.png")); err == nil {
		t.Errorf("escape path must be rejected")
	}
}

func TestResolveImageRequiresValue(t *testing.T) {
	s := &server{}
	if _, err := s.resolveImage(map[string]any{}, "image"); err == nil {
		t.Errorf("empty image should error")
	}
}

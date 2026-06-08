package mjclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/ehmo/mj/internal/mjapi"
)

// firebaseAPIKey is Midjourney's public Firebase web API key (captured live).
const firebaseAPIKey = "AIzaSyAjizp68NsH3JGUS0EyLXsChW4fN0A92tM"

const securetokenJS = `async ({apiKey, refresh}) => {
  try {
    const r = await fetch('https://securetoken.googleapis.com/v1/token?key=' + apiKey, {
      method: 'POST',
      headers: {'Content-Type': 'application/x-www-form-urlencoded'},
      body: 'grant_type=refresh_token&refresh_token=' + encodeURIComponent(refresh)
    });
    let j = {};
    try { j = await r.json(); } catch (e) {}
    return {status: r.status, id_token: j.id_token || '', refresh_token: j.refresh_token || '',
            error: (j.error && j.error.message) || ''};
  } catch (e) {
    return {status: 0, id_token: '', refresh_token: '', error: String(e)};
  }
}`

// securetoken exchanges a Firebase refresh token for a fresh id token. It runs
// inside the browser (default credentials, so Google's wildcard CORS applies)
// rather than via Go HTTP — this keeps all egress on the browser's working
// direct-network path and avoids the credentials/CORS conflict in FetchBytes.
func (c *Client) securetoken(ctx context.Context, refresh string) (idToken, newRefresh string, err error) {
	var out struct {
		Status       int    `json:"status"`
		IDToken      string `json:"id_token"`
		RefreshToken string `json:"refresh_token"`
		Error        string `json:"error"`
	}
	raw, err := c.conn.Eval(ctx, securetokenJS, map[string]any{"apiKey": firebaseAPIKey, "refresh": refresh})
	if err != nil {
		return "", "", fmt.Errorf("securetoken eval: %w", err)
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return "", "", fmt.Errorf("securetoken decode: %w", err)
	}
	if out.IDToken == "" {
		reason := out.Error
		if reason == "" {
			reason = fmt.Sprintf("status %d", out.Status)
		}
		return "", "", &AuthError{Reason: "securetoken refused refresh token: " + reason}
	}
	if out.RefreshToken == "" {
		out.RefreshToken = refresh
	}
	return out.IDToken, out.RefreshToken, nil
}

type fbCookie struct {
	Type         string `json:"type"`
	IDToken      string `json:"idToken"`
	RefreshToken string `json:"refreshToken"`
}

type fbLoginBody struct {
	Type   string   `json:"type"`
	Cookie fbCookie `json:"cookie"`
}

// firebaseLogin POSTs /api/firebase-login inside the browser (establishing the
// MJ session cookie in the context) and returns the parsed authUser. Uses api
// directly to avoid the call() 401 re-auth recursion.
func (c *Client) firebaseLogin(ctx context.Context, idToken, refresh string) (mjapi.Account, error) {
	body, _ := json.Marshal(fbLoginBody{
		Type:   "firebase",
		Cookie: fbCookie{Type: "firebase", IDToken: idToken, RefreshToken: refresh},
	})
	status, b, err := c.api(ctx, http.MethodPost, "/api/firebase-login", body)
	if err != nil {
		return mjapi.Account{}, fmt.Errorf("firebase-login: %w", err)
	}
	if status != http.StatusOK {
		return mjapi.Account{}, &APIError{Status: status, Path: "/api/firebase-login", Body: preview(b)}
	}
	return mjapi.ParseAuthUser(b)
}

// EnsureSession (re)establishes the MJ session: fetch refresh token → securetoken
// → firebase-login → cache authUser. Persists a rotated refresh token to the
// CredStore when one is configured.
//
// It is serialized by authMu so concurrent 401 retries don't run the token
// exchange twice (which can invalidate a rotated refresh token mid-flight). A
// caller that arrives just after another goroutine refreshed returns immediately
// rather than re-exchanging.
func (c *Client) EnsureSession(ctx context.Context) error {
	c.authMu.Lock()
	defer c.authMu.Unlock()
	c.mu.Lock()
	fresh := c.acct != nil && time.Since(c.authedAt) < 10*time.Second
	c.mu.Unlock()
	if fresh {
		return nil
	}

	refresh, err := c.refreshToken(ctx)
	if err != nil {
		return err
	}
	idToken, newRefresh, err := c.securetoken(ctx, refresh)
	if err != nil {
		return err
	}
	acct, err := c.firebaseLogin(ctx, idToken, newRefresh)
	if err != nil {
		return err
	}
	effective := newRefresh
	if effective == "" {
		effective = refresh
	}
	c.mu.Lock()
	c.acct = &acct
	c.authedAt = time.Now()
	c.curRefresh = effective
	c.mu.Unlock()
	if newRefresh != "" && newRefresh != refresh && c.cfg.Creds != nil {
		_ = c.cfg.Creds.Set(ctx, newRefresh) // best-effort rotation persistence
	}
	return nil
}

// CurrentRefreshToken returns the refresh token most recently validated by
// EnsureSession (post-rotation). Empty until a session is established.
func (c *Client) CurrentRefreshToken() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.curRefresh
}

// invalidateSession clears the cached account so the next call re-authenticates.
// Called when an auth-dependent request fails after a re-auth attempt.
func (c *Client) invalidateSession() {
	c.mu.Lock()
	c.acct = nil
	c.authedAt = time.Time{}
	c.mu.Unlock()
}

// refreshToken resolves the Firebase refresh token: explicit config →
// CredStore → the persistent profile's Firebase IndexedDB.
func (c *Client) refreshToken(ctx context.Context) (string, error) {
	if c.cfg.RefreshToken != "" {
		return c.cfg.RefreshToken, nil
	}
	if c.cfg.Creds != nil {
		if tok, err := c.cfg.Creds.Get(ctx); err == nil && tok != "" {
			return tok, nil
		}
	}
	tok, err := c.refreshFromIndexedDB(ctx)
	if err != nil {
		return "", err
	}
	if tok == "" {
		return "", &AuthError{Reason: "no refresh token (run `mj login` first)"}
	}
	return tok, nil
}

const idbRefreshJS = `async () => {
  const open = () => new Promise((res, rej) => {
    const r = indexedDB.open('firebaseLocalStorageDb');
    r.onsuccess = () => res(r.result); r.onerror = () => rej(r.error);
  });
  let db;
  try { db = await open(); } catch (e) { return ""; }
  const rows = await new Promise((res, rej) => {
    const ga = db.transaction('firebaseLocalStorage', 'readonly')
      .objectStore('firebaseLocalStorage').getAll();
    ga.onsuccess = () => res(ga.result); ga.onerror = () => rej(ga.error);
  });
  for (const row of rows) {
    const v = (row && row.value) ? row.value : row;
    const u = (v && v.value) ? v.value : v;
    if (u && u.stsTokenManager && u.stsTokenManager.refreshToken) return u.stsTokenManager.refreshToken;
  }
  return "";
}`

// ReadRefreshToken returns the Firebase refresh token from the parked page's
// IndexedDB (populated after an interactive login). Empty string if not logged in.
func (c *Client) ReadRefreshToken(ctx context.Context) (string, error) {
	return c.refreshFromIndexedDB(ctx)
}

// refreshFromIndexedDB reads the Firebase refresh token from the parked page's
// IndexedDB (origin midjourney.com). Populated after an interactive login.
func (c *Client) refreshFromIndexedDB(ctx context.Context) (string, error) {
	raw, err := c.conn.Eval(ctx, idbRefreshJS)
	if err != nil {
		return "", fmt.Errorf("read firebase IndexedDB: %w", err)
	}
	var tok string
	_ = json.Unmarshal(raw, &tok)
	return tok, nil
}

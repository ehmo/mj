package mjclient

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ehmo/mj/internal/mjapi"
)

var wsDebug = os.Getenv("MJ_WS_DEBUG") == "1"

const wsSetupJS = `async ({token}) => {
  return await new Promise((resolve) => {
    if (window.__mjws) { try { window.__mjws.close(); } catch(e){} }
    window.__mjframes = [];
    const ws = new WebSocket('wss://ws.midjourney.com/ws?token=' + token);
    window.__mjws = ws;
    ws.binaryType = 'blob';
    ws.onopen = () => { try { ws.send(JSON.stringify({type:'subscribe_to_user'})); } catch(e){}; resolve({open:true}); };
    ws.onmessage = async (e) => {
      try {
        if (typeof e.data === 'string') return;
        const buf = await e.data.arrayBuffer();
        const dataUrl = await new Promise(r => { const fr = new FileReader(); fr.onloadend = () => r(fr.result); fr.readAsDataURL(new Blob([buf])); });
        window.__mjframes.push(dataUrl.split(',')[1]);
      } catch(err) {}
    };
    ws.onerror = () => resolve({open:false});
    setTimeout(() => resolve({open:false}), 8000);
  });
}`

const wsDrainJS = `() => { const f = window.__mjframes || []; window.__mjframes = []; return f; }`

// WatchConnect opens the realtime WebSocket and subscribes to the user BEFORE a
// job is submitted (the server only streams a job's frames to clients that were
// subscribed when it started). Returns true if the socket is open; false means
// callers should fall back to status polling. Requires a session-local
// websocketToken (local client only).
func (c *Client) WatchConnect(ctx context.Context) bool {
	if err := c.EnsureSession(ctx); err != nil {
		return false
	}
	acct, _ := c.Account(ctx)
	if wsDebug {
		fmt.Fprintf(os.Stderr, "[ws] connect: token_present=%v\n", acct.WSToken != "")
	}
	if acct.WSToken == "" {
		return false
	}
	var open struct {
		Open bool `json:"open"`
	}
	raw, err := c.conn.Eval(ctx, wsSetupJS, map[string]any{"token": acct.WSToken})
	if err != nil {
		if wsDebug {
			fmt.Fprintf(os.Stderr, "[ws] connect eval err: %v\n", err)
		}
		return false
	}
	_ = json.Unmarshal(raw, &open)
	if wsDebug {
		fmt.Fprintf(os.Stderr, "[ws] connect: open=%v raw=%s\n", open.Open, string(raw))
	}
	return open.Open
}

// WatchJob streams progress for a job, calling cb on each percent/status change,
// until terminal or timeout. It drains WS frames for live percent and polls
// job-status as a backstop for reliable terminal detection. Returns the final
// job. If the WS isn't open (wsOpen=false) it polls only.
func (c *Client) WatchJob(ctx context.Context, id mjapi.JobID, wsOpen bool, timeout time.Duration, cb func(mjapi.Progress)) (mjapi.Job, error) {
	if timeout == 0 {
		timeout = 6 * time.Minute
	}
	if wsDebug {
		fmt.Fprintf(os.Stderr, "[ws] watch job=%s wsOpen=%v\n", id, wsOpen)
	}
	if wsOpen {
		_, _ = c.conn.Eval(ctx, `({jid}) => { try { window.__mjws.send(JSON.stringify({type:"subscribe_to_job", job_id: jid})); } catch(e){} return true; }`,
			map[string]any{"jid": string(id)})
	}
	deadline := time.Now().Add(timeout)
	lastPct, lastStatus := -1, mjapi.Status("")
	nextPoll := time.Now()
	emit := func(p mjapi.Progress) {
		if p.Percent != lastPct || p.Status != lastStatus {
			lastPct, lastStatus = p.Percent, p.Status
			cb(p)
		}
	}
	for time.Now().Before(deadline) {
		if wsOpen {
			if raw, err := c.conn.Eval(ctx, wsDrainJS); err == nil {
				var frames []string
				_ = json.Unmarshal(raw, &frames)
				for _, f := range frames {
					data, derr := base64.StdEncoding.DecodeString(strings.TrimSpace(f))
					if derr != nil {
						continue
					}
					if pr, ok := mjapi.ParseProgress(data); ok && (pr.JobID == "" || pr.JobID == string(id)) {
						emit(pr)
						if pr.Status.Terminal() {
							goto done
						}
					}
				}
			}
		}
		// job-status backstop (terminal detection + percent fallback)
		if time.Now().After(nextPoll) {
			nextPoll = time.Now().Add(4 * time.Second)
			if jobs, err := c.Status(ctx, id); err == nil && len(jobs) > 0 {
				if !wsOpen {
					emit(mjapi.Progress{JobID: string(id), Status: jobs[0].Status})
				}
				if jobs[0].Status.Terminal() {
					if jobs[0].Status.OK() {
						emit(mjapi.Progress{JobID: string(id), Status: jobs[0].Status, Percent: 100})
					}
					return jobs[0], terminalErr(jobs[0].Status)
				}
			}
		}
		select {
		case <-time.After(900 * time.Millisecond):
		case <-ctx.Done():
			return mjapi.Job{}, ctx.Err()
		}
	}
done:
	if wsOpen {
		_, _ = c.conn.Eval(ctx, `() => { try { window.__mjws.close(); } catch(e){} return true; }`)
	}
	jobs, err := c.Status(ctx, id)
	if err != nil || len(jobs) == 0 {
		return mjapi.Job{ID: id, Status: lastStatus}, err
	}
	return jobs[0], terminalErr(jobs[0].Status)
}

func terminalErr(s mjapi.Status) error {
	if s.Terminal() && !s.OK() {
		return fmt.Errorf("%w: %s", ErrJobFailed, s)
	}
	return nil
}

// WatchProgress is a convenience wrapper: connect + watch a single job.
func (c *Client) WatchProgress(ctx context.Context, id mjapi.JobID, timeout time.Duration, cb func(mjapi.Progress)) (mjapi.Job, error) {
	ws := c.WatchConnect(ctx)
	return c.WatchJob(ctx, id, ws, timeout, cb)
}

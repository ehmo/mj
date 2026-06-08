package mjclient

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ehmo/mj/internal/mjapi"
)

// TestLiveWSCapture opens the realtime WebSocket in-page, submits a draft job,
// and dumps the server frames (base64) to capture/ws-frames.txt for decoding.
// MJ_LIVE=1 MJ_WS=1 (submits one generation).
func TestLiveWSCapture(t *testing.T) {
	if os.Getenv("MJ_LIVE") != "1" || os.Getenv("MJ_WS") != "1" {
		t.Skip("set MJ_LIVE=1 MJ_WS=1")
	}
	profile := os.Getenv("MJ_PROFILE")
	if profile == "" {
		profile = "/Users/nan/Work/ai/midjourney/capture/profile"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
	defer cancel()
	c, err := New(ctx, Config{ProfileDir: profile, Headless: true})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer c.Close()
	acct, err := c.Account(ctx)
	if err != nil {
		t.Fatalf("auth: %v", err)
	}
	if acct.WSToken == "" {
		t.Fatal("no websocketToken")
	}

	const setupJS = `async ({token}) => {
	  return await new Promise((resolve) => {
	    window.__mjframes = [];
	    const ws = new WebSocket('wss://ws.midjourney.com/ws?token=' + token);
	    window.__mjws = ws;
	    ws.binaryType = 'blob';
	    ws.onopen = () => { try { ws.send(JSON.stringify({type:'subscribe_to_user'})); } catch(e){}; resolve({open:true}); };
	    ws.onmessage = async (e) => {
	      try {
	        if (typeof e.data === 'string') { window.__mjframes.push('STR:' + e.data); return; }
	        const buf = await e.data.arrayBuffer();
	        const dataUrl = await new Promise(r => { const fr = new FileReader(); fr.onloadend = () => r(fr.result); fr.readAsDataURL(new Blob([buf])); });
	        window.__mjframes.push(dataUrl.split(',')[1]);
	      } catch(err) {}
	    };
	    ws.onerror = () => resolve({open:false, error:'ws error'});
	    setTimeout(() => resolve({open:false, error:'timeout'}), 8000);
	  });
	}`
	var openRes struct {
		Open  bool   `json:"open"`
		Error string `json:"error"`
	}
	raw, err := c.conn.Eval(ctx, setupJS, map[string]any{"token": acct.WSToken})
	if err != nil {
		t.Fatalf("ws setup eval: %v", err)
	}
	_ = json.Unmarshal(raw, &openRes)
	t.Logf("ws open=%v err=%q", openRes.Open, openRes.Error)
	if !openRes.Open {
		t.Fatalf("ws did not open")
	}

	job, err := c.Imagine(ctx, ImagineReq{Prompt: "a simple blue circle on white", Mode: mjapi.ModeFast})
	if err != nil {
		t.Fatalf("imagine: %v", err)
	}
	t.Logf("submitted %s", job.ID)
	_, _ = c.conn.Eval(ctx, `({jid}) => { try { window.__mjws.send(JSON.stringify({type:'subscribe_to_job', job_id: jid})); } catch(e){} return true; }`, map[string]any{"jid": string(job.ID)})

	// collect frames for ~40s or until completion
	var frames []string
	deadline := time.Now().Add(45 * time.Second)
	for time.Now().Before(deadline) {
		r, _ := c.conn.Eval(ctx, `() => window.__mjframes`)
		var fr []string
		_ = json.Unmarshal(r, &fr)
		frames = fr
		if len(frames) > 40 {
			break
		}
		time.Sleep(2 * time.Second)
	}
	t.Logf("collected %d frames", len(frames))

	// dump: text frames inline; binary frames summarized + first bytes hex
	var b strings.Builder
	for i, f := range frames {
		if strings.HasPrefix(f, "STR:") {
			s := f[4:]
			if len(s) > 400 {
				s = s[:400]
			}
			b.WriteString("TEXT " + s + "\n")
			continue
		}
		data, _ := base64.StdEncoding.DecodeString(f)
		head := data
		if len(head) > 32 {
			head = head[:32]
		}
		b.WriteString("BIN  len=" + strconv.Itoa(len(data)) + " head=" + hexBytes(head) + " ascii=" + asciiOf(head) + "\n")
		_ = i
	}
	out := "/Users/nan/Work/ai/midjourney/capture/ws-frames.txt"
	_ = os.WriteFile(out, []byte(b.String()), 0o644)
	t.Logf("wrote %s", out)
}

func hexBytes(b []byte) string {
	const hx = "0123456789abcdef"
	out := make([]byte, 0, len(b)*3)
	for _, x := range b {
		out = append(out, hx[x>>4], hx[x&0xf], ' ')
	}
	return string(out)
}
func asciiOf(b []byte) string {
	out := make([]byte, len(b))
	for i, x := range b {
		if x >= 32 && x < 127 {
			out[i] = x
		} else {
			out[i] = '.'
		}
	}
	return string(out)
}

// Command mj-mcp is a stdio MCP server exposing Midjourney generation tools to
// AI agents. It reuses mjclient (one stealth browser session) with a single-slot
// generation semaphore and the same ToS/credential posture as the CLI.
//
// Automating Midjourney violates their ToS; use your OWN account at low volume.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/ehmo/mj/internal/creds"
	"github.com/ehmo/mj/internal/mjapi"
	"github.com/ehmo/mj/internal/mjclient"
	"github.com/ehmo/mj/internal/mjconfig"
	"github.com/ehmo/mj/internal/mjversion"
)

const protocolVersion = "2024-11-05"

var version = "dev"

func displayVersion() string { return mjversion.Effective(version) }

func main() {
	cfg, err := mjconfig.Load()
	if err != nil {
		fail("load config: %v", err)
	}
	if !cfg.TOSAck {
		fail("ToS not acknowledged — run `mj login --i-understand` once before starting mj-mcp")
	}
	srv := &server{
		cfg:     cfg,
		sem:     make(chan struct{}, 1),
		out:     bufio.NewWriter(os.Stdout),
		cancels: map[string]context.CancelFunc{},
	}
	defer srv.close()
	defer srv.flush()

	// Requests are handled in their own goroutines so a long generation doesn't
	// freeze the whole server: reads (status/search/etc.) and cancellations are
	// served while an imagine is in flight. stdout writes are mutex-serialized.
	dec := json.NewDecoder(bufio.NewReader(os.Stdin))
	for {
		var req rpcReq
		if err := dec.Decode(&req); err != nil {
			// EOF / closed stdin: cancel any in-flight calls and wait for handlers
			// to flush their responses before exiting (don't drop pipelined replies).
			srv.cancelAll()
			srv.wg.Wait()
			return
		}
		r := req
		srv.wg.Add(1)
		go srv.handle(&r)
	}
}

func fail(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "mj-mcp: "+format+"\n", a...)
	os.Exit(1)
}

// ---- JSON-RPC ----

type rpcReq struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResp struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Result  any             `json:"result,omitempty"`
	Error   *rpcErr         `json:"error,omitempty"`
}

type rpcErr struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type server struct {
	cfg    mjconfig.Config
	mu     sync.Mutex
	client *mjclient.Client
	openMu sync.Mutex    // serializes the cold browser open (one profile = one instance)
	sem    chan struct{} // single-slot generation gate

	writeMu sync.Mutex // serializes stdout writes across concurrent handlers
	out     *bufio.Writer

	wg       sync.WaitGroup // tracks in-flight handler goroutines
	cancelMu sync.Mutex
	cancels  map[string]context.CancelFunc // in-flight tools/call ctx by request id
}

// handle processes one decoded request (in its own goroutine). Cancellation
// notifications abort the matching in-flight call; everything else dispatches
// and writes a response unless it is a notification.
func (s *server) handle(req *rpcReq) {
	defer s.wg.Done()
	if req.Method == "notifications/cancelled" {
		s.cancelRequest(req.Params)
		return
	}
	resp, isNotification := s.dispatch(req)
	if isNotification {
		return
	}
	s.writeResp(resp)
}

func (s *server) dispatch(req *rpcReq) (*rpcResp, bool) {
	resp := &rpcResp{JSONRPC: "2.0", ID: req.ID}
	// A request with no id, or an explicit JSON null id, is a notification (no
	// response per JSON-RPC 2.0).
	notification := len(req.ID) == 0 || string(req.ID) == "null"
	switch req.Method {
	case "initialize":
		resp.Result = map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo":      map[string]any{"name": "mj", "version": displayVersion()},
		}
	case "notifications/initialized":
		return nil, true
	case "ping":
		resp.Result = map[string]any{}
	case "tools/list":
		resp.Result = map[string]any{"tools": toolList()}
	case "tools/call":
		ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
		defer cancel()
		if !notification {
			s.registerCancel(string(req.ID), cancel)
			defer s.unregisterCancel(string(req.ID))
		}
		resp.Result = s.callTool(ctx, req.Params)
	default:
		if notification {
			return nil, true
		}
		resp.Error = &rpcErr{Code: -32601, Message: "method not found: " + req.Method}
	}
	return resp, notification
}

func (s *server) writeResp(resp *rpcResp) {
	b, _ := json.Marshal(resp)
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.out.Write(b)
	s.out.WriteByte('\n')
	s.out.Flush()
}

func (s *server) flush() {
	s.writeMu.Lock()
	defer s.writeMu.Unlock()
	s.out.Flush()
}

func (s *server) registerCancel(id string, cancel context.CancelFunc) {
	s.cancelMu.Lock()
	s.cancels[id] = cancel
	s.cancelMu.Unlock()
}

func (s *server) unregisterCancel(id string) {
	s.cancelMu.Lock()
	delete(s.cancels, id)
	s.cancelMu.Unlock()
}

// cancelRequest cancels the in-flight tools/call whose id matches the
// notifications/cancelled requestId (no-op if it already finished).
func (s *server) cancelRequest(params json.RawMessage) {
	var p struct {
		RequestID json.RawMessage `json:"requestId"`
	}
	if err := json.Unmarshal(params, &p); err != nil || len(p.RequestID) == 0 {
		return
	}
	s.cancelMu.Lock()
	cancel := s.cancels[string(p.RequestID)]
	s.cancelMu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// cancelAll cancels every in-flight tools/call (used on stdin EOF so a pending
// generation aborts promptly instead of blocking shutdown).
func (s *server) cancelAll() {
	s.cancelMu.Lock()
	for _, cancel := range s.cancels {
		cancel()
	}
	s.cancelMu.Unlock()
}

func (s *server) close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.client != nil {
		s.client.Close()
	}
}

func (s *server) getClient(ctx context.Context) (*mjclient.Client, error) {
	s.mu.Lock()
	c := s.client
	s.mu.Unlock()
	if c != nil {
		return c, nil
	}
	// Serialize the cold open so concurrent first calls don't each launch a
	// browser on the same persistent profile (a second Firefox instance on one
	// profile dir would fail/corrupt). openMu is separate from mu so close() and
	// already-open fast paths never block on a cold start.
	s.openMu.Lock()
	defer s.openMu.Unlock()
	s.mu.Lock()
	c = s.client
	s.mu.Unlock()
	if c != nil {
		return c, nil
	}
	profile, err := s.cfg.ProfilePath()
	if err != nil {
		return nil, err
	}
	c, err = mjclient.Open(ctx, mjclient.Config{
		ProfileDir:   profile,
		Headless:     true,
		RefreshToken: creds.FromEnv(),
		MinSubmitGap: time.Duration(s.cfg.ThrottleSecs) * time.Second,
		Creds:        creds.HaspStore{},
	})
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	s.client = c
	s.mu.Unlock()
	return c, nil
}

// ---- tools ----

func toolList() []map[string]any {
	obj := func(props map[string]any, required ...string) map[string]any {
		m := map[string]any{"type": "object", "properties": props}
		if len(required) > 0 {
			m["required"] = required
		}
		return m
	}
	str := map[string]any{"type": "string"}
	integer := map[string]any{"type": "integer"}
	number := map[string]any{"type": "number"}
	boolean := map[string]any{"type": "boolean"}
	return []map[string]any{
		{"name": "midjourney_imagine", "description": "Generate a 4-image grid from a text prompt. Waits for completion and returns CDN image URLs.",
			"inputSchema": obj(map[string]any{
				"prompt": str, "ar": str, "version": str, "mode": str, "stealth": boolean,
				"stylize": integer, "chaos": integer, "seed": integer, "no": str, "raw": str,
				"wait": boolean, "download_dir": str,
			}, "prompt")},
		{"name": "midjourney_variation", "description": "Create a variation of a grid cell (index 1-4).",
			"inputSchema": obj(map[string]any{"job_id": str, "index": integer, "strong": boolean, "wait": boolean, "download_dir": str}, "job_id", "index")},
		{"name": "midjourney_upscale", "description": "Upscale a grid cell (index 1-4). kind: creative|subtle.",
			"inputSchema": obj(map[string]any{"job_id": str, "index": integer, "kind": str, "wait": boolean, "download_dir": str}, "job_id", "index")},
		{"name": "midjourney_reroll", "description": "Re-run a grid's prompt.",
			"inputSchema": obj(map[string]any{"job_id": str, "wait": boolean}, "job_id")},
		{"name": "midjourney_video", "description": "Animate a grid cell into a short image-to-video clip. index 1-4; motion high|low (default low).",
			"inputSchema": obj(map[string]any{"job_id": str, "index": integer, "motion": str, "wait": boolean, "download_dir": str}, "job_id", "index")},
		{"name": "midjourney_status", "description": "Get current status for one or more job ids.",
			"inputSchema": obj(map[string]any{"job_ids": map[string]any{"type": "array", "items": str}}, "job_ids")},
		{"name": "midjourney_history", "description": "List the account's recent jobs.",
			"inputSchema": obj(map[string]any{"page_size": integer, "cursor": str})},
		{"name": "midjourney_account", "description": "Show plan and relax/stealth eligibility.",
			"inputSchema": obj(map[string]any{})},
		{"name": "midjourney_search", "description": "Semantic vector search over the public Midjourney explore gallery. Returns published community images with prompts and image URLs.",
			"inputSchema": obj(map[string]any{"query": str, "page": integer, "limit": integer}, "query")},
		{"name": "midjourney_explore", "description": "Browse the public explore gallery. feed: top|top_week|top_month|hot|random|videos.",
			"inputSchema": obj(map[string]any{"feed": str, "page": integer, "limit": integer})},
		{"name": "midjourney_likes", "description": "List the images the account has liked (page is 1-based).",
			"inputSchema": obj(map[string]any{"page": integer, "limit": integer})},
		{"name": "midjourney_styles", "description": "Browse the community style-reference gallery. Each result carries an sref code usable as --sref in imagine. page is 0-based.",
			"inputSchema": obj(map[string]any{"page": integer, "limit": integer})},
		{"name": "midjourney_profile", "description": "List a user's published spotlight gallery by username (username_v2).",
			"inputSchema": obj(map[string]any{"username": str, "limit": integer}, "username")},
		{"name": "midjourney_uploads", "description": "List the account's uploaded assets in personal storage (hosted URLs reusable as image prompts / references).",
			"inputSchema": obj(map[string]any{})},
		{"name": "midjourney_queue", "description": "List the account's running and waiting jobs.",
			"inputSchema": obj(map[string]any{})},
		{"name": "midjourney_moodboards", "description": "List the account's personalization moodboards.",
			"inputSchema": obj(map[string]any{})},
		{"name": "midjourney_folders", "description": "List the account's folders/collections (raw JSON).",
			"inputSchema": obj(map[string]any{})},
		{"name": "midjourney_profiles", "description": "List the account's personalization profiles (raw JSON). The ids are usable as the `profile` argument to midjourney_imagine.",
			"inputSchema": obj(map[string]any{})},
		{"name": "midjourney_describe", "description": "Describe an image (public URL or previously-uploaded URL) — returns ~4 suggested prompts. Synchronous.",
			"inputSchema": obj(map[string]any{"image": str}, "image")},
		{"name": "midjourney_retexture", "description": "Re-render an image's texture/style from a prompt while keeping its structure (image as depth reference). image = public URL (local paths require MJ_MCP_FILE_ROOT).",
			"inputSchema": obj(map[string]any{"image": str, "prompt": str, "ar": str, "wait": boolean, "download_dir": str}, "image", "prompt")},
		{"name": "midjourney_zoom", "description": "Zoom out (outpaint) an image into a wider scene. image = public URL (local paths require MJ_MCP_FILE_ROOT); factor > 1 (e.g. 1.5 or 2); prompt describes the surrounding scene.",
			"inputSchema": obj(map[string]any{"image": str, "prompt": str, "factor": number, "wait": boolean, "download_dir": str}, "image", "prompt")},
		{"name": "midjourney_pan", "description": "Pan (extend) an image in a direction by outpainting. image = public URL (local paths require MJ_MCP_FILE_ROOT); dir = left|right|up|down; amount = fraction of the image size to add (default 0.5).",
			"inputSchema": obj(map[string]any{"image": str, "prompt": str, "dir": str, "amount": number, "wait": boolean, "download_dir": str}, "image", "prompt", "dir")},
		{"name": "midjourney_vary_region", "description": "Regenerate a rectangular region of an image from a prompt (inpaint), keeping the rest. image = public URL (local paths require MJ_MCP_FILE_ROOT); region = {x,y,w,h} in source pixels.",
			"inputSchema": obj(map[string]any{"image": str, "prompt": str, "x": integer, "y": integer, "w": integer, "h": integer, "wait": boolean, "download_dir": str}, "image", "prompt", "x", "y", "w", "h")},
		{"name": "midjourney_api", "description": "Raw call to any midjourney.com/api endpoint (escape hatch for capabilities without a dedicated tool: folders, personalized-profiles, model-ratings, following-for-user, etc.). Returns raw JSON.",
			"inputSchema": obj(map[string]any{"method": str, "path": str, "body": str}, "path")},
		{"name": "midjourney_download", "description": "Download a completed job's images to a directory.",
			"inputSchema": obj(map[string]any{"job_id": str, "dir": str, "which": str}, "job_id", "dir")},
	}
}

type callParams struct {
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
}

func (s *server) callTool(ctx context.Context, raw json.RawMessage) map[string]any {
	var p callParams
	if err := json.Unmarshal(raw, &p); err != nil {
		return toolErr("bad params: " + err.Error())
	}
	args := map[string]any{}
	_ = json.Unmarshal(p.Arguments, &args)

	switch p.Name {
	case "midjourney_imagine":
		return s.guardedGen(ctx, func(c *mjclient.Client) (mjapi.Job, error) {
			return c.Imagine(ctx, mjclient.ImagineReq{
				Prompt:  getStr(args, "prompt"),
				Params:  argsToParams(args),
				Mode:    mjapi.Mode(getStrDef(args, "mode", "fast")),
				Private: getBool(args, "stealth"),
			})
		}, args)
	case "midjourney_variation":
		return s.guardedGen(ctx, func(c *mjclient.Client) (mjapi.Job, error) {
			return c.Vary(ctx, mjapi.JobID(getStr(args, "job_id")), getInt(args, "index"), getBool(args, "strong"))
		}, args)
	case "midjourney_upscale":
		kind := mjapi.UpscaleCreative
		if getStr(args, "kind") == "subtle" {
			kind = mjapi.UpscaleSubtle
		}
		return s.guardedGen(ctx, func(c *mjclient.Client) (mjapi.Job, error) {
			return c.Upscale(ctx, mjapi.JobID(getStr(args, "job_id")), getInt(args, "index"), kind)
		}, args)
	case "midjourney_reroll":
		return s.guardedGen(ctx, func(c *mjclient.Client) (mjapi.Job, error) {
			return c.Reroll(ctx, mjapi.JobID(getStr(args, "job_id")))
		}, args)
	case "midjourney_video":
		return s.guardedGen(ctx, func(c *mjclient.Client) (mjapi.Job, error) {
			return c.Video(ctx, mjclient.VideoReq{
				Parent: mjapi.JobID(getStr(args, "job_id")),
				Index:  getInt(args, "index"),
				Motion: getStr(args, "motion"),
			})
		}, args)
	case "midjourney_status":
		return s.toolStatus(ctx, args)
	case "midjourney_history":
		return s.toolHistory(ctx, args)
	case "midjourney_account":
		return s.toolAccount(ctx)
	case "midjourney_search":
		return s.toolSearch(ctx, args)
	case "midjourney_explore":
		return s.toolExplore(ctx, args)
	case "midjourney_likes":
		return s.simpleRead(ctx, func(c *mjclient.Client) (any, error) {
			return c.Likes(ctx, getInt(args, "page"))
		})
	case "midjourney_styles":
		return s.simpleRead(ctx, func(c *mjclient.Client) (any, error) {
			return c.Styles(ctx, getInt(args, "page"))
		})
	case "midjourney_profile":
		return s.simpleRead(ctx, func(c *mjclient.Client) (any, error) {
			return c.ProfileFeed(ctx, getStr(args, "username"))
		})
	case "midjourney_uploads":
		return s.simpleRead(ctx, func(c *mjclient.Client) (any, error) { return c.Uploads(ctx) })
	case "midjourney_queue":
		return s.simpleRead(ctx, func(c *mjclient.Client) (any, error) { return c.Queue(ctx) })
	case "midjourney_moodboards":
		return s.simpleRead(ctx, func(c *mjclient.Client) (any, error) { return c.Moodboards(ctx) })
	case "midjourney_folders":
		return s.rawRead(ctx, func(c *mjclient.Client) ([]byte, error) { return c.Folders(ctx) })
	case "midjourney_profiles":
		return s.rawRead(ctx, func(c *mjclient.Client) ([]byte, error) { return c.PersonalizedProfiles(ctx) })
	case "midjourney_retexture":
		image, err := s.resolveImage(args, "image")
		if err != nil {
			return toolErr(err.Error())
		}
		return s.guardedGen(ctx, func(c *mjclient.Client) (mjapi.Job, error) {
			return c.Retexture(ctx, image, getStr(args, "prompt"), getStr(args, "ar"), "fast")
		}, args)
	case "midjourney_zoom":
		image, err := s.resolveImage(args, "image")
		if err != nil {
			return toolErr(err.Error())
		}
		factor := getFloatDef(args, "factor", 2.0)
		return s.guardedGen(ctx, func(c *mjclient.Client) (mjapi.Job, error) {
			w, h, err := c.ImageSize(ctx, image)
			if err != nil {
				return mjapi.Job{}, err
			}
			padW := int(float64(w) * (factor - 1) / 2)
			padH := int(float64(h) * (factor - 1) / 2)
			return c.Outpaint(ctx, image, getStr(args, "prompt"), padW, padH, padW, padH, "fast")
		}, args)
	case "midjourney_pan":
		image, err := s.resolveImage(args, "image")
		if err != nil {
			return toolErr(err.Error())
		}
		amount := getFloatDef(args, "amount", 0.5)
		return s.guardedGen(ctx, func(c *mjclient.Client) (mjapi.Job, error) {
			w, h, err := c.ImageSize(ctx, image)
			if err != nil {
				return mjapi.Job{}, err
			}
			var l, t, r, b int
			switch getStr(args, "dir") {
			case "left":
				l = int(float64(w) * amount)
			case "right":
				r = int(float64(w) * amount)
			case "up", "top":
				t = int(float64(h) * amount)
			case "down", "bottom":
				b = int(float64(h) * amount)
			default:
				return mjapi.Job{}, fmt.Errorf("dir must be left|right|up|down")
			}
			return c.Outpaint(ctx, image, getStr(args, "prompt"), l, t, r, b, "fast")
		}, args)
	case "midjourney_vary_region":
		image, err := s.resolveImage(args, "image")
		if err != nil {
			return toolErr(err.Error())
		}
		return s.guardedGen(ctx, func(c *mjclient.Client) (mjapi.Job, error) {
			return c.VaryRegion(ctx, image, getStr(args, "prompt"),
				getInt(args, "x"), getInt(args, "y"), getInt(args, "w"), getInt(args, "h"), "fast")
		}, args)
	case "midjourney_describe":
		image, err := s.resolveImage(args, "image")
		if err != nil {
			return toolErr(err.Error())
		}
		c, err := s.getClient(ctx)
		if err != nil {
			return toolErr(err.Error())
		}
		prompts, err := c.DescribeImage(ctx, image)
		if err != nil {
			return toolErr(err.Error())
		}
		return toolJSON(map[string]any{"descriptions": prompts})
	case "midjourney_api":
		return s.toolAPI(ctx, args)
	case "midjourney_download":
		return s.toolDownload(ctx, args)
	default:
		return toolErr("unknown tool: " + p.Name)
	}
}

// guardedGen runs a submit under the single-slot semaphore, then waits/downloads.
func (s *server) guardedGen(ctx context.Context, submit func(*mjclient.Client) (mjapi.Job, error), args map[string]any) map[string]any {
	select {
	case s.sem <- struct{}{}:
		defer func() { <-s.sem }()
	default:
		return toolErr("another generation is in progress (retriable)")
	}
	c, err := s.getClient(ctx)
	if err != nil {
		return toolErr(err.Error())
	}
	job, err := submit(c)
	if err != nil {
		return toolErr(err.Error())
	}
	wait := true
	if v, ok := args["wait"]; ok {
		wait, _ = v.(bool)
	}
	final := job
	if wait {
		w, err := c.Wait(ctx, job.ID, mjclient.WaitOpts{Timeout: 6 * time.Minute})
		if errors.Is(err, mjclient.ErrJobFailed) {
			// terminal failure — tell the agent to STOP (don't keep polling)
			return toolErr(fmt.Sprintf("job %s %s (terminal — do not retry status)", w.ID, w.Status))
		}
		if err != nil {
			// still running at the 6-min cap: agent can poll via midjourney_status
			return toolJSON(map[string]any{"job_id": string(job.ID), "status": "pending", "note": err.Error()})
		}
		if w.BatchSize == 0 {
			w.BatchSize = job.BatchSize
		}
		final = w
	}
	res := jobResult(final)
	if dir := getStr(args, "download_dir"); dir != "" && final.Status.OK() {
		if files, err := c.Download(ctx, final, dir, mjclient.SelAll); err != nil {
			res["download_error"] = err.Error() // surface so the agent knows files weren't saved
		} else {
			res["files"] = files
		}
	}
	return toolJSON(res)
}

func (s *server) toolStatus(ctx context.Context, args map[string]any) map[string]any {
	var ids []mjapi.JobID
	if arr, ok := args["job_ids"].([]any); ok {
		for _, v := range arr {
			if str, ok := v.(string); ok {
				ids = append(ids, mjapi.JobID(str))
			}
		}
	}
	if len(ids) == 0 {
		return toolErr("job_ids required")
	}
	c, err := s.getClient(ctx)
	if err != nil {
		return toolErr(err.Error())
	}
	jobs, err := c.Status(ctx, ids...)
	if err != nil {
		return toolErr(err.Error())
	}
	return toolJSON(map[string]any{"jobs": jobs})
}

func (s *server) toolHistory(ctx context.Context, args map[string]any) map[string]any {
	c, err := s.getClient(ctx)
	if err != nil {
		return toolErr(err.Error())
	}
	jobs, next, err := c.History(ctx, getInt(args, "page_size"), getStr(args, "cursor"))
	if err != nil {
		return toolErr(err.Error())
	}
	return toolJSON(map[string]any{"jobs": jobs, "cursor": next})
}

func (s *server) toolSearch(ctx context.Context, args map[string]any) map[string]any {
	q := getStr(args, "query")
	if q == "" {
		return toolErr("query required")
	}
	c, err := s.getClient(ctx)
	if err != nil {
		return toolErr(err.Error())
	}
	page := getInt(args, "page")
	if page < 1 {
		page = 1
	}
	items, err := c.Search(ctx, q, page)
	if err != nil {
		return toolErr(err.Error())
	}
	return toolJSON(limitItems(items, getInt(args, "limit")))
}

func (s *server) toolExplore(ctx context.Context, args map[string]any) map[string]any {
	c, err := s.getClient(ctx)
	if err != nil {
		return toolErr(err.Error())
	}
	feed := mjclient.ExploreFeed(getStrDef(args, "feed", "top"))
	items, err := c.Explore(ctx, feed, getInt(args, "page"))
	if err != nil {
		return toolErr(err.Error())
	}
	return toolJSON(limitItems(items, getInt(args, "limit")))
}

// rawRead runs a read that returns raw JSON bytes (folders/profiles) and passes
// the body through verbatim as text.
func (s *server) rawRead(ctx context.Context, fn func(*mjclient.Client) ([]byte, error)) map[string]any {
	c, err := s.getClient(ctx)
	if err != nil {
		return toolErr(err.Error())
	}
	b, err := fn(c)
	if err != nil {
		return toolErr(err.Error())
	}
	return map[string]any{"content": []map[string]any{{"type": "text", "text": string(b)}}}
}

func (s *server) simpleRead(ctx context.Context, fn func(*mjclient.Client) (any, error)) map[string]any {
	c, err := s.getClient(ctx)
	if err != nil {
		return toolErr(err.Error())
	}
	v, err := fn(c)
	if err != nil {
		return toolErr(err.Error())
	}
	return toolJSON(v)
}

func limitItems(items []mjapi.ExploreItem, limit int) []mjapi.ExploreItem {
	if limit <= 0 {
		limit = 20
	}
	if len(items) > limit {
		return items[:limit]
	}
	return items
}

func (s *server) toolAccount(ctx context.Context) map[string]any {
	c, err := s.getClient(ctx)
	if err != nil {
		return toolErr(err.Error())
	}
	a, err := c.Account(ctx)
	if err != nil {
		return toolErr(err.Error())
	}
	return toolJSON(a)
}

func (s *server) toolAPI(ctx context.Context, args map[string]any) map[string]any {
	path := getStr(args, "path")
	if path == "" {
		return toolErr("path required")
	}
	if path[0] != '/' {
		path = "/" + path
	}
	method := strings.ToUpper(getStrDef(args, "method", "GET"))
	// The raw escape hatch is read-only by default on the (prompt-injectable) MCP
	// surface: a write would grant an agent full account control. Opt in with
	// MJ_MCP_ALLOW_RAW_WRITE=1. (The CLI `mj api` is operator-driven and stays
	// unrestricted.) Submit-jobs stays throttled in call() regardless.
	if method != "GET" && os.Getenv("MJ_MCP_ALLOW_RAW_WRITE") != "1" {
		return toolErr("raw API writes are disabled (set MJ_MCP_ALLOW_RAW_WRITE=1 to allow non-GET midjourney_api calls)")
	}
	c, err := s.getClient(ctx)
	if err != nil {
		return toolErr(err.Error())
	}
	body := getStr(args, "body")
	var b []byte
	if method == "GET" {
		b, err = c.Get(ctx, path)
	} else {
		var bb []byte
		if body != "" {
			bb = []byte(body)
		}
		b, err = c.PostJSON(ctx, path, bb)
	}
	if err != nil {
		return toolErr(err.Error())
	}
	return map[string]any{"content": []map[string]any{{"type": "text", "text": string(b)}}}
}

func (s *server) toolDownload(ctx context.Context, args map[string]any) map[string]any {
	c, err := s.getClient(ctx)
	if err != nil {
		return toolErr(err.Error())
	}
	id := mjapi.JobID(getStr(args, "job_id"))
	jobs, err := c.Status(ctx, id)
	if err != nil || len(jobs) == 0 {
		return toolErr("job not found")
	}
	which := mjclient.AssetSel(getStrDef(args, "which", "all"))
	files, err := c.Download(ctx, jobs[0], getStrDef(args, "dir", "."), which)
	if err != nil {
		return toolErr(err.Error())
	}
	return toolJSON(map[string]any{"files": files})
}

// ---- result helpers ----

func jobResult(j mjapi.Job) map[string]any {
	res := map[string]any{"job_id": string(j.ID), "status": string(j.Status)}
	var images []string
	for _, a := range mjapi.Assets(j) {
		switch a.Kind {
		case mjapi.AssetGrid:
			res["grid_url"] = a.URL
		case mjapi.AssetCell, mjapi.AssetVideo:
			images = append(images, a.URL)
		}
	}
	if images != nil {
		res["images"] = images
	}
	return res
}

func toolJSON(v any) map[string]any {
	b, _ := json.MarshalIndent(v, "", "  ")
	return map[string]any{"content": []map[string]any{{"type": "text", "text": string(b)}}}
}

func toolErr(msg string) map[string]any {
	return map[string]any{"isError": true, "content": []map[string]any{{"type": "text", "text": msg}}}
}

// ---- arg coercion ----

func getStr(m map[string]any, k string) string {
	if v, ok := m[k].(string); ok {
		return v
	}
	return ""
}
func getStrDef(m map[string]any, k, def string) string {
	if v := getStr(m, k); v != "" {
		return v
	}
	return def
}

// getBool accepts a JSON bool or a string "true"/"1" (agents sometimes stringify).
func getBool(m map[string]any, k string) bool {
	switch v := m[k].(type) {
	case bool:
		return v
	case string:
		return v == "true" || v == "1"
	}
	return false
}

func getInt(m map[string]any, k string) int { return int(getInt64(m, k)) }

// getInt64 accepts a JSON number or a numeric string (agents sometimes send "250").
func getInt64(m map[string]any, k string) int64 {
	switch v := m[k].(type) {
	case float64:
		return int64(v)
	case string:
		if n, err := strconv.ParseInt(strings.TrimSpace(v), 10, 64); err == nil {
			return n
		}
	}
	return 0
}

func getFloatDef(m map[string]any, k string, def float64) float64 {
	switch v := m[k].(type) {
	case float64:
		return v
	case string:
		if f, err := strconv.ParseFloat(strings.TrimSpace(v), 64); err == nil {
			return f
		}
	}
	return def
}

func argsToParams(m map[string]any) mjapi.Params {
	p := mjapi.Params{AR: getStr(m, "ar"), Version: getStr(m, "version"), Raw: getStr(m, "raw")}
	if v := getStr(m, "no"); v != "" {
		p.No = splitCSV(v)
	}
	if _, ok := m["stylize"]; ok {
		n := getInt(m, "stylize")
		p.Stylize = &n
	}
	if _, ok := m["chaos"]; ok {
		n := getInt(m, "chaos")
		p.Chaos = &n
	}
	if _, ok := m["seed"]; ok {
		n := getInt64(m, "seed")
		p.Seed = &n
	}
	return p
}

func splitCSV(s string) []string {
	var out []string
	cur := ""
	for _, r := range s {
		if r == ',' {
			out = append(out, cur)
			cur = ""
		} else {
			cur += string(r)
		}
	}
	if cur != "" {
		out = append(out, cur)
	}
	return out
}

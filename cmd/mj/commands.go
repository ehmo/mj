package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/ehmo/mj/internal/creds"
	"github.com/ehmo/mj/internal/mjapi"
	"github.com/ehmo/mj/internal/mjclient"
	"github.com/ehmo/mj/internal/mjconfig"
)

// ---- helpers ----

func openClient(ctx context.Context, headful bool) (*mjclient.Client, mjconfig.Config, error) {
	cfg, err := mjconfig.Load()
	if err != nil {
		return nil, cfg, err
	}
	profile, err := cfg.ProfilePath()
	if err != nil {
		return nil, cfg, err
	}
	if !headful {
		maybeAutostartDaemon(cfg) // transparent warm daemon when enabled
	}
	c, err := mjclient.Open(ctx, mjclient.Config{
		ProfileDir:   profile,
		Headless:     !headful,
		RefreshToken: creds.FromEnv(),
		MinSubmitGap: time.Duration(cfg.ThrottleSecs) * time.Second,
		Creds:        creds.HaspStore{},
	})
	return c, cfg, err
}

func ensureTOS(cfg mjconfig.Config) error {
	if !cfg.TOSAck {
		return fmt.Errorf("Terms-of-Service acknowledgement required — run `mj login --i-understand` first")
	}
	return nil
}

func exitCode(err error) int {
	var ae *mjclient.AuthError
	var api *mjclient.APIError
	switch {
	case errors.Is(err, mjclient.ErrWaitTimeout):
		return 5
	case errors.Is(err, mjclient.ErrJobFailed):
		return 4
	case errors.As(err, &ae):
		return 3
	case errors.As(err, &api):
		if api.Status == 403 {
			return 6
		}
		return 1
	}
	return 1
}

func printJSON(v any) {
	b, _ := json.MarshalIndent(v, "", "  ")
	fmt.Println(string(b))
}

// jobResult is the structured result for generate commands.
type jobResult struct {
	JobID   string   `json:"job_id"`
	Status  string   `json:"status,omitempty"`
	Images  []string `json:"images,omitempty"`
	GridURL string   `json:"grid_url,omitempty"`
	Files   []string `json:"files,omitempty"`
}

func buildResult(j mjapi.Job) jobResult {
	r := jobResult{JobID: string(j.ID), Status: string(j.Status)}
	for _, a := range mjapi.Assets(j) {
		switch a.Kind {
		case mjapi.AssetGrid:
			r.GridURL = a.URL
		case mjapi.AssetCell, mjapi.AssetVideo:
			r.Images = append(r.Images, a.URL)
		}
	}
	return r
}

func emitJob(jsonOut bool, j mjapi.Job, files []string) {
	r := buildResult(j)
	r.Files = files
	if jsonOut {
		printJSON(r)
		return
	}
	fmt.Printf("job %s  status=%s\n", r.JobID, orDash(r.Status))
	for _, u := range r.Images {
		fmt.Println("  ", u)
	}
	if r.GridURL != "" {
		fmt.Println("  grid:", r.GridURL)
	}
	for _, f := range r.Files {
		fmt.Println("  saved:", f)
	}
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

// assetOpts validates --format/--size into an mjapi.AssetOpts.
func assetOpts(format string, size int) (mjapi.AssetOpts, error) {
	switch format {
	case "", "png":
		format = "png"
	case "webp":
	default:
		return mjapi.AssetOpts{}, fmt.Errorf("--format must be png or webp (got %q)", format)
	}
	if size < 0 {
		return mjapi.AssetOpts{}, fmt.Errorf("--size must be >= 0")
	}
	return mjapi.AssetOpts{Format: format, Size: size}, nil
}

// runGenerated handles the shared --wait/--download tail for a submitted job.
func runGenerated(ctx context.Context, c *mjclient.Client, j mjapi.Job, wait bool, download string, jsonOut bool) error {
	final := j
	if wait || download != "" {
		w, err := c.Wait(ctx, j.ID, mjclient.WaitOpts{})
		if err != nil {
			return err
		}
		// Wait returns status-only job; merge batch metadata from submit.
		if w.BatchSize == 0 {
			w.BatchSize = j.BatchSize
		}
		final = w
	}
	var files []string
	if download != "" {
		fs, err := c.Download(ctx, final, download, mjclient.SelAll)
		if err != nil {
			return err
		}
		files = fs
	}
	emitJob(jsonOut, final, files)
	return nil
}

// ---- optional pointer-flag plumbing for imagine ----

type paramFlags struct {
	fs                                             *flag.FlagSet
	ar, version, no, profile, raw, mode, kind      *string
	stylize, chaos, weird, seed, stop, sw, sv, ow  *int
	cw                                             *int
	quality                                        *float64
	sref, oref, cref, image                        *multiString
	tile, styleRaw, stealth, strong, wait, watchWS *bool
	index                                          *int
	download                                       *string
	jsonOut                                        *bool
}

type multiString []string

func (m *multiString) String() string { return strings.Join(*m, ",") }
func (m *multiString) Set(v string) error {
	*m = append(*m, v)
	return nil
}

func newImagineFlags() *paramFlags {
	fs := flag.NewFlagSet("imagine", flag.ExitOnError)
	pf := &paramFlags{fs: fs, sref: &multiString{}, oref: &multiString{}, cref: &multiString{}, image: &multiString{}}
	pf.ar = fs.String("ar", "", "aspect ratio W:H")
	pf.version = fs.String("version", "", "model version: 7,8,8.1,niji 6,niji 7")
	pf.stylize = fs.Int("stylize", 0, "0..1000")
	pf.chaos = fs.Int("chaos", 0, "0..100")
	pf.weird = fs.Int("weird", 0, "0..3000")
	pf.quality = fs.Float64("quality", 0, "0.25/0.5/1/2/4")
	pf.seed = fs.Int("seed", 0, "0..4294967295")
	pf.no = fs.String("no", "", "negative terms, comma-separated")
	pf.tile = fs.Bool("tile", false, "seamless tiling")
	pf.stop = fs.Int("stop", 0, "10..100")
	fs.Var(pf.sref, "sref", "style reference url/code (repeatable)")
	pf.sw = fs.Int("sw", 0, "style weight 0..1000")
	pf.sv = fs.Int("sv", 0, "style version 1..6")
	fs.Var(pf.oref, "oref", "omni reference url (repeatable)")
	pf.ow = fs.Int("ow", 0, "omni weight 0..1000")
	fs.Var(pf.cref, "cref", "character reference url (repeatable)")
	fs.Var(pf.image, "image", "image prompt: local file or URL (repeatable)")
	pf.cw = fs.Int("cw", 0, "character weight 0..100")
	pf.profile = fs.String("profile", "", "personalization profile id")
	pf.styleRaw = fs.Bool("style-raw", false, "minimal styling")
	pf.raw = fs.String("raw", "", "verbatim trailing params")
	pf.mode = fs.String("mode", "fast", "fast|relax|turbo")
	pf.stealth = fs.Bool("stealth", false, "private/stealth (Pro/Mega)")
	pf.wait = fs.Bool("wait", false, "wait for completion")
	pf.watchWS = fs.Bool("watch", false, "stream live progress %% over WebSocket (runs locally)")
	pf.download = fs.String("download", "", "download results to dir")
	pf.jsonOut = fs.Bool("json", false, "JSON output")
	return pf
}

func (pf *paramFlags) params() mjapi.Params {
	set := map[string]bool{}
	pf.fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	p := mjapi.Params{
		AR: *pf.ar, Version: *pf.version, Profile: *pf.profile,
		Tile: *pf.tile, StyleRaw: *pf.styleRaw, Raw: *pf.raw,
		Sref: []string(*pf.sref), Oref: []string(*pf.oref), Cref: []string(*pf.cref),
	}
	if *pf.no != "" {
		p.No = strings.Split(*pf.no, ",")
	}
	if set["stylize"] {
		p.Stylize = pf.stylize
	}
	if set["chaos"] {
		p.Chaos = pf.chaos
	}
	if set["weird"] {
		p.Weird = pf.weird
	}
	if set["quality"] {
		p.Quality = pf.quality
	}
	if set["seed"] {
		v := int64(*pf.seed)
		p.Seed = &v
	}
	if set["stop"] {
		p.Stop = pf.stop
	}
	if set["sw"] {
		p.Sw = pf.sw
	}
	if set["sv"] {
		p.Sv = pf.sv
	}
	if set["ow"] {
		p.Ow = pf.ow
	}
	if set["cw"] {
		p.Cw = pf.cw
	}
	return p
}

// ---- commands ----

func cmdImagine(ctx context.Context, args []string) error {
	pf := newImagineFlags()
	pos := parseArgs(pf.fs, args)
	prompt := strings.TrimSpace(strings.Join(pos, " "))
	if prompt == "" {
		return fmt.Errorf("usage: mj imagine \"PROMPT\" [flags]")
	}
	params := pf.params()
	if warns, err := params.Validate(); err != nil {
		return err
	} else {
		for _, w := range warns {
			fmt.Fprintln(os.Stderr, "warning:", w)
		}
	}
	cfg, err := mjconfig.Load()
	if err != nil {
		return err
	}
	if err := ensureTOS(cfg); err != nil {
		return err
	}
	useWatch := *pf.watchWS
	var c *mjclient.Client
	if useWatch {
		c, _, err = openLocal(ctx) // WebSocket token is session-local; daemon doesn't expose it
	} else {
		c, _, err = openClient(ctx, false)
	}
	if err != nil {
		return err
	}
	defer c.Close()
	if imgs := []string(*pf.image); len(imgs) > 0 {
		var urls []string
		for _, im := range imgs {
			u, uerr := c.UploadIfLocal(ctx, im)
			if uerr != nil {
				return fmt.Errorf("image %q: %w", im, uerr)
			}
			urls = append(urls, u)
		}
		prompt = strings.Join(urls, " ") + " " + prompt
	}
	wsOpen := false
	if useWatch {
		wsOpen = c.WatchConnect(ctx) // subscribe BEFORE submit so the server streams this job's frames
	}
	j, err := c.Imagine(ctx, mjclient.ImagineReq{
		Prompt: prompt, Params: params, Mode: mjapi.Mode(*pf.mode), Private: *pf.stealth,
	})
	if err != nil {
		return err
	}
	if useWatch {
		return watchAndFinish(ctx, c, j, wsOpen, *pf.download, *pf.jsonOut)
	}
	return runGenerated(ctx, c, j, *pf.wait, *pf.download, *pf.jsonOut)
}

// watchAndFinish streams live progress for j, then downloads (optional) and emits.
func watchAndFinish(ctx context.Context, c *mjclient.Client, j mjapi.Job, wsOpen bool, download string, jsonOut bool) error {
	final, err := c.WatchJob(ctx, j.ID, wsOpen, 8*time.Minute, func(p mjapi.Progress) {
		if !jsonOut {
			fmt.Fprintf(os.Stderr, "\r%-10s %3d%%        ", orDash(string(p.Status)), p.Percent)
		}
	})
	if !jsonOut {
		fmt.Fprintln(os.Stderr)
	}
	if err != nil {
		return err
	}
	var files []string
	if download != "" && final.Status.OK() {
		if final.BatchSize == 0 {
			final.BatchSize = 4
		}
		fs2, derr := c.Download(ctx, final, download, mjclient.SelAll)
		files = fs2 // keep any partial successes
		if derr != nil {
			emitJob(jsonOut, final, files)
			return fmt.Errorf("download: %w", derr)
		}
	}
	emitJob(jsonOut, final, files)
	return nil
}

func cmdVary(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("vary", flag.ExitOnError)
	index := fs.Int("index", 0, "grid index 1..4")
	strong := fs.Bool("strong", false, "strong variation")
	wait := fs.Bool("wait", false, "")
	download := fs.String("download", "", "")
	jsonOut := fs.Bool("json", false, "")
	pos := parseArgs(fs, args)
	id, err := firstPos(pos, "vary JOB_ID --index N")
	if err != nil {
		return err
	}
	c, cfg, err := openClientWithTOS(ctx)
	if err != nil {
		return err
	}
	_ = cfg
	defer c.Close()
	j, err := c.Vary(ctx, mjapi.JobID(id), *index, *strong)
	if err != nil {
		return err
	}
	return runGenerated(ctx, c, j, *wait, *download, *jsonOut)
}

func cmdUpscale(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("upscale", flag.ExitOnError)
	index := fs.Int("index", 0, "grid index 1..4")
	kind := fs.String("kind", "creative", "creative|subtle")
	wait := fs.Bool("wait", false, "")
	download := fs.String("download", "", "")
	jsonOut := fs.Bool("json", false, "")
	pos := parseArgs(fs, args)
	id, err := firstPos(pos, "upscale JOB_ID --index N")
	if err != nil {
		return err
	}
	uk := mjapi.UpscaleCreative
	if *kind == "subtle" {
		uk = mjapi.UpscaleSubtle
	}
	c, _, err := openClientWithTOS(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	j, err := c.Upscale(ctx, mjapi.JobID(id), *index, uk)
	if err != nil {
		return err
	}
	return runGenerated(ctx, c, j, *wait, *download, *jsonOut)
}

func cmdReroll(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("reroll", flag.ExitOnError)
	wait := fs.Bool("wait", false, "")
	jsonOut := fs.Bool("json", false, "")
	pos := parseArgs(fs, args)
	id, err := firstPos(pos, "reroll JOB_ID")
	if err != nil {
		return err
	}
	c, _, err := openClientWithTOS(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	j, err := c.Reroll(ctx, mjapi.JobID(id))
	if err != nil {
		return err
	}
	return runGenerated(ctx, c, j, *wait, "", *jsonOut)
}

func cmdVideo(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("video", flag.ExitOnError)
	index := fs.Int("index", 0, "grid index 1..4")
	motion := fs.String("motion", "low", "high|low")
	wait := fs.Bool("wait", false, "")
	download := fs.String("download", "", "")
	jsonOut := fs.Bool("json", false, "")
	pos := parseArgs(fs, args)
	id, err := firstPos(pos, "video JOB_ID --index N")
	if err != nil {
		return err
	}
	c, _, err := openClientWithTOS(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	j, err := c.Video(ctx, mjclient.VideoReq{Parent: mjapi.JobID(id), Index: *index, Motion: *motion})
	if err != nil {
		return err
	}
	final := j
	if *wait || *download != "" {
		w, werr := c.Wait(ctx, j.ID, mjclient.WaitOpts{Timeout: 15 * time.Minute})
		if werr != nil {
			return werr
		}
		if w.BatchSize == 0 {
			w.BatchSize = j.BatchSize
		}
		final = w
	}
	var files []string
	if *download != "" {
		fs2, derr := c.Download(ctx, final, *download, mjclient.SelAll)
		if derr != nil {
			return derr
		}
		files = fs2
	}
	emitJob(*jsonOut, final, files)
	return nil
}

func cmdStatus(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("status", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "")
	pos := parseArgs(fs, args)
	if len(pos) == 0 {
		return fmt.Errorf("usage: mj status JOB_ID...")
	}
	ids := make([]mjapi.JobID, len(pos))
	for i, a := range pos {
		ids[i] = mjapi.JobID(a)
	}
	c, _, err := openClient(ctx, false)
	if err != nil {
		return err
	}
	defer c.Close()
	jobs, err := c.Status(ctx, ids...)
	if err != nil {
		return err
	}
	if *jsonOut {
		printJSON(jobs)
		return nil
	}
	for _, j := range jobs {
		fmt.Printf("%s  %s  %s  %dx%d\n", j.ID, orDash(string(j.Status)), j.JobType, j.Width, j.Height)
	}
	return nil
}

func cmdWait(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("wait", flag.ExitOnError)
	timeout := fs.Duration("timeout", 5*time.Minute, "")
	jsonOut := fs.Bool("json", false, "")
	pos := parseArgs(fs, args)
	id, err := firstPos(pos, "wait JOB_ID")
	if err != nil {
		return err
	}
	c, _, err := openClient(ctx, false)
	if err != nil {
		return err
	}
	defer c.Close()
	j, err := c.Wait(ctx, mjapi.JobID(id), mjclient.WaitOpts{Timeout: *timeout})
	if err != nil {
		return err
	}
	emitJob(*jsonOut, j, nil)
	return nil
}

func cmdHistory(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("history", flag.ExitOnError)
	pageSize := fs.Int("page-size", 50, "")
	cursor := fs.String("cursor", "", "")
	jsonOut := fs.Bool("json", false, "")
	_ = fs.Parse(args)
	c, _, err := openClient(ctx, false)
	if err != nil {
		return err
	}
	defer c.Close()
	jobs, next, err := c.History(ctx, *pageSize, *cursor)
	if err != nil {
		return err
	}
	if *jsonOut {
		printJSON(map[string]any{"jobs": jobs, "cursor": next})
		return nil
	}
	for _, j := range jobs {
		fmt.Printf("%s  %s  %s\n", j.ID, j.EventType, truncate(j.FullCommand, 70))
	}
	if next != "" {
		fmt.Fprintln(os.Stderr, "next cursor:", next)
	}
	return nil
}

func cmdDownload(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("download", flag.ExitOnError)
	dir := fs.String("dir", ".", "")
	which := fs.String("which", "all", "grid|cells|all")
	format := fs.String("format", "png", "png|webp (webp is ~5x smaller, same resolution)")
	size := fs.Int("size", 0, "thumbnail edge in px (e.g. 384, 640); 0 = full resolution")
	pos := parseArgs(fs, args)
	id, err := firstPos(pos, "download JOB_ID")
	if err != nil {
		return err
	}
	opts, err := assetOpts(*format, *size)
	if err != nil {
		return err
	}
	c, _, err := openClient(ctx, false)
	if err != nil {
		return err
	}
	defer c.Close()
	jobs, err := c.Status(ctx, mjapi.JobID(id))
	if err != nil {
		return err
	}
	if len(jobs) == 0 {
		return fmt.Errorf("job %s not found", id)
	}
	files, err := c.DownloadOpts(ctx, jobs[0], *dir, mjclient.AssetSel(*which), opts)
	if err != nil {
		return err
	}
	for _, f := range files {
		fmt.Println(f)
	}
	return nil
}

func cmdAccount(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("account", flag.ExitOnError)
	jsonOut := fs.Bool("json", false, "")
	_ = fs.Parse(args)
	c, _, err := openClient(ctx, false)
	if err != nil {
		return err
	}
	defer c.Close()
	a, err := c.Account(ctx)
	if err != nil {
		return err
	}
	if *jsonOut {
		printJSON(a)
		return nil
	}
	fmt.Printf("user:    %s\n", a.UserID)
	fmt.Printf("email:   %s\n", a.Email)
	fmt.Printf("plan:    %s\n", orDash(a.PlanType))
	fmt.Printf("relax:   %v\n", a.CanRelax)
	fmt.Printf("stealth: %v\n", a.CanPrivate)
	fmt.Printf("default: %s\n", orDash(a.DefaultSpeed))
	return nil
}

// ---- small utilities ----

// parseArgs parses flags that may appear before and/or after positional args
// (Go's flag stops at the first positional). It extracts leading non-flag
// tokens, parses the remainder, and returns all positionals (leading + trailing).
func parseArgs(fs *flag.FlagSet, args []string) []string {
	i := 0
	for i < len(args) && !strings.HasPrefix(args[i], "-") {
		i++
	}
	lead := append([]string(nil), args[:i]...)
	_ = fs.Parse(args[i:])
	return append(lead, fs.Args()...)
}

func firstPos(pos []string, usage string) (string, error) {
	if len(pos) < 1 {
		return "", fmt.Errorf("usage: mj %s", usage)
	}
	return pos[0], nil
}

func openClientWithTOS(ctx context.Context) (*mjclient.Client, mjconfig.Config, error) {
	cfg, err := mjconfig.Load()
	if err != nil {
		return nil, cfg, err
	}
	if err := ensureTOS(cfg); err != nil {
		return nil, cfg, err
	}
	c, cfg2, err := openClient(ctx, false)
	return c, cfg2, err
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

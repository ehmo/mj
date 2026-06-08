package main

import (
	"context"
	"flag"
	"fmt"
	"strconv"
	"strings"

	"github.com/ehmo/mj/internal/mjapi"
)

// cmdRetexture: mj retexture IMAGE "PROMPT" [--ar W:H] [--wait] [--download DIR]
func cmdRetexture(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("retexture", flag.ExitOnError)
	ar := fs.String("ar", "", "aspect ratio W:H")
	mode := fs.String("mode", "fast", "fast|relax|turbo")
	wait := fs.Bool("wait", false, "")
	download := fs.String("download", "", "")
	jsonOut := fs.Bool("json", false, "")
	pos := parseArgs(fs, args)
	if len(pos) < 2 {
		return fmt.Errorf("usage: mj retexture IMAGE \"PROMPT\" [--ar W:H]")
	}
	source := pos[0]
	prompt := strings.TrimSpace(strings.Join(pos[1:], " "))
	c, _, err := openClientWithTOS(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	j, err := c.Retexture(ctx, source, prompt, *ar, mjapi.Mode(*mode))
	if err != nil {
		return err
	}
	return runGenerated(ctx, c, j, *wait, *download, *jsonOut)
}

// cmdOutpaint: mj outpaint IMAGE "PROMPT" [--left N --top N --right N --bottom N] [--mode]
func cmdOutpaint(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("outpaint", flag.ExitOnError)
	left := fs.Int("left", 0, "pixels to add on the left")
	top := fs.Int("top", 0, "pixels to add on top")
	right := fs.Int("right", 0, "pixels to add on the right")
	bottom := fs.Int("bottom", 0, "pixels to add on the bottom")
	mode := fs.String("mode", "fast", "fast|relax|turbo")
	wait := fs.Bool("wait", false, "")
	download := fs.String("download", "", "")
	jsonOut := fs.Bool("json", false, "")
	pos := parseArgs(fs, args)
	if len(pos) < 2 {
		return fmt.Errorf("usage: mj outpaint IMAGE \"PROMPT\" [--left/--top/--right/--bottom N]")
	}
	source := pos[0]
	prompt := strings.TrimSpace(strings.Join(pos[1:], " "))
	c, _, err := openClientWithTOS(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	j, err := c.Outpaint(ctx, source, prompt, *left, *top, *right, *bottom, mjapi.Mode(*mode))
	if err != nil {
		return err
	}
	return runGenerated(ctx, c, j, *wait, *download, *jsonOut)
}

// cmdZoom: mj zoom IMAGE "PROMPT" [--factor 2] [--mode]  (symmetric outpaint)
func cmdZoom(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("zoom", flag.ExitOnError)
	factor := fs.Float64("factor", 2.0, "zoom-out factor, e.g. 1.5 or 2")
	mode := fs.String("mode", "fast", "fast|relax|turbo")
	wait := fs.Bool("wait", false, "")
	download := fs.String("download", "", "")
	jsonOut := fs.Bool("json", false, "")
	pos := parseArgs(fs, args)
	if len(pos) < 2 {
		return fmt.Errorf("usage: mj zoom IMAGE \"PROMPT\" [--factor 2]")
	}
	if *factor <= 1.0 {
		return fmt.Errorf("zoom factor must be > 1 (got %v)", *factor)
	}
	source := pos[0]
	prompt := strings.TrimSpace(strings.Join(pos[1:], " "))
	c, _, err := openClientWithTOS(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	w, h, err := c.ImageSize(ctx, source)
	if err != nil {
		return err
	}
	padW := int(float64(w) * (*factor - 1) / 2)
	padH := int(float64(h) * (*factor - 1) / 2)
	j, err := c.Outpaint(ctx, source, prompt, padW, padH, padW, padH, mjapi.Mode(*mode))
	if err != nil {
		return err
	}
	return runGenerated(ctx, c, j, *wait, *download, *jsonOut)
}

// cmdPan: mj pan IMAGE "PROMPT" --dir left|right|up|down [--amount 0.5] [--mode]
func cmdPan(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("pan", flag.ExitOnError)
	dir := fs.String("dir", "", "left|right|up|down")
	amount := fs.Float64("amount", 0.5, "fraction of the image size to extend (0..2)")
	mode := fs.String("mode", "fast", "fast|relax|turbo")
	wait := fs.Bool("wait", false, "")
	download := fs.String("download", "", "")
	jsonOut := fs.Bool("json", false, "")
	pos := parseArgs(fs, args)
	if len(pos) < 2 {
		return fmt.Errorf("usage: mj pan IMAGE \"PROMPT\" --dir left|right|up|down [--amount 0.5]")
	}
	if *amount <= 0 {
		return fmt.Errorf("--amount must be > 0")
	}
	source := pos[0]
	prompt := strings.TrimSpace(strings.Join(pos[1:], " "))
	c, _, err := openClientWithTOS(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	w, h, err := c.ImageSize(ctx, source)
	if err != nil {
		return err
	}
	var l, t, r, b int
	switch strings.ToLower(*dir) {
	case "left":
		l = int(float64(w) * *amount)
	case "right":
		r = int(float64(w) * *amount)
	case "up", "top":
		t = int(float64(h) * *amount)
	case "down", "bottom":
		b = int(float64(h) * *amount)
	default:
		return fmt.Errorf("--dir must be left|right|up|down (got %q)", *dir)
	}
	j, err := c.Outpaint(ctx, source, prompt, l, t, r, b, mjapi.Mode(*mode))
	if err != nil {
		return err
	}
	return runGenerated(ctx, c, j, *wait, *download, *jsonOut)
}

// cmdVaryRegion: mj vary-region IMAGE "PROMPT" --region X,Y,W,H [--mode]
func cmdVaryRegion(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("vary-region", flag.ExitOnError)
	region := fs.String("region", "", "X,Y,W,H rectangle (source pixels) to regenerate")
	mode := fs.String("mode", "fast", "fast|relax|turbo")
	wait := fs.Bool("wait", false, "")
	download := fs.String("download", "", "")
	jsonOut := fs.Bool("json", false, "")
	pos := parseArgs(fs, args)
	if len(pos) < 2 {
		return fmt.Errorf("usage: mj vary-region IMAGE \"PROMPT\" --region X,Y,W,H")
	}
	x, y, rw, rh, err := parseRegion(*region)
	if err != nil {
		return err
	}
	source := pos[0]
	prompt := strings.TrimSpace(strings.Join(pos[1:], " "))
	c, _, err := openClientWithTOS(ctx)
	if err != nil {
		return err
	}
	defer c.Close()
	j, err := c.VaryRegion(ctx, source, prompt, x, y, rw, rh, mjapi.Mode(*mode))
	if err != nil {
		return err
	}
	return runGenerated(ctx, c, j, *wait, *download, *jsonOut)
}

func parseRegion(s string) (x, y, w, h int, err error) {
	parts := strings.Split(strings.TrimSpace(s), ",")
	if len(parts) != 4 {
		return 0, 0, 0, 0, fmt.Errorf("--region must be X,Y,W,H")
	}
	v := make([]int, 4)
	for i, p := range parts {
		n, e := strconv.Atoi(strings.TrimSpace(p))
		if e != nil {
			return 0, 0, 0, 0, fmt.Errorf("--region: %q is not an integer", p)
		}
		v[i] = n
	}
	return v[0], v[1], v[2], v[3], nil
}

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/ehmo/mj/internal/creds"
	"github.com/ehmo/mj/internal/mjapi"
	"github.com/ehmo/mj/internal/mjclient"
	"github.com/ehmo/mj/internal/mjconfig"
)

// openLocal builds a local (non-daemon) client. WatchProgress needs the
// session-local websocketToken, which the daemon does not expose.
func openLocal(ctx context.Context) (*mjclient.Client, mjconfig.Config, error) {
	cfg, err := mjconfig.Load()
	if err != nil {
		return nil, cfg, err
	}
	profile, err := cfg.ProfilePath()
	if err != nil {
		return nil, cfg, err
	}
	c, err := mjclient.New(ctx, mjclient.Config{
		ProfileDir:   profile,
		Headless:     true,
		RefreshToken: creds.FromEnv(),
		MinSubmitGap: time.Duration(cfg.ThrottleSecs) * time.Second,
		Creds:        creds.HaspStore{},
	})
	return c, cfg, err
}

// cmdWatch streams realtime progress (live %) for a job over the WebSocket.
func cmdWatch(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("watch", flag.ExitOnError)
	timeout := fs.Duration("timeout", 6*time.Minute, "")
	download := fs.String("download", "", "download on completion")
	jsonOut := fs.Bool("json", false, "")
	pos := parseArgs(fs, args)
	id, err := firstPos(pos, "watch JOB_ID")
	if err != nil {
		return err
	}
	c, _, err := openLocal(ctx)
	if err != nil {
		return err
	}
	defer c.Close()

	wsOpen := c.WatchConnect(ctx)
	final, err := c.WatchJob(ctx, mjapi.JobID(id), wsOpen, *timeout, func(p mjapi.Progress) {
		if !*jsonOut {
			fmt.Fprintf(os.Stderr, "\r%-10s %3d%%        ", orDash(string(p.Status)), p.Percent)
		}
	})
	if !*jsonOut {
		fmt.Fprintln(os.Stderr)
	}
	if err != nil {
		return err
	}
	var files []string
	if *download != "" && final.Status.OK() {
		if final.BatchSize == 0 {
			final.BatchSize = 4
		}
		fs2, derr := c.Download(ctx, final, *download, mjclient.SelAll)
		files = fs2
		if derr != nil {
			emitJob(*jsonOut, final, files)
			return fmt.Errorf("download: %w", derr)
		}
	}
	emitJob(*jsonOut, final, files)
	return nil
}

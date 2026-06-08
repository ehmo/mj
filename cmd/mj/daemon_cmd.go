package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/ehmo/mj/internal/creds"
	"github.com/ehmo/mj/internal/mjclient"
	"github.com/ehmo/mj/internal/mjconfig"
)

// cmdServe runs the daemon: one warm Camoufox browser shared by all CLI/MCP
// clients over a unix socket, eliminating per-command cold start.
func cmdServe(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("serve", flag.ExitOnError)
	idle := fs.Duration("idle", 0, "shut down after this much inactivity (0 = run until stopped)")
	_ = fs.Parse(args)

	cfg, err := mjconfig.Load()
	if err != nil {
		return err
	}
	profile, err := cfg.ProfilePath()
	if err != nil {
		return err
	}
	c, err := mjclient.New(ctx, mjclient.Config{
		ProfileDir:   profile,
		Headless:     true,
		RefreshToken: creds.FromEnv(),
		MinSubmitGap: time.Duration(cfg.ThrottleSecs) * time.Second,
		Creds:        creds.HaspStore{},
	})
	if err != nil {
		return err
	}
	defer c.Close()
	sock := mjclient.DaemonSocketPath()
	fmt.Fprintf(os.Stderr, "mj daemon listening on %s (warm browser ready; Ctrl-C to stop)\n", sock)
	return mjclient.ServeDaemon(ctx, sock, c, *idle)
}

// cmdDaemon manages the daemon: status | stop.
func cmdDaemon(ctx context.Context, args []string) error {
	sub := "status"
	if len(args) > 0 {
		sub = args[0]
	}
	sock := mjclient.DaemonSocketPath()
	switch sub {
	case "status":
		if mjclient.DaemonAvailable(sock) {
			fmt.Printf("running (%s)\n", sock)
		} else {
			fmt.Println("not running")
		}
	case "stop":
		if err := mjclient.StopDaemon(sock); err != nil {
			return fmt.Errorf("stop: %w (daemon not running?)", err)
		}
		fmt.Println("stopped")
	default:
		return fmt.Errorf("usage: mj daemon status|stop")
	}
	return nil
}

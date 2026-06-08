// Command mj is the unofficial Midjourney web CLI. It drives a stealth Camoufox
// browser (via gomoufox) to use a user's OWN Midjourney account.
//
// This tool automates access to Midjourney, which their Terms of Service
// prohibit; accounts can be permanently banned. Use your own account, at low
// volume, at your own risk. See `mj login`.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/ehmo/mj/internal/mjversion"
)

var version = "dev"

const tosClause = `Midjourney's Terms of Service prohibit automated access:
  "You may not use automated tools to access, interact with, or generate
   Assets through the Services."
Accounts have been permanently banned for automation. By continuing you accept
that risk, on your OWN account only. Pass --i-understand to acknowledge.`

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	args := os.Args[1:]
	if len(args) == 0 {
		usage()
		os.Exit(2)
	}
	cmd, rest := args[0], args[1:]

	var err error
	switch cmd {
	case "help", "-h", "--help":
		if len(rest) > 0 {
			if !printCommandHelp(rest[0]) {
				usage()
				os.Exit(2)
			}
			return
		}
		usage()
		return
	case "version", "--version", "-v":
		fmt.Println("mj", mjversion.Effective(version))
		return
	case "login":
		err = cmdLogin(ctx, rest)
	case "logout":
		err = cmdLogout(ctx, rest)
	case "auth":
		err = cmdAuth(ctx, rest)
	case "doctor":
		err = cmdDoctor(ctx, rest)
	case "serve":
		err = cmdServe(ctx, rest)
	case "daemon":
		err = cmdDaemon(ctx, rest)
	case "account":
		err = cmdAccount(ctx, rest)
	case "search":
		err = cmdSearch(ctx, rest)
	case "explore":
		err = cmdExplore(ctx, rest)
	case "likes":
		err = cmdLikes(ctx, rest)
	case "styles":
		err = cmdStyles(ctx, rest)
	case "profile":
		err = cmdProfile(ctx, rest)
	case "uploads":
		err = cmdUploads(ctx, rest)
	case "liked-styles":
		err = cmdLikedStyles(ctx, rest)
	case "folders":
		err = cmdFolders(ctx, rest)
	case "profiles":
		err = cmdProfiles(ctx, rest)
	case "queue":
		err = cmdQueue(ctx, rest)
	case "moodboards":
		err = cmdMoodboards(ctx, rest)
	case "api":
		err = cmdAPI(ctx, rest)
	case "imagine":
		err = cmdImagine(ctx, rest)
	case "vary":
		err = cmdVary(ctx, rest)
	case "upscale":
		err = cmdUpscale(ctx, rest)
	case "reroll":
		err = cmdReroll(ctx, rest)
	case "video":
		err = cmdVideo(ctx, rest)
	case "describe":
		err = cmdDescribe(ctx, rest)
	case "retexture":
		err = cmdRetexture(ctx, rest)
	case "outpaint":
		err = cmdOutpaint(ctx, rest)
	case "zoom":
		err = cmdZoom(ctx, rest)
	case "pan":
		err = cmdPan(ctx, rest)
	case "vary-region":
		err = cmdVaryRegion(ctx, rest)
	case "status":
		err = cmdStatus(ctx, rest)
	case "wait":
		err = cmdWait(ctx, rest)
	case "watch":
		err = cmdWatch(ctx, rest)
	case "history":
		err = cmdHistory(ctx, rest)
	case "download":
		err = cmdDownload(ctx, rest)
	default:
		fmt.Fprintf(os.Stderr, "mj: unknown command %q\n\n", cmd)
		usage()
		os.Exit(2)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "mj:", err)
		os.Exit(exitCode(err))
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `mj — unofficial Midjourney web CLI (use your OWN account; ToS-risky)

Auth:
  mj login [--i-understand]      one-time interactive login (headful)
  mj logout                      remove local session + stored token
  mj auth status                 show auth/session state
  mj account                     plan, relax/stealth eligibility
  mj doctor                      check toolchain (gomoufox, hasp)
  mj serve                       run a warm daemon (fast CLI/MCP; shares one browser)
  mj daemon status|stop          manage the daemon

Generate:
  mj imagine "PROMPT" [--ar 3:2] [--version 7] [--mode fast|relax|turbo] [--stealth]
        [--stylize N] [--chaos N] [--weird N] [--quality Q] [--seed N] [--no a,b]
        [--tile] [--stop N] [--sref U]... [--sw N] [--sv N] [--oref U]... [--ow N]
        [--cref U]... [--cw N] [--profile ID] [--style-raw] [--raw "..."]
        [--wait] [--download DIR] [--json]
  mj vary    JOB_ID --index N [--strong] [--wait] [--download DIR]
  mj upscale JOB_ID --index N [--kind creative|subtle] [--wait] [--download DIR]
  mj reroll  JOB_ID [--wait]
  mj video   JOB_ID --index N [--motion high|low] [--wait] [--download DIR]
  mj describe IMAGE             suggested prompts for an image (URL or local file)
  mj retexture IMAGE "PROMPT" [--ar W:H]   re-render texture/style (keeps structure)
  mj zoom      IMAGE "PROMPT" [--factor 2] zoom out (outpaint a wider scene)
  mj pan       IMAGE "PROMPT" --dir left|right|up|down [--amount 0.5]
  mj outpaint  IMAGE "PROMPT" [--left/--top/--right/--bottom N]  extend canvas
  mj vary-region IMAGE "PROMPT" --region X,Y,W,H   regenerate a rectangle

Discover:
  mj search "QUERY" [--page N] [--limit N] [--download DIR] [--format webp] [--size N]
  mj explore [--feed top|top_week|top_month|hot|random|videos] [--page N] [--limit N] [--download DIR] [--format webp] [--size N]
  mj likes [--page N] [--download DIR]                     your liked images
  mj styles [--page N] [--limit N]                          browse community style refs (--sref codes)
  mj profile USERNAME [--limit N] [--download DIR]         a user's spotlight gallery
  mj uploads [--download DIR]                              your uploaded assets (storage)
  mj liked-styles                                          your liked style refs (raw JSON)
  mj folders                                               your folders/collections (raw JSON)
  mj profiles                                              your personalization profiles (--profile ids)
  mj queue                      running/waiting jobs
  mj moodboards                 personalization moodboards
  mj api METHOD PATH [--data J]  raw call to any /api endpoint (folders, profiles, ratings, …)

Jobs:
  mj status JOB_ID...            current status
  mj wait   JOB_ID [--timeout 5m]
  mj watch  JOB_ID [--download DIR]    live progress %% over WebSocket
  mj history [--page-size N] [--cursor C] [--json]
  mj download JOB_ID [--dir .] [--which grid|cells|all] [--format png|webp] [--size 384]

Credentials are read from the MIDJOURNEY_FIREBASE_REFRESH_TOKEN env (use
`+"`hasp inject`"+`) or the local browser profile after `+"`mj login`"+`.
`)
}

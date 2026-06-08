package main

import (
	"fmt"
	"os"
	"sort"
	"strings"
)

// commandHelp holds focused per-command help: a one-line synopsis, the flag
// list, and a couple of examples. `mj help <cmd>` prints this instead of the
// dense global usage screen.
var commandHelp = map[string]string{
	"imagine": `mj imagine "PROMPT" [flags]   generate a 4-image grid from a text prompt

Flags:
  --ar W:H              aspect ratio (e.g. 3:2)
  --version V           model: 7, 8, 8.1, niji 6, niji 7
  --mode M             fast|relax|turbo (relax/turbo are plan-gated)
  --stealth            private generation (Pro/Mega)
  --stylize N          0..1000      --chaos N   0..100      --weird N  0..3000
  --quality Q          0.25|0.5|1|2|4              --seed N  0..4294967295
  --no a,b             negative terms (comma-separated)
  --tile               --stop N (10..100)   --style-raw   --raw "..."
  --sref U.. --sw N --sv N   --oref U.. --ow N   --cref U.. --cw N   --profile ID
  --image FILE|URL     image prompt (repeatable; local files are uploaded)
  --wait               block until the job completes
  --watch              stream live 0->100% over WebSocket (runs locally)
  --download DIR       save results to DIR        --json   machine output

Examples:
  mj imagine "a teal bicycle on a pink wall --ar 3:2" --wait --download ./out
  mj imagine "a koi in dark water" --watch --download ./out
  mj imagine "logo, minimal" --version 7 --stylize 250 --json`,

	"vary":        "mj vary JOB_ID --index N [--strong] [--wait] [--download DIR]\n\nCreate a variation of grid cell N (1..4).",
	"upscale":     "mj upscale JOB_ID --index N [--kind creative|subtle] [--wait] [--download DIR]\n\nUpscale grid cell N (1..4).",
	"reroll":      "mj reroll JOB_ID [--wait] [--json]\n\nRe-run a grid's prompt.",
	"video":       "mj video JOB_ID --index N [--motion high|low] [--wait] [--download DIR]\n\nRender an image-to-video clip from grid cell N.",
	"describe":    "mj describe IMAGE [--json]\n\nReturn ~4 suggested prompts for an image. IMAGE is a URL or a local file\n(local files are uploaded first).",
	"retexture":   "mj retexture IMAGE \"PROMPT\" [--ar W:H] [--wait] [--download DIR]\n\nRe-render an image's texture/style from PROMPT while keeping its structure.",
	"zoom":        "mj zoom IMAGE \"PROMPT\" [--factor 2] [--wait] [--download DIR]\n\nZoom out (outpaint) into a wider scene. factor > 1.",
	"pan":         "mj pan IMAGE \"PROMPT\" --dir left|right|up|down [--amount 0.5] [--wait] [--download DIR]\n\nExtend (outpaint) the image in one direction.",
	"outpaint":    "mj outpaint IMAGE \"PROMPT\" [--left N] [--top N] [--right N] [--bottom N] [--wait]\n\nExtend the canvas by pixel margins and fill the new area from PROMPT.",
	"vary-region": "mj vary-region IMAGE \"PROMPT\" --region X,Y,W,H [--wait] [--download DIR]\n\nRegenerate a rectangular region (inpaint), keeping the rest.",

	"search":       "mj search \"QUERY\" [--page N] [--limit N] [--download DIR] [--format png|webp] [--size N]\n\nSemantic vector search over the public explore gallery.\n\nExamples:\n  mj search \"neon cyberpunk city\" --limit 10 --download ./found\n  mj search \"...\" --download ./found --format webp --size 384",
	"explore":      "mj explore [--feed top|top_week|top_month|hot|random|videos] [--page N] [--limit N] [--download DIR] [--format png|webp] [--size N]\n\nBrowse the public explore gallery.",
	"likes":        "mj likes [--page N] [--download DIR]\n\nList images the account has liked (page is 1-based).",
	"styles":       "mj styles [--page N] [--limit N]\n\nBrowse community style references (each carries an --sref code).",
	"profile":      "mj profile USERNAME [--limit N] [--download DIR]\n\nList a user's published spotlight gallery (by username_v2).",
	"uploads":      "mj uploads [--download DIR]\n\nList the account's uploaded assets (personal storage).",
	"liked-styles": "mj liked-styles\n\nList the style references the account has liked (raw JSON).",
	"folders":      "mj folders\n\nList your folders/collections (raw JSON).",
	"profiles":     "mj profiles\n\nList your personalization profiles (raw JSON). The ids are usable as\n`mj imagine \"...\" --profile <id>`. For a creator's gallery, use `mj profile USERNAME`.",
	"moodboards":   "mj moodboards\n\nList the account's personalization moodboards.",
	"queue":        "mj queue [--json]\n\nList the account's running and waiting jobs.",
	"api":          "mj api METHOD PATH [--data JSON]\n\nRaw call to any midjourney.com/api endpoint.\n\nExamples:\n  mj api GET /api/folders\n  mj api POST /api/user-flags --data '{\"flag\":\"x\"}'",

	"status":   "mj status JOB_ID... [--json]\n\nShow current status for one or more jobs.",
	"wait":     "mj wait JOB_ID [--timeout 5m] [--json]\n\nBlock until the job reaches a terminal state.",
	"watch":    "mj watch JOB_ID [--download DIR]\n\nStream live progress % over WebSocket for an in-flight job.",
	"history":  "mj history [--page-size N] [--cursor C] [--json]\n\nList the account's recent jobs.",
	"download": "mj download JOB_ID [--dir .] [--which grid|cells|all] [--format png|webp] [--size N]\n\nDownload a completed job's assets. webp is ~5x smaller; --size sets a thumbnail edge.",
	"login":    "mj login [--i-understand]\n\nOne-time interactive (headful) login. Saves a local session.",
	"logout":   "mj logout\n\nRemove the local session and any stored refresh token.",
	"account":  "mj account [--json]\n\nShow plan and relax/stealth eligibility.",
	"auth":     "mj auth status\n\nShow whether a session is established and the current user/plan.",
	"doctor":   "mj doctor\n\nCheck the toolchain: gomoufox runtime, hasp, credentials, ToS ack.",
	"serve":    "mj serve [--idle 30m]\n\nRun a warm daemon so CLI/MCP commands share one browser + session.",
	"daemon":   "mj daemon status|stop\n\nManage the warm daemon.",
}

// printCommandHelp prints focused help for cmd and reports whether it was known.
func printCommandHelp(cmd string) bool {
	cmd = strings.TrimLeft(cmd, "-")
	h, ok := commandHelp[cmd]
	if !ok {
		fmt.Fprintf(os.Stderr, "mj: no help for %q\n\n", cmd)
		fmt.Fprintln(os.Stderr, "known topics:", strings.Join(helpTopics(), ", "))
		return false
	}
	fmt.Println(h)
	return true
}

func helpTopics() []string {
	out := make([]string, 0, len(commandHelp))
	for k := range commandHelp {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

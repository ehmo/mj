# mj CLI reference

Every command takes `--json` for machine output. Data goes to stdout. Progress,
warnings, and the next-page cursor go to stderr. Pipes stay clean.

Run `mj help <command>` for focused help on any command.

## Exit codes

| Code | Meaning |
|---|---|
| 0 | Success |
| 1 | Generic error |
| 2 | Usage error or unknown command |
| 3 | Auth failure (no session, bad token) |
| 4 | Job failed or was moderated |
| 5 | Timed out waiting for a job |
| 6 | Cloudflare block (HTTP 403) |

Scripts can branch on these. For example, exit 4 means stop retrying; exit 6
means back off.

## Account

```bash
mj login --i-understand      # one-time interactive login (opens a browser)
mj logout                    # remove the local session and stored token
mj account [--json]          # plan, relax/stealth eligibility, default speed
mj auth status               # is a session established? who?
mj doctor                    # check gomoufox, hasp, credentials, ToS ack
```

`mj login` opens a real browser. Sign in with Google or Discord, wait for the
Create page, press Enter. Run it once. The session persists in a local profile.

## Generate

```bash
mj imagine "PROMPT" [flags]
mj vary    JOB_ID --index N [--strong] [--wait] [--download DIR]
mj upscale JOB_ID --index N [--kind creative|subtle] [--wait] [--download DIR]
mj reroll  JOB_ID [--wait]
mj video   JOB_ID --index N [--motion high|low] [--wait] [--download DIR]
```

`index` is the grid cell, 1 to 4.

`imagine` flags:

```text
--ar W:H              aspect ratio, e.g. 3:2
--version V           model: 7, 8, 8.1, niji 6, niji 7
--mode M              fast | relax | turbo   (relax and turbo need an eligible plan)
--stealth             private generation (Pro/Mega)
--stylize N           0..1000
--chaos N             0..100
--weird N             0..3000
--quality Q           0.25 | 0.5 | 1 | 2 | 4
--seed N              0..4294967295
--no a,b              negative terms, comma separated
--tile                repeating tile pattern
--stop N              10..100
--style-raw           minimal styling
--raw "..."           verbatim trailing flags, appended last
--sref U --sw N --sv N    style reference (repeatable), weight, version
--oref U --ow N           omni reference (repeatable), weight
--cref U --cw N           character reference (repeatable), weight
--profile ID          personalization profile (see `mj profiles`)
--image FILE|URL      image prompt (repeatable; local files upload first)
--wait                block until the job finishes
--watch               stream live 0..100% over the WebSocket (runs locally)
--download DIR        save the result to DIR
--json                machine output
```

`mj` validates ranges before it submits, so a bad `--stylize 9000` fails fast
instead of wasting a job. It also checks your plan before relax, turbo, or stealth.

Examples:

```bash
mj imagine "a teal bicycle on a pink wall --ar 3:2" --wait --download ./out
mj imagine "a koi in dark water" --watch --download ./out
mj imagine "logo, minimal" --version 7 --stylize 250 --json
mj imagine "in this style, a fox" --sref 12345 --image ref.png
mj vary    abc123 --index 2 --strong --wait
mj upscale abc123 --index 2 --kind creative --download ./out
mj video   abc123 --index 1 --motion low --wait --download ./clips
```

## Edit

```bash
mj describe    IMAGE [--json]
mj retexture   IMAGE "PROMPT" [--ar W:H] [--wait] [--download DIR]
mj zoom        IMAGE "PROMPT" [--factor 2] [--wait] [--download DIR]
mj pan         IMAGE "PROMPT" --dir left|right|up|down [--amount 0.5] [--wait]
mj outpaint    IMAGE "PROMPT" [--left N] [--top N] [--right N] [--bottom N] [--wait]
mj vary-region IMAGE "PROMPT" --region X,Y,W,H [--wait] [--download DIR]
```

`IMAGE` is a local file or a URL. `mj` composites the edit the way the web editor
does (it extends the canvas for pan and zoom, erases a hole for vary-region), then
submits the upload. `describe` returns about four suggested prompts.

```bash
mj describe    photo.png
mj zoom        photo.png "a wider shot" --factor 2 --wait --download ./out
mj pan         photo.png "more sky" --dir up --amount 0.5 --wait
mj vary-region photo.png "a golden crown" --region 300,40,420,300 --wait
mj retexture   photo.png "oil painting, impasto" --ar 1:1 --wait --download ./out
```

## Discover

```bash
mj search  "QUERY" [--page N] [--limit N] [--download DIR] [--format png|webp] [--size N]
mj explore [--feed FEED] [--page N] [--limit N] [--download DIR] [--format png|webp] [--size N]
mj likes   [--page N] [--download DIR]
mj styles  [--page N] [--limit N]
mj profile USERNAME [--limit N] [--download DIR]
mj uploads [--download DIR]
mj liked-styles
mj folders
mj profiles
mj queue [--json]
mj moodboards
```

`--feed` is one of top, top_week, top_month, hot, random, videos.

`--format webp` downloads webp instead of png. Same resolution, about five times
smaller. `--size N` downloads an N-pixel thumbnail (for example 384 or 640).

`search` runs Midjourney's semantic vector search. Each result carries the full
reproducible command (prompt plus flags), the media type, whether you liked it,
and parent lineage. Use `--json` to get all of it.

`styles` lists community style references, each with an `--sref` code. `profiles`
lists your own personalization profiles, whose ids work with `--profile`. `folders`
lists your collections.

```bash
mj search "neon cyberpunk city" --limit 10 --download ./found
mj search "..." --download ./found --format webp --size 384
mj explore --feed top_week --limit 20
mj profile someUser --limit 50 --download ./out
mj styles                       # copy an --sref code into `mj imagine`
```

## Jobs

```bash
mj status   JOB_ID... [--json]
mj wait     JOB_ID [--timeout 5m] [--json]
mj watch    JOB_ID [--download DIR]
mj history  [--page-size N] [--cursor C] [--json]
mj download JOB_ID [--dir .] [--which grid|cells|all] [--format png|webp] [--size N]
```

`watch` streams a live percentage over the WebSocket for a job already in flight.
`download` works on any job id, including community results from `search` or
`explore`. It pulls the real cell count from job status, so grids give four cells
plus the grid, upscales give one, and videos give the mp4.

```bash
mj status   abc123 def456
mj watch    abc123 --download ./out
mj download abc123 --dir ./out --which all --format webp
mj history  --page-size 20 --json | jq -r '.jobs[].id'
```

## Daemon

```bash
mj serve [--idle 30m]        # run a warm daemon (foreground; background it yourself)
mj daemon status            # is it running, and where is the socket?
mj daemon stop              # ask it to shut down
```

A warm daemon keeps one browser and one session alive. Commands route to it
automatically and fall back to a one-off browser when it is not running. `--idle`
shuts it down after that much inactivity.

## Raw

```bash
mj api METHOD PATH [--data JSON]
```

A direct call to any `midjourney.com/api/*` endpoint. Useful for capabilities that
do not have a dedicated command yet.

```bash
mj api GET  /api/folders
mj api POST /api/user-flags --data '{"flag":"x"}'
```

Submits through `/api/submit-jobs` still pass the throttle, even here.

## Global behavior

- `--json` on any command gives structured output on stdout.
- `--throttle <dur>` (or `throttle_secs` in config) sets the minimum gap between
  submits. Default 12s.
- `MJ_DEBUG=1` logs every API call to stderr (method, path, status, timing),
  tokens redacted.
- Config lives at `<config dir>/mj/config.json`. It holds no secrets.

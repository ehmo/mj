# mj

<p align="center">
  <img alt="Go 1.26" src="https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white">
  <img alt="MIT license" src="https://img.shields.io/badge/license-MIT-2ea44f">
  <img alt="MCP ready" src="https://img.shields.io/badge/MCP-25%20tools-7c3aed">
  <img alt="platforms" src="https://img.shields.io/badge/macOS%20%7C%20Linux-amd64%20%7C%20arm64-555">
  <img alt="status" src="https://img.shields.io/badge/status-unofficial-orange">
</p>

`mj` drives Midjourney's web app from Go. It is a command-line tool and an MCP
server for AI agents. It uses your own Midjourney account. There is no Discord
bot and no account pool.

Under the hood it drives a real [Camoufox](https://camoufox.com) browser through
[gomoufox](https://github.com/ehmo/gomoufox), so every request to
`midjourney.com` and the result CDN goes out the way a logged-in browser does.
That is the only path that clears Cloudflare.

> [!WARNING]
> Midjourney's Terms of Service prohibit automated access: "You may not use
> automated tools to access, interact with, or generate Assets through the
> Services." Accounts have been banned for it. `mj` is bring-your-own-account
> only. Your account, your risk. It ships a conservative submit throttle, backs
> off when the server rate-limits you, and refuses to generate until you pass
> `--i-understand` once. Run it at low volume.

## At a glance

| You want to | Use |
|---|---|
| Generate images from a terminal or script | `mj imagine "..."` |
| Give an AI agent image tools | `mj-mcp` (25 tools) |
| Search and pull from the community gallery | `mj search`, `mj explore` |
| Edit an image (zoom out, pan, inpaint, restyle) | `mj zoom`, `mj pan`, `mj vary-region`, `mj retexture` |
| Watch a job render live | `mj imagine "..." --watch` |
| Share one warm browser across many calls | `mj serve` |

## Install

You need Go 1.26 or newer. On first run, gomoufox downloads a managed Camoufox
browser (a Python venv plus a Firefox binary, about 300 to 660 MB). That happens
once.

Install with Homebrew:

```bash
brew install ehmo/mj/mj
```

Or install the two binaries with Go:

```bash
go install github.com/ehmo/mj/cmd/mj@latest
go install github.com/ehmo/mj/cmd/mj-mcp@latest
```

Or build from a clone:

```bash
git clone https://github.com/ehmo/mj && cd mj
go build -o mj ./cmd/mj
go build -o mj-mcp ./cmd/mj-mcp
```

Check the toolchain:

```bash
mj doctor
```

## Log in once

```bash
mj login --i-understand
```

A browser window opens. Sign in with Google or Discord, wait for the Create page
to load, then press Enter in the terminal. `mj` saves the session to a local
profile. If you have [hasp](https://github.com/ehmo/hasp), it also stores the
Firebase refresh token in your vault. The token never lands in `mj`'s own config.

## First image

```bash
mj imagine "a vintage teal bicycle on a pink wall --ar 3:2" --wait --download ./out
```

This submits one job (a 4-image grid), waits for it to finish, and saves the
four cells plus the grid to `./out`. Add `--json` for machine output.

Want to watch it render? Swap `--wait` for `--watch`:

```bash
mj imagine "a koi in dark water" --watch --download ./out
```

You get a live percentage from 0 to 100 over Midjourney's WebSocket.

## When to use what

Use the **CLI** when you drive Midjourney yourself, from a shell, a script, or a
cron job. Pipe `--json` into `jq` and you have a generation pipeline.

Use the **MCP server** when an agent should drive Midjourney. Claude (or any MCP
client) gets 25 tools: generate, vary, upscale, animate, search, edit, download,
and a raw escape hatch.

Use the **daemon** when you make many calls. Each one-off command cold-starts a
browser, which costs about 4 to 6 seconds. A warm daemon shares one browser and
one session, so calls drop to under a second.

## Use cases

### 1. Generate from a script

```bash
for p in "red maple leaf" "blue spruce cone" "golden ginkgo"; do
  mj imagine "$p, white background, studio light --ar 1:1" --wait --download ./botany --json \
    | jq -r '.images[]'
done
```

Each line of output is a CDN URL. The files land in `./botany`.

### 2. Let an agent make images

Register the server with Claude Code:

```bash
claude mcp add midjourney -- mj-mcp
```

Now ask the agent in plain language: "Generate three logo options for a coffee
brand, then upscale the one with the cleanest type." The agent calls
`midjourney_imagine`, reads the result, and calls `midjourney_upscale`. See
[docs/mcp.md](docs/mcp.md) for the full tool list and a worked transcript.

### 3. Mine the community gallery

```bash
mj search "brutalist concrete interior, soft light" --limit 12 --download ./refs
mj explore --feed top_week --limit 20
mj styles                      # browse --sref style codes
```

Search runs Midjourney's semantic vector search. Every result carries the full
prompt and flags that made it, so you can reproduce or remix it.

### 4. Edit an existing image

```bash
mj zoom        photo.png "a wider establishing shot" --factor 2 --wait --download ./out
mj pan         photo.png "more sky"  --dir up --amount 0.5 --wait --download ./out
mj vary-region photo.png "a golden crown" --region 300,40,420,300 --wait --download ./out
mj retexture   photo.png "oil painting, thick impasto" --ar 1:1 --wait --download ./out
```

`mj` composites the edit the same way Midjourney's web editor does, then submits
it. The source can be a local file or a URL.

### 5. Animate a still

```bash
JOB=$(mj imagine "a paper boat on a calm pond --ar 16:9" --wait --json | jq -r .job_id)
mj video "$JOB" --index 1 --motion low --wait --download ./clips
```

Videos download in bounded-memory chunks, so a long clip does not balloon memory.

## CLI

Every command takes `--json`. Data goes to stdout, progress and warnings go to
stderr, so pipes stay clean. Exit codes separate auth failures (3), job failures
(4), timeouts (5), and Cloudflare blocks (6) from generic errors (1).

```text
Generate   imagine  vary  upscale  reroll  video
Edit       describe  retexture  zoom  pan  outpaint  vary-region
Discover   search  explore  likes  styles  profile  uploads  liked-styles
           folders  profiles  queue  moodboards
Jobs       status  wait  watch  history  download
Account    login  logout  account  auth  doctor
Daemon     serve  daemon
Raw        api
```

Run `mj help <command>` for focused help on any of them. Full reference:
[docs/cli.md](docs/cli.md).

Generation flags map straight to Midjourney parameters:

```text
--ar W:H   --version 7|8|8.1|niji 6|niji 7   --mode fast|relax|turbo   --stealth
--stylize 0..1000   --chaos 0..100   --weird 0..3000   --quality 0.25|0.5|1|2|4
--seed N   --no a,b   --tile   --stop 10..100   --style-raw   --raw "..."
--sref U --sw N --sv N    --oref U --ow N    --cref U --cw N    --profile ID
--image FILE|URL (repeatable; local files upload first)
```

`--mode relax|turbo` and `--stealth` need an eligible plan. `mj` checks your plan
before it submits, so you get a clear error instead of a wasted job.

## MCP

`mj-mcp` speaks MCP over stdio, so it works with any MCP client.

Claude Code:

```bash
claude mcp add midjourney -- mj-mcp
```

Codex, in `~/.codex/config.toml`:

```toml
[mcp_servers.midjourney]
command = "mj-mcp"
```

Cursor, Windsurf, or any other stdio client, in its MCP config:

```json
{ "mcpServers": { "midjourney": { "command": "mj-mcp" } } }
```

It exposes 25 tools:

```text
imagine  variation  upscale  reroll  video        (generate)
describe retexture  zoom  pan  vary_region         (edit)
search   explore  likes  styles  profile  uploads  (discover)
folders  profiles  queue  moodboards               (account)
status   history  account  download  api           (jobs / raw)
```

It runs one generation at a time. It reads status, runs searches, and cancels
work while a generation is in flight, so the agent is never blocked. It refuses
to start until you have run `mj login --i-understand`.

### Agent safety

An agent's input can be hostile (prompt injection from a web page or an image).
The MCP surface is locked down for that:

- `image` arguments must be public `https` URLs. Local paths are refused unless
  you set `MJ_MCP_FILE_ROOT=/some/dir`, and then only files under that directory.
  This stops "describe this image: /home/you/.ssh/id_rsa" from uploading your
  files to a third party.
- URLs that point at private, loopback, or cloud-metadata hosts are blocked.
- `midjourney_api` (the raw endpoint) is read-only unless you set
  `MJ_MCP_ALLOW_RAW_WRITE=1`.

Full tool reference, schemas, and an example agent session: [docs/mcp.md](docs/mcp.md).

## Daemon

```bash
mj serve                  # warm browser; keep it running (or background it)
mj daemon status          # running (…/mj/daemon.sock)
mj search "cat"           # ~0.3s instead of ~5s
mj daemon stop
```

Commands route to the daemon when it runs and fall back to a one-off browser when
it does not. The daemon throttles generations across every client, CLI and MCP
together, through one session. That is faster and safer for your account than two
clients submitting in parallel.

Set `daemon_autostart: true` in config (or `MJ_DAEMON_AUTOSTART=1`) and the first
command spawns a background daemon for you.

## Ban safety

Automating Midjourney risks your account. `mj` does what it can to keep the risk
low:

- A minimum gap between submits (12s by default), enforced at one choke point so
  even the raw `api` tool cannot skip it.
- Adaptive backoff: a `429` grows the gap and a success shrinks it again.
- One submit funnel through the daemon, shared by every client.
- The ToS gate (`--i-understand`), stored once.

None of this makes automation safe. It makes it slower, which is the point. Set a
larger gap with `--throttle` or the `throttle_secs` config if you want more
headroom. Watch traffic with `MJ_DEBUG=1`, which logs every API call (method,
path, status, timing) with tokens redacted.

## Credentials

`mj` reads the Firebase refresh token from, in order:

1. `MIDJOURNEY_FIREBASE_REFRESH_TOKEN` (use `hasp inject … -- mj …` to broker it).
2. The local browser profile after `mj login`.

It never writes the token to its own config. `mj logout` removes the local
session and deletes the stored token.

## Documentation

- [docs/cli.md](docs/cli.md): every command, flag, and example.
- [docs/mcp.md](docs/mcp.md): every MCP tool, its schema, and an agent transcript.
- [SECURITY.md](SECURITY.md): trust boundaries and how to report an issue.
- [CONTRIBUTING.md](CONTRIBUTING.md): build, test, and the bar for changes.

## License

MIT, copyright Wraxle LLC. See [LICENSE](LICENSE).

Camoufox is MPL-2.0 and runs as a subprocess, downloaded at runtime, not vendored.
gomoufox and playwright-go are MIT.

This is an unofficial, independent project. It is not affiliated with Midjourney,
and Midjourney has not endorsed it.

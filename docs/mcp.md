# mj-mcp: Midjourney tools for AI agents

`mj-mcp` is a Model Context Protocol server. It gives an AI agent (Claude or any
MCP client) 25 tools to generate, edit, search, and download Midjourney images
through your own account. It speaks MCP over stdio.

This page covers setup, every tool, the safety model, the environment knobs, and
a full agent transcript.

## Setup

Build the binary, then register it with your client.

```bash
go install github.com/ehmo/mj/cmd/mj-mcp@latest
```

You must log in once before the server will start:

```bash
mj login --i-understand
```

Claude Code:

```bash
claude mcp add midjourney -- mj-mcp
```

Codex, in `~/.codex/config.toml`:

```toml
[mcp_servers.midjourney]
command = "mj-mcp"
# env = { MJ_MCP_FILE_ROOT = "/path/to/images" }   # optional, see Agent safety
```

Cursor, Windsurf, or any other stdio MCP client, in its config:

```json
{
  "mcpServers": {
    "midjourney": { "command": "mj-mcp" }
  }
}
```

`mj-mcp` reads JSON-RPC on stdin and writes responses on stdout. Logs and errors
go to stderr. It needs no arguments. It finds your session the same way the CLI
does (the `MIDJOURNEY_FIREBASE_REFRESH_TOKEN` env var, or the profile from
`mj login`).

The server refuses to start until `mj login --i-understand` has run once. That is
the Terms-of-Service gate. It is deliberate.

## How it behaves

One generation runs at a time. A single-slot gate holds new generations while one
is in flight, and returns a clear "another generation is in progress" so the agent
can retry.

Reads never block. The server handles each request in its own goroutine, so the
agent can call `midjourney_status`, `midjourney_search`, or cancel work while a
60-second generation runs. An old single-threaded server would freeze; this one
does not.

Cancellation works. If the client sends `notifications/cancelled`, the matching
in-flight call stops.

Failures are honest. A moderated or failed job returns a terminal error that tells
the agent to stop polling. A download error rides back in the result rather than
being swallowed, so the agent knows the files did not save.

## The 25 tools

### Generate

| Tool | Required | Optional |
|---|---|---|
| `midjourney_imagine` | `prompt` | `ar`, `version`, `mode`, `stealth`, `stylize`, `chaos`, `seed`, `no`, `raw`, `wait`, `download_dir` |
| `midjourney_variation` | `job_id`, `index` | `strong`, `wait`, `download_dir` |
| `midjourney_upscale` | `job_id`, `index` | `kind` (creative\|subtle), `wait`, `download_dir` |
| `midjourney_reroll` | `job_id` | `wait` |
| `midjourney_video` | `job_id`, `index` | `motion` (high\|low), `wait`, `download_dir` |

`index` is the grid cell, 1 to 4. `wait` (default true) blocks until the job
finishes. `download_dir` saves the result if the job succeeds.

### Edit

| Tool | Required | Optional |
|---|---|---|
| `midjourney_describe` | `image` | |
| `midjourney_retexture` | `image`, `prompt` | `ar`, `wait`, `download_dir` |
| `midjourney_zoom` | `image`, `prompt` | `factor` (default 2), `wait`, `download_dir` |
| `midjourney_pan` | `image`, `prompt`, `dir` (left\|right\|up\|down) | `amount` (default 0.5), `wait`, `download_dir` |
| `midjourney_vary_region` | `image`, `prompt`, `x`, `y`, `w`, `h` | `wait`, `download_dir` |

`image` is a public `https` URL by default. See Agent safety below for local files.

### Discover

| Tool | Required | Optional |
|---|---|---|
| `midjourney_search` | `query` | `page`, `limit` |
| `midjourney_explore` | | `feed` (top\|top_week\|top_month\|hot\|random\|videos), `page`, `limit` |
| `midjourney_likes` | | `page`, `limit` |
| `midjourney_styles` | | `page`, `limit` |
| `midjourney_profile` | `username` | `limit` |
| `midjourney_uploads` | | |

`midjourney_styles` returns `sref` codes. Pass one as the `raw` argument to
`midjourney_imagine` (for example `raw: "--sref 12345"`) to reuse that style.

### Account and jobs

| Tool | Required | Optional |
|---|---|---|
| `midjourney_account` | | |
| `midjourney_queue` | | |
| `midjourney_moodboards` | | |
| `midjourney_folders` | | |
| `midjourney_profiles` | | |
| `midjourney_status` | `job_ids` (array) | |
| `midjourney_history` | | `page_size`, `cursor` |
| `midjourney_download` | `job_id`, `dir` | `which` (grid\|cells\|all) |

`midjourney_profiles` lists your personalization profiles. The ids work as the
`profile` argument to `midjourney_imagine`.

### Raw

| Tool | Required | Optional |
|---|---|---|
| `midjourney_api` | `path` | `method` (default GET), `body` |

The escape hatch for any `midjourney.com/api/*` endpoint without a dedicated tool.
Read-only by default. See Agent safety.

## Agent safety

Treat agent input as hostile. A prompt injected through a web page or an image can
try to make the agent leak files or hit internal services. The server defends the
boundary:

- `image` arguments must be public `https` URLs. Local file paths are refused
  unless you set `MJ_MCP_FILE_ROOT`, and then only for files under that directory.
  Without this, an agent told "describe /home/you/.aws/credentials" would upload
  that file to Midjourney.
- URLs that resolve to private, loopback, or cloud-metadata hosts are blocked.
- `midjourney_api` is read-only. Set `MJ_MCP_ALLOW_RAW_WRITE=1` to allow non-GET
  calls. A write through this tool would otherwise hand the agent full control of
  your account.
- Every submit, including one made through `midjourney_api`, passes the same
  throttle. An agent cannot rush submits past the ban-safety gap.

## Environment

| Variable | Effect |
|---|---|
| `MIDJOURNEY_FIREBASE_REFRESH_TOKEN` | Refresh token, usually injected by hasp. |
| `MJ_MCP_FILE_ROOT` | Directory under which local `image` paths are allowed. |
| `MJ_MCP_ALLOW_RAW_WRITE=1` | Allow non-GET `midjourney_api` calls. |
| `MJ_DAEMON_AUTOSTART=1` | Spawn a warm daemon on first call. |
| `MJ_DEBUG=1` | Log every API call to stderr, tokens redacted. |

When `mj serve` runs, `mj-mcp` shares that warm browser and session with your CLI.
One stealth session serves both you and the agent, and tool calls drop to under a
second.

## Result shapes

A generation returns:

```json
{
  "job_id": "ca287797-…",
  "status": "completed",
  "images": ["https://cdn.midjourney.com/ca287797-…/0_0.png", "…0_1.png", "…"],
  "grid_url": "https://cdn.midjourney.com/ca287797-…/grid_0.png",
  "files": ["./out/ca287797-…_0.png", "…"]
}
```

`files` appears only when you pass `download_dir` and the job succeeds. A failed
download adds `download_error` instead.

A read tool (`search`, `explore`, `account`, and so on) returns the parsed JSON as
text content.

## A worked session

The agent is asked: "Make three coffee-brand logos, then upscale the cleanest one."

```jsonc
// 1. The agent generates a grid.
→ tools/call midjourney_imagine
  { "prompt": "minimal coffee brand logo, single bean mark, flat",
    "ar": "1:1", "stylize": 200, "wait": true }
← { "job_id": "abc123", "status": "completed",
    "images": ["…/abc123/0_0.png", "…0_1.png", "…0_2.png", "…0_3.png"] }

// 2. The agent looks at the four cells, picks cell 2, and upscales it.
→ tools/call midjourney_upscale
  { "job_id": "abc123", "index": 2, "kind": "creative",
    "wait": true, "download_dir": "./logos" }
← { "job_id": "def456", "status": "completed",
    "images": ["…/def456/0_0.png"],
    "files": ["./logos/def456_0.png"] }
```

The agent reads image URLs, decides, and acts. It never sees your token, and it
cannot submit faster than the throttle allows.

## Troubleshooting

`ToS not acknowledged`: run `mj login --i-understand` once.

`another generation is in progress`: one generation runs at a time. Retry, or
wait for the current one.

`local file paths are disabled`: pass an `https` URL, or set `MJ_MCP_FILE_ROOT`.

`raw API writes are disabled`: set `MJ_MCP_ALLOW_RAW_WRITE=1` if you mean it.

Job stuck at `pending` past the wait window: the generation is slow, not failed.
Poll with `midjourney_status` using the `job_id`.

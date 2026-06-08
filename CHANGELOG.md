# Changelog

All notable changes to `mj`. The format follows [Keep a Changelog](https://keepachangelog.com),
and the project uses [semantic versioning](https://semver.org).

## v0.11.1

### Fixed
- Reissued the public release after the one-commit history reset, with
  `go install` binaries reporting the module tag version instead of `dev`.

## v0.11.0

### Added
- `midjourney_video` MCP tool. Agents can now animate a grid cell, matching the
  CLI's `mj video`. The MCP server exposes 25 tools.
- Public documentation: a rewritten README, `docs/cli.md`, `docs/mcp.md`,
  `SECURITY.md`, and `CONTRIBUTING.md`.

## v0.10.0

### Added
- Adaptive rate-limit backoff. An HTTP 429 on submit grows the gap (capped at
  5 minutes) and a success shrinks it, in both the client and the daemon funnel.
- `MJ_DEBUG=1` logs every API call (method, path, status, timing) to stderr with
  tokens redacted.
- `mj folders` and `mj profiles` commands, plus the matching MCP tools.

### Removed
- The unused `BlendDim` type. Blend is done through image prompts.

## v0.9.0

### Security
- The MCP server source is under version control. A `.gitignore` pattern had
  excluded the whole `cmd/mj-mcp/` tree.
- The submit throttle is enforced at one choke point, so the raw `api` paths can
  no longer skip the ban-safety gap.
- The MCP image arguments require public URLs (local paths gated by
  `MJ_MCP_FILE_ROOT`) with an SSRF guard. `midjourney_api` is read-only by default.
- The CBOR WebSocket decoder is overflow-safe and no longer panics on malformed
  frames.

### Changed
- The MCP server handles requests concurrently, so reads and cancellations run
  while a generation is in flight.

## v0.8.0

- Hardening pass across search, creation through MCP, login, download, and
  caching.

## v0.7.0

- Richer search and explore results. New browse feeds: likes, profile spotlight,
  uploads, liked styles. webp and thumbnail download variants.

## v0.6.0

- Canvas editor: pan, zoom out, and vary-region (inpaint).

## v0.5.0

- Live WebSocket progress for `mj imagine --watch` and `mj watch`.

## v0.4.0

- Editor: retexture.

## v0.2.0

- Daemon mode. One warm browser and session shared over a local socket by the CLI
  and MCP server. Cross-client submit throttle.

## v0.1.0

- First cut. CLI and MCP server, Firebase auth through gomoufox, generation
  (imagine, vary, upscale, reroll, video), status and wait, CDN download.

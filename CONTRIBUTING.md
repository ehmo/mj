# Contributing

Thanks for looking. `mj` is a small, focused codebase. Here is how to build it,
test it, and the bar a change needs to clear.

## Build and test

You need Go 1.26 or newer.

```bash
go build ./...
go vet ./...
gofmt -l .            # must print nothing
go test ./...         # offline tests, no browser, no account
go test -race ./...   # the concurrency paths are real; keep them clean
```

CI runs gofmt, vet, the offline tests, and the build on every push and pull
request. All four must pass.

## Live tests

Tests that touch a real account or browser are gated behind environment
variables and skip by default:

```bash
MJ_LIVE=1 MJ_LIVE_GEN=1 go test ./internal/mjclient/ -run TestLiveGen
```

These submit real jobs and cost credits. Do not add a live test to the default
path. Keep parsers, coercion, and transport logic in offline tests with fixtures.

## The bar

- Match the surrounding style. Read the file before you change it.
- A change to a function signature or behavior means checking the callers.
- New parsing of untrusted input (API JSON, CBOR frames, agent arguments) needs a
  test for malformed input that proves it does not panic.
- Ban safety is a feature. Do not add a path that submits without passing the
  throttle. The single choke point is `call()` in `internal/mjclient`.
- The MCP surface treats agent input as hostile. A new tool that takes a file path
  or a URL goes through the same confinement and SSRF checks as the others.
- Secrets never land in config, logs, or error strings. Redaction is not optional.

## Layout

```text
cmd/mj         CLI entry point and command handlers
cmd/mj-mcp     MCP server (stdio JSON-RPC)
internal/mjapi      data model, parsers, parameter serialization (no network)
internal/mjclient   browser transport, auth, generation, download, daemon
```

`internal/mjapi` has no network dependency, so its parsers are fully testable
offline. Keep it that way.

## Commits

Write a clear subject line that says what changed and why. Group related changes.
Keep unrelated changes in separate commits.

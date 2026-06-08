# Security

`mj` automates a Midjourney account and drives a real browser. This page covers
how it handles your credentials, where the trust boundaries sit, and how to report
a problem.

## Reporting a vulnerability

Open a private security advisory on the GitHub repository, or email the
maintainer. Please do not file a public issue for a vulnerability. Give it a few
days before public disclosure.

## Credentials

The Firebase refresh token is the keys to your account. `mj` treats it that way.

- It never writes the token to its own config file.
- It reads the token from, in order: the `MIDJOURNEY_FIREBASE_REFRESH_TOKEN`
  environment variable (usually injected by [hasp](https://github.com/ehmo/hasp)),
  then the local browser profile after `mj login`.
- `mj logout` removes the local session and deletes the stored token.
- Error messages and debug logs redact anything token-shaped before printing, so
  `MJ_DEBUG=1` output is safe to paste.

The Firebase web API key in the source is public by design. Firebase web keys
identify the project; they are not secrets. Account security comes from the
refresh token, never from that key.

## Trust boundaries

There are three places where untrusted input meets `mj`.

**Your shell, the CLI.** You run the commands. The CLI trusts you. Local file
paths, raw API calls, and download directories are all yours to choose.

**An agent, the MCP server.** An agent's input can be hostile. A prompt injected
through a web page or an image can try to make the agent leak files or reach
internal services. The MCP server defends this boundary:

- `image` arguments must be public `https` URLs. Local paths are refused unless
  you set `MJ_MCP_FILE_ROOT`, and then only files under that directory.
- URLs that resolve to private, loopback, or cloud-metadata hosts are blocked.
- `midjourney_api` is read-only unless you set `MJ_MCP_ALLOW_RAW_WRITE=1`.
- Every submit, including a raw one, passes the throttle.

**Midjourney and the CDN, the network.** All traffic runs inside the Camoufox
browser over TLS. `mj` parses JSON and CBOR frames from those responses. The
parsers are written to never panic on malformed input, so a corrupt or truncated
frame cannot crash a running watch.

## Downloads

Asset filenames come from Midjourney job ids. `mj` sanitizes any path separator
out of an id before it builds a filename, so a download cannot escape the target
directory.

## What `mj` does not protect against

It cannot make automating Midjourney safe. Their Terms of Service prohibit it, and
accounts have been banned. The throttle and backoff lower the risk; they do not
remove it. Run your own account, at low volume, knowing the stakes.

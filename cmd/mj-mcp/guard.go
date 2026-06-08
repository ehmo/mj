package main

import (
	"fmt"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// resolveImage validates an agent-supplied image reference before it reaches the
// browser/upload path. The MCP boundary is prompt-injectable, so:
//   - https/http URLs are allowed but SSRF-checked (no private/loopback hosts);
//   - local file paths are REFUSED unless the operator opted in via
//     MJ_MCP_FILE_ROOT, and then only for files under that root (no `..`/symlink
//     escape). This stops "describe /Users/you/.ssh/id_rsa"-style exfiltration.
func (s *server) resolveImage(args map[string]any, key string) (string, error) {
	raw := strings.TrimSpace(getStr(args, key))
	if raw == "" {
		return "", fmt.Errorf("%s required", key)
	}
	if isHTTPURL(raw) {
		if err := validatePublicURL(raw); err != nil {
			return "", err
		}
		return raw, nil
	}
	return validateLocalPath(raw)
}

func isHTTPURL(s string) bool {
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// validatePublicURL rejects non-http(s) schemes and URLs pointing at private,
// loopback, or link-local hosts (cloud metadata, localhost services). It does
// not defend against DNS rebinding — a hostname that resolves to a private IP at
// fetch time can still slip through; literal internal targets are blocked.
func validatePublicURL(raw string) error {
	u, err := url.Parse(raw)
	if err != nil {
		return fmt.Errorf("invalid url: %w", err)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return fmt.Errorf("url scheme %q not allowed (use https)", u.Scheme)
	}
	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("url has no host")
	}
	lower := strings.ToLower(host)
	if lower == "localhost" || strings.HasSuffix(lower, ".localhost") ||
		strings.HasSuffix(lower, ".local") || lower == "metadata.google.internal" {
		return fmt.Errorf("url host %q is not allowed", host)
	}
	if ip := net.ParseIP(host); ip != nil && !isPublicIP(ip) {
		return fmt.Errorf("url host %s resolves to a non-public address", host)
	}
	// Best-effort: if the literal host resolves to only private addresses, block.
	if net.ParseIP(host) == nil {
		if ips, lerr := net.LookupIP(host); lerr == nil && len(ips) > 0 {
			allPrivate := true
			for _, ip := range ips {
				if isPublicIP(ip) {
					allPrivate = false
					break
				}
			}
			if allPrivate {
				return fmt.Errorf("url host %q resolves only to non-public addresses", host)
			}
		}
	}
	return nil
}

func isPublicIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() {
		return false
	}
	return true
}

// validateLocalPath returns the resolved path only if local files are enabled
// (MJ_MCP_FILE_ROOT) and the target lives under that root.
func validateLocalPath(p string) (string, error) {
	root := strings.TrimSpace(os.Getenv("MJ_MCP_FILE_ROOT"))
	if root == "" {
		return "", fmt.Errorf("local file paths are disabled in the MCP server; pass an https URL " +
			"(or set MJ_MCP_FILE_ROOT to a directory to allow local files)")
	}
	rootResolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("MJ_MCP_FILE_ROOT %q is invalid: %w", root, err)
	}
	resolved, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", fmt.Errorf("image path %q: %w", p, err)
	}
	rel, err := filepath.Rel(rootResolved, resolved)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("image path %q is outside MJ_MCP_FILE_ROOT", p)
	}
	return resolved, nil
}

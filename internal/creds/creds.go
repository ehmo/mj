// Package creds resolves and persists the Firebase refresh token without ever
// writing it to mj's own config. Primary path: hasp-brokered env injection
// (the user runs mj via `hasp inject`/`hasp run`, which sets the env var).
// Login-time persistence shells out to the hasp CLI best-effort.
package creds

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// EnvVar is the env var hasp injects (or a user exports) with the refresh token.
const EnvVar = "MIDJOURNEY_FIREBASE_REFRESH_TOKEN"

// SecretName is the hasp vault secret name.
const SecretName = "MIDJOURNEY_FIREBASE_REFRESH_TOKEN"

// FromEnv returns the refresh token from the injected env var, if present.
func FromEnv() string { return strings.TrimSpace(os.Getenv(EnvVar)) }

// HaspAvailable reports whether the hasp CLI is on PATH.
func HaspAvailable() bool {
	_, err := exec.LookPath("hasp")
	return err == nil
}

// HaspStore is a CredStore backed by the hasp CLI. Get prefers the injected env
// var (zero-coupling to hasp internals); Set stores via `hasp secret`.
type HaspStore struct{}

// Get returns the token from the env var (set by `hasp inject`). It does not
// attempt to drive hasp's interactive broker from within mj.
func (HaspStore) Get(ctx context.Context) (string, error) {
	if t := FromEnv(); t != "" {
		return t, nil
	}
	return "", fmt.Errorf("hasp: token not injected; run mj via `hasp inject --env %s=%s -- mj …`", EnvVar, SecretName)
}

// Set stores/updates the secret in the hasp vault (best-effort). The value is
// passed on stdin to avoid it appearing in argv/process listing.
func (HaspStore) Set(ctx context.Context, refreshToken string) error {
	if !HaspAvailable() {
		return fmt.Errorf("hasp not found on PATH")
	}
	// `hasp secret add <name>` reading the value from stdin. Tries update on conflict.
	for _, verb := range []string{"add", "update"} {
		cmd := exec.CommandContext(ctx, "hasp", "secret", verb, SecretName, "--stdin")
		cmd.Stdin = strings.NewReader(refreshToken)
		var stderr bytes.Buffer
		cmd.Stderr = &stderr
		if err := cmd.Run(); err == nil {
			return nil
		}
	}
	return fmt.Errorf("hasp secret add/update failed for %s (store it manually: hasp secret add %s)", SecretName, SecretName)
}

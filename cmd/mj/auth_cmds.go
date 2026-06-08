package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/ehmo/mj/internal/creds"
	"github.com/ehmo/mj/internal/mjconfig"
)

func cmdLogin(ctx context.Context, args []string) error {
	fs := flag.NewFlagSet("login", flag.ExitOnError)
	ack := fs.Bool("i-understand", false, "acknowledge the ToS automation risk")
	_ = fs.Parse(args)

	fmt.Fprintln(os.Stderr, tosClause)
	if !*ack {
		return fmt.Errorf("re-run with --i-understand to proceed")
	}
	cfg, err := mjconfig.Load()
	if err != nil {
		return err
	}
	cfg.TOSAck = true
	if err := cfg.Save(); err != nil {
		return err
	}

	c, _, err := openClient(ctx, true) // headful
	if err != nil {
		return err
	}
	defer c.Close()

	fmt.Fprintln(os.Stderr, "\nA browser window opened. Log in to Midjourney (Google or Discord),")
	fmt.Fprintln(os.Stderr, "wait until your Create page loads, then press Enter here.")
	bufio.NewReader(os.Stdin).ReadString('\n')

	if token, err := c.ReadRefreshToken(ctx); err != nil {
		return fmt.Errorf("could not read login session: %w", err)
	} else if token == "" {
		return fmt.Errorf("no Firebase session found — did the login complete?")
	}
	// Validate by establishing a session and reading the account.
	acct, err := c.Account(ctx)
	if err != nil {
		return fmt.Errorf("login validation failed: %w", err)
	}
	// Use the token EnsureSession actually validated (post any Firebase rotation),
	// not the pre-rotation value read from IndexedDB above.
	token := c.CurrentRefreshToken()
	if token == "" {
		token, _ = c.ReadRefreshToken(ctx)
	}
	cfg.UserID = acct.UserID
	if err := cfg.Save(); err != nil {
		return err
	}
	if token != "" && creds.HaspAvailable() {
		if err := (creds.HaspStore{}).Set(ctx, token); err != nil {
			fmt.Fprintln(os.Stderr, "note: could not store token in hasp:", err)
			fmt.Fprintf(os.Stderr, "      the local browser profile retains the session; or store it yourself:\n      hasp secret add %s\n", creds.SecretName)
		} else {
			fmt.Fprintf(os.Stderr, "stored refresh token in hasp (%s)\n", creds.SecretName)
		}
	}
	fmt.Printf("logged in as %s (plan: %s)\n", acct.UserID, orDash(acct.PlanType))
	return nil
}

func cmdLogout(ctx context.Context, args []string) error {
	cfg, err := mjconfig.Load()
	if err != nil {
		return err
	}
	profile, err := cfg.ProfilePath()
	if err == nil && profile != "" {
		if err := os.RemoveAll(profile); err != nil {
			fmt.Fprintln(os.Stderr, "warning: could not remove profile:", err)
		}
	}
	cfg.UserID = ""
	if err := cfg.Save(); err != nil {
		return err
	}
	if creds.HaspAvailable() {
		// delete (not hide) so a later login as a different account can't pick up
		// the old refresh token from the vault.
		_ = exec.CommandContext(ctx, "hasp", "secret", "delete", creds.SecretName).Run()
	}
	fmt.Println("logged out (local session removed)")
	return nil
}

func cmdAuth(ctx context.Context, args []string) error {
	sub := "status"
	if len(args) > 0 {
		sub = args[0]
	}
	if sub != "status" {
		return fmt.Errorf("usage: mj auth status")
	}
	c, cfg, err := openClient(ctx, false)
	if err != nil {
		return err
	}
	defer c.Close()
	acct, err := c.Account(ctx)
	if err != nil {
		fmt.Printf("session: NOT authenticated (%v)\n", err)
		return err
	}
	fmt.Println("session: OK")
	fmt.Printf("user:    %s\n", acct.UserID)
	fmt.Printf("plan:    %s\n", orDash(acct.PlanType))
	fmt.Printf("tos_ack: %v\n", cfg.TOSAck)
	return nil
}

func cmdDoctor(ctx context.Context, args []string) error {
	ok := true
	check := func(name string, pass bool, detail string) {
		mark := "OK  "
		if !pass {
			mark = "FAIL"
			ok = false
		}
		fmt.Printf("[%s] %-14s %s\n", mark, name, detail)
	}

	// gomoufox managed venv
	cacheBase, _ := os.UserCacheDir()
	venv := filepath.Join(cacheBase, "gomoufox", "venv")
	_, venvErr := os.Stat(venv)
	check("gomoufox venv", venvErr == nil, venv+" (run any mj command to auto-install)")

	// hasp
	check("hasp", creds.HaspAvailable(), "credential broker (optional but recommended)")

	// credentials
	cfg, _ := mjconfig.Load()
	hasEnv := creds.FromEnv() != ""
	profile, _ := cfg.ProfilePath()
	_, profErr := os.Stat(profile)
	check("credentials", hasEnv || profErr == nil,
		fmt.Sprintf("env=%v profile=%v (run `mj login` if both false)", hasEnv, profErr == nil))

	// ToS ack
	check("tos_ack", cfg.TOSAck, "set by `mj login --i-understand`")

	if !ok {
		return fmt.Errorf("doctor: some checks failed")
	}
	fmt.Println("doctor: ready")
	return nil
}

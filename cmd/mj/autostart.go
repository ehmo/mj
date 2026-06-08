package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"

	"github.com/ehmo/mj/internal/mjclient"
	"github.com/ehmo/mj/internal/mjconfig"
)

// autostartEnabled reports whether headless commands may spawn a daemon.
// MJ_DAEMON_AUTOSTART=1/0 overrides the config setting.
func autostartEnabled(cfg mjconfig.Config) bool {
	switch os.Getenv("MJ_DAEMON_AUTOSTART") {
	case "1", "true":
		return true
	case "0", "false":
		return false
	}
	return cfg.DaemonAutostart
}

// maybeAutostartDaemon spawns a detached `mj serve` if autostart is enabled and
// no daemon is running, then waits (briefly) for it to accept connections.
// Best-effort: failures are ignored (caller falls back to a local browser).
func maybeAutostartDaemon(cfg mjconfig.Config) {
	if !autostartEnabled(cfg) {
		return
	}
	sock := mjclient.DaemonSocketPath()
	if mjclient.DaemonAvailable(sock) {
		return
	}
	// Serialize autostart across concurrent CLI invocations with an exclusive
	// file lock, so two commands don't each spawn a daemon (one would be
	// orphaned). If another process holds the lock, wait for its socket instead.
	lockf, err := os.OpenFile(autostartLockPath(), os.O_CREATE|os.O_RDWR, 0o600)
	if err == nil {
		defer lockf.Close()
		if syscall.Flock(int(lockf.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) != nil {
			// someone else is spawning — wait briefly for their socket
			waitForSocket(sock, 75*time.Second)
			return
		}
		defer syscall.Flock(int(lockf.Fd()), syscall.LOCK_UN)
		if mjclient.DaemonAvailable(sock) { // a daemon appeared while we waited for the lock
			return
		}
	}
	exe, err := os.Executable()
	if err != nil {
		return
	}
	logPath := daemonLogPath()
	logf, _ := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)

	cmd := exec.Command(exe, "serve", "--idle", "30m")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true} // detach from this process
	if logf != nil {
		cmd.Stdout = logf
		cmd.Stderr = logf
	}
	if err := cmd.Start(); err != nil {
		return
	}
	// Don't reap; let it run independently.
	go func() { _ = cmd.Process.Release() }()

	waitForSocket(sock, 75*time.Second)
}

func waitForSocket(sock string, timeout time.Duration) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if mjclient.DaemonAvailable(sock) {
			return
		}
		time.Sleep(400 * time.Millisecond)
	}
}

func daemonLogPath() string {
	if d, err := mjconfig.DataDir(); err == nil {
		return filepath.Join(d, "daemon.log")
	}
	return filepath.Join(os.TempDir(), "mj-daemon.log")
}

func autostartLockPath() string {
	if d, err := mjconfig.DataDir(); err == nil {
		return filepath.Join(d, "daemon.autostart.lock")
	}
	return filepath.Join(os.TempDir(), "mj-daemon.autostart.lock")
}

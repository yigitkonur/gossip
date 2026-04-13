// Package daemon owns the AgentBridge daemon lifecycle: PID file, liveness
// check, and self-launch as a subprocess of the main binary.
package daemon

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/raysonmeng/agent-bridge/internal/statedir"
)

// LifecycleOptions configures a Lifecycle.
type LifecycleOptions struct {
	StateDir    *statedir.StateDir
	ControlPort int
	Logger      func(msg string)
}

// Lifecycle wraps daemon PID, status, and killed-sentinel bookkeeping.
type Lifecycle struct {
	opts LifecycleOptions
}

// NewLifecycle constructs a Lifecycle.
func NewLifecycle(opts LifecycleOptions) *Lifecycle { return &Lifecycle{opts: opts} }

// HealthURL returns the daemon /healthz URL.
func (l *Lifecycle) HealthURL() string {
	return fmt.Sprintf("http://127.0.0.1:%d/healthz", l.opts.ControlPort)
}

// ReadyURL returns the daemon /readyz URL.
func (l *Lifecycle) ReadyURL() string {
	return fmt.Sprintf("http://127.0.0.1:%d/readyz", l.opts.ControlPort)
}

// ControlWsURL returns the control WebSocket URL.
func (l *Lifecycle) ControlWsURL() string {
	return fmt.Sprintf("ws://127.0.0.1:%d/ws", l.opts.ControlPort)
}

// WriteKilled writes the sentinel so Claude Code sessions do not auto-reconnect.
func (l *Lifecycle) WriteKilled() error {
	return os.WriteFile(l.opts.StateDir.KilledFile(), []byte(time.Now().Format(time.RFC3339)+"\n"), 0o644)
}

// WasKilled reports whether the sentinel is present.
func (l *Lifecycle) WasKilled() bool {
	_, err := os.Stat(l.opts.StateDir.KilledFile())
	return err == nil
}

// ClearKilled removes the sentinel.
func (l *Lifecycle) ClearKilled() { _ = os.Remove(l.opts.StateDir.KilledFile()) }

// WritePid writes our own PID to the pid file.
func (l *Lifecycle) WritePid() error {
	return os.WriteFile(l.opts.StateDir.PidFile(), []byte(strconv.Itoa(os.Getpid())+"\n"), 0o644)
}

// RemovePid removes the pid file.
func (l *Lifecycle) RemovePid() { _ = os.Remove(l.opts.StateDir.PidFile()) }

// RemoveStatusFile removes the daemon status file.
func (l *Lifecycle) RemoveStatusFile() { _ = os.Remove(l.opts.StateDir.StatusFile()) }

// ReadPid returns the daemon pid, or 0 if missing.
func (l *Lifecycle) ReadPid() int {
	b, err := os.ReadFile(l.opts.StateDir.PidFile())
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(string(bytesTrimSpace(b)))
	if err != nil {
		return 0
	}
	return pid
}

// IsProcessAlive tests whether pid is running.
func IsProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	return proc.Signal(syscall.Signal(0)) == nil
}

// IsHealthy pings /healthz and returns true on HTTP 200.
func (l *Lifecycle) IsHealthy(ctx context.Context) bool {
	client := &http.Client{Timeout: time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, l.HealthURL(), nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// IsReady pings /readyz and returns true on HTTP 200.
func (l *Lifecycle) IsReady(ctx context.Context) bool {
	client := &http.Client{Timeout: time.Second}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, l.ReadyURL(), nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	_ = resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// EnsureRunning checks health, then PID, then launches as needed.
func (l *Lifecycle) EnsureRunning(ctx context.Context) error {
	if l.IsHealthy(ctx) {
		return l.waitReady(ctx)
	}
	if pid := l.ReadPid(); pid > 0 {
		if IsProcessAlive(pid) && l.isDaemonProcess(pid) {
			return l.waitReady(ctx)
		}
		l.cleanup()
	}
	if !l.acquireLock() {
		return l.waitReady(ctx)
	}
	defer l.releaseLock()
	return l.Launch(ctx)
}

// Launch spawns the daemon subprocess by re-invoking the current binary with the hidden daemon subcommand.
func (l *Lifecycle) Launch(ctx context.Context) error {
	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve self: %w", err)
	}
	cmd := exec.Command(self, "daemon")
	cmd.SysProcAttr = daemonSysProcAttr()
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil
	if err := cmd.Start(); err != nil {
		return err
	}
	_ = cmd.Process.Release()
	return l.waitReady(ctx)
}

// Kill stops the daemon process referenced by the pid file.
func (l *Lifecycle) Kill(gracefulTimeout time.Duration) (bool, error) {
	pid := l.ReadPid()
	if pid == 0 {
		l.cleanup()
		return false, nil
	}
	if !IsProcessAlive(pid) {
		l.cleanup()
		return false, nil
	}
	if !l.isDaemonProcess(pid) {
		l.cleanup()
		return false, nil
	}
	if err := syscall.Kill(pid, syscall.SIGTERM); err != nil {
		l.cleanup()
		return false, err
	}
	deadline := time.Now().Add(gracefulTimeout)
	for time.Now().Before(deadline) {
		if !IsProcessAlive(pid) {
			l.cleanup()
			return true, nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	_ = syscall.Kill(pid, syscall.SIGKILL)
	l.cleanup()
	return true, nil
}

func (l *Lifecycle) waitReady(ctx context.Context) error {
	deadline := time.Now().Add(10 * time.Second)
	for {
		if l.IsReady(ctx) {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("daemon did not become ready")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(250 * time.Millisecond):
		}
	}
}

func (l *Lifecycle) acquireLock() bool {
	l.opts.StateDir.Ensure()
	f, err := os.OpenFile(l.opts.StateDir.LockFile(), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
	if err == nil {
		_, _ = f.WriteString(strconv.Itoa(os.Getpid()) + "\n")
		_ = f.Close()
		return true
	}
	b, readErr := os.ReadFile(l.opts.StateDir.LockFile())
	if readErr != nil {
		return false
	}
	pid, convErr := strconv.Atoi(string(bytesTrimSpace(b)))
	if convErr != nil || !IsProcessAlive(pid) {
		l.releaseLock()
		f, err = os.OpenFile(l.opts.StateDir.LockFile(), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			return false
		}
		_, _ = f.WriteString(strconv.Itoa(os.Getpid()) + "\n")
		_ = f.Close()
		return true
	}
	return false
}

func (l *Lifecycle) releaseLock() { _ = os.Remove(l.opts.StateDir.LockFile()) }

func (l *Lifecycle) cleanup() {
	l.RemovePid()
	l.RemoveStatusFile()
	l.releaseLock()
}

func (l *Lifecycle) isDaemonProcess(pid int) bool {
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "command=").Output()
	if err != nil {
		return false
	}
	cmd := string(bytesTrimSpace(out))
	return strings.Contains(cmd, "daemon") && (strings.Contains(cmd, "agentbridge") || strings.Contains(cmd, "agent_bridge"))
}

func bytesTrimSpace(b []byte) []byte {
	start := 0
	for start < len(b) && (b[start] == ' ' || b[start] == '\n' || b[start] == '\t' || b[start] == '\r') {
		start++
	}
	end := len(b)
	for end > start && (b[end-1] == ' ' || b[end-1] == '\n' || b[end-1] == '\t' || b[end-1] == '\r') {
		end--
	}
	return b[start:end]
}

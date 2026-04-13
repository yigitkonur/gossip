// Package daemon owns the AgentBridge daemon lifecycle: PID file, liveness
// checks, and self-launching the daemon as a subprocess of the main binary.
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

const (
	defaultHealthTimeout = 1 * time.Second
	defaultReadyTimeout  = 10 * time.Second
	defaultReadyPoll     = 250 * time.Millisecond
)

// LifecycleOptions configures a Lifecycle.
type LifecycleOptions struct {
	StateDir    *statedir.StateDir
	ControlPort int
	Logger      func(msg string)
}

// Lifecycle wraps daemon PID, health, and killed-sentinel bookkeeping.
type Lifecycle struct {
	opts LifecycleOptions
}

// NewLifecycle constructs a Lifecycle.
func NewLifecycle(opts LifecycleOptions) *Lifecycle {
	return &Lifecycle{opts: opts}
}

// HealthURL returns the daemon /healthz URL.
func (l *Lifecycle) HealthURL() string {
	return fmt.Sprintf("http://127.0.0.1:%d/healthz", l.opts.ControlPort)
}

// ReadyURL returns the daemon /readyz URL.
func (l *Lifecycle) ReadyURL() string {
	return fmt.Sprintf("http://127.0.0.1:%d/readyz", l.opts.ControlPort)
}

// ControlWsURL returns the daemon control WebSocket URL.
func (l *Lifecycle) ControlWsURL() string {
	return fmt.Sprintf("ws://127.0.0.1:%d/ws", l.opts.ControlPort)
}

// WriteKilled writes the sentinel so intentional shutdowns are remembered.
func (l *Lifecycle) WriteKilled() error {
	if err := l.ensureStateDir(); err != nil {
		return err
	}
	stamp := time.Now().Format(time.RFC3339) + "\n"
	return os.WriteFile(l.opts.StateDir.KilledFile(), []byte(stamp), 0o644)
}

// WasKilled reports whether the intentional-shutdown sentinel is present.
func (l *Lifecycle) WasKilled() bool {
	if l.opts.StateDir == nil {
		return false
	}
	_, err := os.Stat(l.opts.StateDir.KilledFile())
	return err == nil
}

// ClearKilled removes the killed sentinel.
func (l *Lifecycle) ClearKilled() {
	if l.opts.StateDir == nil {
		return
	}
	_ = os.Remove(l.opts.StateDir.KilledFile())
}

// WritePid writes the current process PID to the pid file.
func (l *Lifecycle) WritePid() error {
	if err := l.ensureStateDir(); err != nil {
		return err
	}
	return os.WriteFile(l.opts.StateDir.PidFile(), []byte(strconv.Itoa(os.Getpid())), 0o644)
}

// ReadPid returns the pid from the pid file, or 0 when unavailable.
func (l *Lifecycle) ReadPid() int {
	if l.opts.StateDir == nil {
		return 0
	}
	b, err := os.ReadFile(l.opts.StateDir.PidFile())
	if err != nil {
		return 0
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil {
		return 0
	}
	return pid
}

// RemovePid removes the pid file.
func (l *Lifecycle) RemovePid() {
	if l.opts.StateDir == nil {
		return
	}
	_ = os.Remove(l.opts.StateDir.PidFile())
}

// IsProcessAlive reports whether the given process is still running.
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

// IsHealthy pings the daemon /healthz endpoint and reports whether it returns 200.
func (l *Lifecycle) IsHealthy(ctx context.Context) bool {
	client := &http.Client{Timeout: defaultHealthTimeout}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, l.HealthURL(), nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// EnsureRunning verifies the daemon is healthy or launches it if needed.
func (l *Lifecycle) EnsureRunning(ctx context.Context) error {
	if l.IsHealthy(ctx) {
		return l.waitReady(ctx)
	}
	if pid := l.ReadPid(); pid > 0 && IsProcessAlive(pid) {
		return l.waitReady(ctx)
	}
	l.RemovePid()
	return l.Launch(ctx)
}

// Launch starts the daemon by re-invoking the current binary with the hidden
// daemon subcommand.
func (l *Lifecycle) Launch(ctx context.Context) error {
	if err := l.ensureStateDir(); err != nil {
		return err
	}

	self, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve self: %w", err)
	}

	cmd := exec.Command(self, "daemon")
	cmd.SysProcAttr = daemonSysProcAttr()
	cmd.Env = append(os.Environ(),
		fmt.Sprintf("AGENTBRIDGE_CONTROL_PORT=%d", l.opts.ControlPort),
		fmt.Sprintf("AGENTBRIDGE_STATE_DIR=%s", l.opts.StateDir.Dir()),
	)
	cmd.Stdout = nil
	cmd.Stderr = nil
	cmd.Stdin = nil

	if err := cmd.Start(); err != nil {
		return err
	}
	if cmd.Process != nil {
		_ = cmd.Process.Release()
	}
	l.logf("launched daemon subprocess on control port %d", l.opts.ControlPort)
	return l.waitReady(ctx)
}

func (l *Lifecycle) waitReady(ctx context.Context) error {
	deadline := time.Now().Add(defaultReadyTimeout)
	for {
		if l.IsHealthy(ctx) {
			return nil
		}
		if time.Now().After(deadline) {
			return errors.New("daemon did not become healthy")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(defaultReadyPoll):
		}
	}
}

func (l *Lifecycle) ensureStateDir() error {
	if l.opts.StateDir == nil {
		return errors.New("daemon lifecycle requires a state dir")
	}
	return l.opts.StateDir.Ensure()
}

func (l *Lifecycle) logf(format string, args ...any) {
	if l.opts.Logger == nil {
		return
	}
	l.opts.Logger(fmt.Sprintf(format, args...))
}

// Package codex drives a local `codex app-server` subprocess and mediates
// between the Codex TUI and Claude Code. It is the Go port of src/codex-adapter.ts.
package codex

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

var (
	processLookPath = exec.LookPath
	processCommand  = exec.CommandContext
	processRun      = func(ctx context.Context, name string, args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, name, args...)
		out, err := cmd.CombinedOutput()
		return strings.TrimSpace(string(out)), err
	}
)

// ProcessOptions configures a Codex app-server subprocess.
type ProcessOptions struct {
	Binary string
	Port   int
	Logger func(stream, line string)
}

// Process manages one codex app-server subprocess.
type Process struct {
	opts ProcessOptions

	mu   sync.Mutex
	cmd  *exec.Cmd
	done chan struct{}
}

// NewProcess constructs a Process with the given options.
func NewProcess(opts ProcessOptions) *Process {
	if opts.Binary == "" {
		opts.Binary = "codex"
	}
	if opts.Port == 0 {
		opts.Port = 4500
	}
	return &Process{opts: opts}
}

// HealthURL returns the /healthz endpoint for the app-server.
func (p *Process) HealthURL() string {
	return fmt.Sprintf("http://127.0.0.1:%d/healthz", p.opts.Port)
}

// WebSocketURL returns the ws:// URL to dial the app-server.
func (p *Process) WebSocketURL() string {
	return fmt.Sprintf("ws://127.0.0.1:%d", p.opts.Port)
}

// Start launches the subprocess and waits for /healthz to return 200.
func (p *Process) Start(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.cmd != nil {
		return errors.New("codex process already started")
	}

	if _, err := processLookPath(p.opts.Binary); err != nil {
		return err
	}
	if err := p.cleanupPort(ctx); err != nil {
		return err
	}

	cmd := processCommand(ctx, p.opts.Binary, "app-server", "--listen", p.WebSocketURL())
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("codex stdout pipe: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("codex stderr pipe: %w", err)
	}
	cmd.Cancel = func() error { return cmd.Process.Signal(syscall.SIGTERM) }
	cmd.WaitDelay = 3 * time.Second

	if err := cmd.Start(); err != nil {
		return err
	}

	p.cmd = cmd
	p.done = make(chan struct{})

	var wg sync.WaitGroup
	wg.Add(2)
	go func() { defer wg.Done(); p.pumpLines("stdout", stdout) }()
	go func() { defer wg.Done(); p.pumpLines("stderr", stderr) }()
	go func() {
		wg.Wait()      // drain all pipe output first
		_ = cmd.Wait() // then reap the process
		close(p.done)  // then signal exit
	}()

	return p.waitForHealthy(ctx)
}

// Stop sends SIGTERM to the subprocess and waits for it to exit.
func (p *Process) Stop(ctx context.Context) error {
	p.mu.Lock()
	cmd := p.cmd
	done := p.done
	p.mu.Unlock()
	if cmd == nil || cmd.Process == nil {
		return nil
	}
	_ = cmd.Process.Signal(syscall.SIGTERM)
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		_ = cmd.Process.Kill()
		return ctx.Err()
	}
}

// Done returns a channel that is closed when the subprocess exits.
func (p *Process) Done() <-chan struct{} {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.done
}

func (p *Process) cleanupPort(ctx context.Context) error {
	pids, err := p.portPIDs(ctx)
	if err != nil {
		return err
	}
	if len(pids) == 0 {
		return nil
	}

	staleCodex := make([]int, 0, len(pids))
	foreign := make([]int, 0)
	for _, pid := range pids {
		cmdline, err := p.processCommandLine(ctx, pid)
		if err != nil {
			continue
		}
		if strings.Contains(cmdline, "codex") && strings.Contains(cmdline, "app-server") {
			staleCodex = append(staleCodex, pid)
			continue
		}
		foreign = append(foreign, pid)
	}
	if len(foreign) > 0 {
		return fmt.Errorf("port %d is already in use by non-Codex process(es): PID(s) %s", p.opts.Port, joinPIDs(foreign))
	}

	for _, pid := range staleCodex {
		if _, err := processRun(ctx, "kill", strconv.Itoa(pid)); err != nil && p.opts.Logger != nil {
			p.opts.Logger("stderr", fmt.Sprintf("failed to kill stale codex app-server pid %d: %v", pid, err))
		}
	}
	if len(staleCodex) == 0 {
		return nil
	}

	time.Sleep(500 * time.Millisecond)
	remaining, err := p.portPIDs(ctx)
	if err != nil {
		return err
	}
	if len(remaining) > 0 {
		return fmt.Errorf("port %d is still occupied after cleanup: PID(s) %s", p.opts.Port, joinPIDs(remaining))
	}
	return nil
}

func (p *Process) portPIDs(ctx context.Context) ([]int, error) {
	out, err := processRun(ctx, "lsof", "-ti", ":"+strconv.Itoa(p.opts.Port))
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && len(strings.TrimSpace(string(exitErr.Stderr))) == 0 && strings.TrimSpace(out) == "" {
			return nil, nil
		}
		return nil, err
	}
	if strings.TrimSpace(out) == "" {
		return nil, nil
	}

	lines := strings.Split(out, "\n")
	pids := make([]int, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		pid, err := strconv.Atoi(line)
		if err != nil {
			return nil, err
		}
		pids = append(pids, pid)
	}
	return pids, nil
}

func (p *Process) processCommandLine(ctx context.Context, pid int) (string, error) {
	return processRun(ctx, "ps", "-p", strconv.Itoa(pid), "-o", "command=")
}

func joinPIDs(pids []int) string {
	parts := make([]string, 0, len(pids))
	for _, pid := range pids {
		parts = append(parts, strconv.Itoa(pid))
	}
	return strings.Join(parts, ", ")
}

func (p *Process) waitForHealthy(ctx context.Context) error {
	client := &http.Client{Timeout: 1 * time.Second}
	deadline := time.Now().Add(10 * time.Second)
	for {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.HealthURL(), nil)
		if err != nil {
			return err
		}
		resp, err := client.Do(req)
		if err == nil {
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				return nil
			}
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("codex app-server did not become healthy at %s", p.HealthURL())
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}

func (p *Process) pumpLines(stream string, r io.Reader) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 1024*1024)
	for scanner.Scan() {
		if p.opts.Logger != nil {
			p.opts.Logger(stream, scanner.Text())
		}
	}
}

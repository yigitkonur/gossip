package codex

import (
	"context"
	"errors"
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestProcess_StartFailsOnMissingBinary(t *testing.T) {
	p := NewProcess(ProcessOptions{
		Binary: "/nonexistent/codex-binary-does-not-exist",
		Port:   45000,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	err := p.Start(ctx)
	if err == nil {
		t.Fatal("expected error for missing binary")
	}
	var execErr *exec.Error
	if !errors.As(err, &execErr) {
		t.Errorf("want exec.Error, got %T: %v", err, err)
	}
}

func TestProcess_HealthURL(t *testing.T) {
	p := NewProcess(ProcessOptions{Port: 4500})
	if got := p.HealthURL(); got != "http://127.0.0.1:4500/healthz" {
		t.Errorf("HealthURL() = %q", got)
	}
	if got := p.WebSocketURL(); got != "ws://127.0.0.1:4500" {
		t.Errorf("WebSocketURL() = %q", got)
	}
}

func TestProcess_CleanupPortRejectsForeignProcess(t *testing.T) {
	restore := overrideProcessRun(t, func(_ context.Context, name string, args ...string) (string, error) {
		switch {
		case name == "lsof":
			return "1234", nil
		case name == "ps":
			return "/usr/bin/python app.py", nil
		default:
			return "", nil
		}
	})
	defer restore()

	p := NewProcess(ProcessOptions{Port: 4500})
	err := p.cleanupPort(context.Background())
	if err == nil || !strings.Contains(err.Error(), "non-Codex process(es): PID(s) 1234") {
		t.Fatalf("cleanupPort() error = %v, want foreign-process failure", err)
	}
}

func TestProcess_CleanupPortKillsStaleCodexProcess(t *testing.T) {
	var killCalls []string
	var lsofCalls int
	restore := overrideProcessRun(t, func(_ context.Context, name string, args ...string) (string, error) {
		switch {
		case name == "lsof":
			lsofCalls++
			if lsofCalls == 1 {
				return "2222", nil
			}
			return "", nil
		case name == "ps":
			return "codex app-server --listen ws://127.0.0.1:4500", nil
		case name == "kill":
			killCalls = append(killCalls, args[0])
			return "", nil
		default:
			return "", nil
		}
	})
	defer restore()

	p := NewProcess(ProcessOptions{Port: 4500})
	if err := p.cleanupPort(context.Background()); err != nil {
		t.Fatalf("cleanupPort() error = %v", err)
	}
	if lsofCalls != 2 {
		t.Fatalf("lsof call count = %d, want 2", lsofCalls)
	}
	if len(killCalls) != 1 || killCalls[0] != "2222" {
		t.Fatalf("kill calls = %#v, want [2222]", killCalls)
	}
}

func TestProcess_CleanupPortSkipsWhenLsofUnavailable(t *testing.T) {
	var logs []string
	restore := overrideProcessRun(t, func(_ context.Context, name string, args ...string) (string, error) {
		if name == "lsof" {
			return "", &exec.Error{Name: "lsof", Err: exec.ErrNotFound}
		}
		return "", nil
	})
	defer restore()

	p := NewProcess(ProcessOptions{
		Port: 4500,
		Logger: func(stream, line string) {
			logs = append(logs, stream+":"+line)
		},
	})
	if err := p.cleanupPort(context.Background()); err != nil {
		t.Fatalf("cleanupPort() error = %v, want nil when lsof is unavailable", err)
	}
	if len(logs) != 1 || !strings.Contains(logs[0], "skipping port cleanup check for 4500") {
		t.Fatalf("logs = %#v, want missing-lsof warning", logs)
	}
}

func TestProcess_StopFallsBackToSigKillAfterGracePeriod(t *testing.T) {
	if _, err := exec.LookPath("sh"); err != nil {
		t.Skipf("sh not available: %v", err)
	}

	cmd := exec.Command("sh", "-c", `trap '' TERM; while :; do sleep 1; done`)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start stubborn process: %v", err)
	}
	done := make(chan struct{})
	go func() {
		_ = cmd.Wait()
		close(done)
	}()
	defer func() {
		_ = cmd.Process.Kill()
		<-done
	}()

	p := &Process{cmd: cmd, done: done}
	ctx, cancel := context.WithTimeout(context.Background(), stopKillGracePeriod+3*time.Second)
	defer cancel()

	started := time.Now()
	if err := p.Stop(ctx); err != nil {
		t.Fatalf("Stop() error = %v", err)
	}
	if elapsed := time.Since(started); elapsed > stopKillGracePeriod+2*time.Second {
		t.Fatalf("Stop() took %s, want SIGKILL fallback before timeout window", elapsed)
	}
}

func overrideProcessRun(t *testing.T, runFn func(context.Context, string, ...string) (string, error)) func() {
	t.Helper()
	prevRun := processRun
	processRun = runFn
	return func() {
		processRun = prevRun
	}
}

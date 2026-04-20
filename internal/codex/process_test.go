package codex

import (
	"context"
	"errors"
	"os/exec"
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

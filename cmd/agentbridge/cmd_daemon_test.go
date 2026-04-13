package main

import (
	"testing"
	"time"

	"github.com/raysonmeng/agent-bridge/internal/config"
	"github.com/raysonmeng/agent-bridge/internal/statedir"
)

func TestDaemonOptionsFromConfig_UsesControlPortHelper(t *testing.T) {
	t.Setenv("AGENTBRIDGE_CONTROL_PORT", "45123")
	sd := statedir.New(t.TempDir())
	cfg := config.DefaultConfig
	cfg.Daemon.Port = 4600
	cfg.Daemon.ProxyPort = 4601
	cfg.IdleShutdownSeconds = 45

	opts := daemonOptionsFromConfig(sd, cfg)
	if opts.ControlPort != 45123 {
		t.Fatalf("ControlPort = %d, want 45123", opts.ControlPort)
	}
	if opts.IdleShutdown != 45*time.Second {
		t.Fatalf("IdleShutdown = %s", opts.IdleShutdown)
	}
}

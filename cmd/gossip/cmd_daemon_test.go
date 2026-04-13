package main

import (
	"testing"
	"time"

	"github.com/yigitkonur/gossip/internal/config"
	"github.com/yigitkonur/gossip/internal/statedir"
)

func TestDaemonOptionsFromConfig_UsesControlPortHelper(t *testing.T) {
	t.Setenv("GOSSIP_CONTROL_PORT", "45123")
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


type fakeEnsurer struct{ err error }

func (f fakeEnsurer) Ensure() error { return f.err }

type fakePidWriter struct{ err error }

func (f fakePidWriter) WritePid() error { return f.err }

func TestEnsureDaemonState_ReturnsEnsureError(t *testing.T) {
	want := errString("ensure failed")
	if err := ensureDaemonState(fakeEnsurer{err: want}, fakePidWriter{}); err != want {
		t.Fatalf("ensureDaemonState() error = %v, want %v", err, want)
	}
}

func TestEnsureDaemonState_ReturnsWritePidError(t *testing.T) {
	want := errString("write pid failed")
	if err := ensureDaemonState(fakeEnsurer{}, fakePidWriter{err: want}); err != want {
		t.Fatalf("ensureDaemonState() error = %v, want %v", err, want)
	}
}

type errString string

func (e errString) Error() string { return string(e) }

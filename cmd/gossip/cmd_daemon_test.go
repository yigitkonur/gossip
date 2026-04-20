package main

import (
	"testing"
	"time"

	"github.com/yigitkonur/gossip/internal/config"
	"github.com/yigitkonur/gossip/internal/filter"
	"github.com/yigitkonur/gossip/internal/statedir"
)

func TestDaemonOptionsFromConfig_UsesControlPortHelper(t *testing.T) {
	t.Setenv("GOSSIP_CONTROL_PORT", "45123")
	sd := statedir.New(t.TempDir())
	cfg := config.DefaultConfig
	cfg.Daemon.Port = 4600
	cfg.Daemon.ProxyPort = 4601
	cfg.IdleShutdownSeconds = 45
	cfg.TurnCoordination.AttentionWindowSeconds = 22

	opts := daemonOptionsFromConfig(sd, cfg)
	if opts.ControlPort != 45123 {
		t.Fatalf("ControlPort = %d, want 45123", opts.ControlPort)
	}
	if opts.AttentionWindow != 22*time.Second {
		t.Fatalf("AttentionWindow = %s, want 22s", opts.AttentionWindow)
	}
	if opts.IdleShutdown != 45*time.Second {
		t.Fatalf("IdleShutdown = %s", opts.IdleShutdown)
	}
}

func TestAttentionWindowFromConfig_EnvOverrideWins(t *testing.T) {
	t.Setenv("GOSSIP_ATTENTION_WINDOW_MS", "2500")
	t.Setenv("AGENTBRIDGE_ATTENTION_WINDOW_MS", "7000")
	if got := attentionWindowFromConfig(config.DefaultConfig); got != 2500*time.Millisecond {
		t.Fatalf("attentionWindowFromConfig() = %s, want 2500ms", got)
	}
}

func TestDaemonFilterMode_UsesPrimaryEnvAndLogsMode(t *testing.T) {
	t.Setenv("GOSSIP_FILTER_MODE", "passthrough")
	t.Setenv("AGENTBRIDGE_FILTER_MODE", "filtered")
	var logs []string
	got := daemonFilterMode(func(msg string) { logs = append(logs, msg) })
	if got != filter.ModeFull {
		t.Fatalf("daemonFilterMode() = %q, want %q", got, filter.ModeFull)
	}
	if len(logs) != 1 || logs[0] != "Filter mode: full" {
		t.Fatalf("logs = %#v, want Filter mode: full", logs)
	}
}

func TestDaemonFilterMode_DefaultsToFiltered(t *testing.T) {
	got := daemonFilterMode(nil)
	if got != filter.ModeFiltered {
		t.Fatalf("daemonFilterMode() = %q, want %q", got, filter.ModeFiltered)
	}
}

func TestParseFilterMode_AcceptsExplicitModes(t *testing.T) {
	tests := []struct {
		raw  string
		want filter.Mode
	}{
		{raw: "filtered", want: filter.ModeFiltered},
		{raw: "full", want: filter.ModeFull},
		{raw: "passthrough", want: filter.ModeFull},
	}

	for _, tt := range tests {
		got, ok := parseFilterMode(tt.raw)
		if !ok {
			t.Fatalf("parseFilterMode(%q) returned ok=false", tt.raw)
		}
		if got != tt.want {
			t.Fatalf("parseFilterMode(%q) = %q, want %q", tt.raw, got, tt.want)
		}
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

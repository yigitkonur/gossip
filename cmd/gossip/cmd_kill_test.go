package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestIsManagedCodexCommand(t *testing.T) {
	tests := []struct {
		name string
		cmd  string
		want bool
	}{
		{name: "split flags", cmd: `codex --enable tui_app_server --remote ws://127.0.0.1:4501`, want: true},
		{name: "equals flags", cmd: `codex --enable=tui_app_server --remote=ws://127.0.0.1:4501`, want: true},
		{name: "full path", cmd: `/opt/homebrew/bin/codex --enable tui_app_server --remote ws://127.0.0.1:4501`, want: true},
		{name: "missing remote", cmd: `codex --enable tui_app_server`, want: false},
		{name: "missing enable", cmd: `codex --remote ws://127.0.0.1:4501`, want: false},
		{name: "wrong enable value", cmd: `codex --enable other_feature --remote ws://127.0.0.1:4501`, want: false},
		{name: "wrong executable", cmd: `other --enable tui_app_server --remote ws://127.0.0.1:4501`, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isManagedCodexCommand(tt.cmd); got != tt.want {
				t.Fatalf("isManagedCodexCommand(%q) = %v, want %v", tt.cmd, got, tt.want)
			}
		})
	}
}

type fakeKillLifecycle struct {
	killedFile string
	calls      []string
}

func (f *fakeKillLifecycle) WriteKilled() error {
	f.calls = append(f.calls, "write_killed")
	return os.WriteFile(f.killedFile, []byte(time.Now().Format(time.RFC3339)), 0o644)
}

func (f *fakeKillLifecycle) Kill(gracefulTimeout time.Duration) (bool, error) {
	f.calls = append(f.calls, "kill")
	if gracefulTimeout != 3*time.Second {
		return false, errString("unexpected graceful timeout")
	}
	if _, err := os.Stat(f.killedFile); err != nil {
		return false, err
	}
	return true, nil
}

func TestWriteKilledAndStopDaemon_WritesSentinelBeforeKill(t *testing.T) {
	fake := &fakeKillLifecycle{killedFile: filepath.Join(t.TempDir(), "killed")}

	killed, err := writeKilledAndStopDaemon(fake, 3*time.Second)
	if err != nil {
		t.Fatalf("writeKilledAndStopDaemon() error = %v", err)
	}
	if !killed {
		t.Fatal("writeKilledAndStopDaemon() = false, want true")
	}
	want := []string{"write_killed", "kill"}
	if len(fake.calls) != len(want) {
		t.Fatalf("calls = %#v, want %#v", fake.calls, want)
	}
	for i, call := range want {
		if fake.calls[i] != call {
			t.Fatalf("calls = %#v, want %#v", fake.calls, want)
		}
	}
}

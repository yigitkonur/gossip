package main

import "testing"

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

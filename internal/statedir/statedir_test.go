package statedir

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStateDir_EnsureCreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sub", "gossip")
	s := New(dir)
	if err := s.Ensure(); err != nil {
		t.Fatalf("Ensure: %v", err)
	}
	info, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if !info.IsDir() {
		t.Errorf("not a dir")
	}
}

func TestStateDir_PathsAreUnderDir(t *testing.T) {
	dir := t.TempDir()
	s := New(dir)
	paths := []string{s.PidFile(), s.LogFile(), s.StatusFile(), s.PortsFile(), s.LockFile(), s.KilledFile(), s.TuiPidFile()}
	for _, p := range paths {
		rel, err := filepath.Rel(dir, p)
		if err != nil || rel == "" || rel[0] == '.' {
			t.Errorf("path %q is not under %q", p, dir)
		}
	}
}

func TestStateDir_UsesAgentBridgeAliasWhenPrimaryUnset(t *testing.T) {
	t.Setenv("GOSSIP_STATE_DIR", "")
	t.Setenv("AGENTBRIDGE_STATE_DIR", filepath.Join(t.TempDir(), "agentbridge-state"))
	if got := New("").Dir(); got != os.Getenv("AGENTBRIDGE_STATE_DIR") {
		t.Fatalf("New(\"\").Dir() = %q, want AGENTBRIDGE_STATE_DIR %q", got, os.Getenv("AGENTBRIDGE_STATE_DIR"))
	}
}

func TestStateDir_PrimaryEnvWinsOverAlias(t *testing.T) {
	primary := filepath.Join(t.TempDir(), "gossip-state")
	alias := filepath.Join(t.TempDir(), "agentbridge-state")
	t.Setenv("GOSSIP_STATE_DIR", primary)
	t.Setenv("AGENTBRIDGE_STATE_DIR", alias)
	if got := New("").Dir(); got != primary {
		t.Fatalf("New(\"\").Dir() = %q, want GOSSIP_STATE_DIR %q", got, primary)
	}
}

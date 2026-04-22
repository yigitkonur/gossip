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
	paths := []string{
		s.PidFile(), s.LogFile(), s.StatusFile(), s.PortsFile(),
		s.LockFile(), s.KilledFile(), s.TuiPidFile(),
		s.LoopStateFile(), s.OutboundQueueFile(),
	}
	for _, p := range paths {
		rel, err := filepath.Rel(dir, p)
		if err != nil || rel == "" || rel[0] == '.' {
			t.Errorf("path %q is not under %q", p, dir)
		}
	}
}

func TestStateDir_UsesGossipEnvOverride(t *testing.T) {
	override := filepath.Join(t.TempDir(), "gossip-state")
	t.Setenv("GOSSIP_STATE_DIR", override)
	if got := New("").Dir(); got != override {
		t.Fatalf("New(\"\").Dir() = %q, want GOSSIP_STATE_DIR %q", got, override)
	}
}

func TestStateDir_ExplicitOverrideWinsOverEnv(t *testing.T) {
	explicit := filepath.Join(t.TempDir(), "explicit")
	t.Setenv("GOSSIP_STATE_DIR", filepath.Join(t.TempDir(), "env"))
	if got := New(explicit).Dir(); got != explicit {
		t.Fatalf("New(%q).Dir() = %q, want explicit override", explicit, got)
	}
}

package statedir

import (
	"os"
	"path/filepath"
	"testing"
)

func TestStateDir_EnsureCreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "sub", "agentbridge")
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
	paths := []string{s.PidFile(), s.LogFile(), s.StatusFile(), s.LockFile(), s.KilledFile(), s.TuiPidFile()}
	for _, p := range paths {
		rel, err := filepath.Rel(dir, p)
		if err != nil || rel == "" || rel[0] == '.' {
			t.Errorf("path %q is not under %q", p, dir)
		}
	}
}

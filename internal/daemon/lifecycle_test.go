package daemon

import (
	"os"
	"testing"

	"github.com/yigitkonur/gossip/internal/statedir"
)

func TestLifecycle_KilledSentinel(t *testing.T) {
	sd := statedir.New(t.TempDir())
	_ = sd.Ensure()
	l := NewLifecycle(LifecycleOptions{StateDir: sd, ControlPort: 4502})

	if l.WasKilled() {
		t.Fatal("should not be killed initially")
	}
	if err := l.WriteKilled(); err != nil {
		t.Fatal(err)
	}
	if !l.WasKilled() {
		t.Error("should be killed after WriteKilled")
	}
	l.ClearKilled()
	if l.WasKilled() {
		t.Error("should be cleared")
	}
}

func TestLifecycle_PidFile(t *testing.T) {
	sd := statedir.New(t.TempDir())
	_ = sd.Ensure()
	l := NewLifecycle(LifecycleOptions{StateDir: sd, ControlPort: 4502})

	if l.ReadPid() != 0 {
		t.Error("ReadPid on missing file should return 0")
	}
	if err := l.WritePid(); err != nil {
		t.Fatal(err)
	}
	if l.ReadPid() != os.Getpid() {
		t.Errorf("ReadPid = %d, want %d", l.ReadPid(), os.Getpid())
	}
	l.RemovePid()
	if l.ReadPid() != 0 {
		t.Error("ReadPid after RemovePid should be 0")
	}
}

func TestLifecycle_AcquireLock_RemovesStaleLock(t *testing.T) {
	sd := statedir.New(t.TempDir())
	_ = sd.Ensure()
	l := NewLifecycle(LifecycleOptions{StateDir: sd, ControlPort: 4502})
	if err := os.WriteFile(sd.LockFile(), []byte("999999\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !l.acquireLock() {
		t.Fatal("expected acquireLock to recover stale lock")
	}
	l.releaseLock()
}

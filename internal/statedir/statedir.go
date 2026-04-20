// Package statedir resolves the shared runtime state directory for Gossip.
package statedir

import (
	"os"
	"path/filepath"
	"runtime"
)

// StateDir wraps a resolved directory.
type StateDir struct {
	dir string
}

// New resolves the state directory, honoring GOSSIP_STATE_DIR and XDG.
func New(override string) *StateDir {
	var dir string
	if override != "" {
		dir = override
	} else if envOverride := os.Getenv("GOSSIP_STATE_DIR"); envOverride != "" {
		dir = envOverride
	} else {
		home, err := os.UserHomeDir()
		if err != nil {
			home = os.TempDir()
		}
		if runtime.GOOS == "darwin" {
			dir = filepath.Join(home, "Library", "Application Support", "Gossip")
		} else {
			xdg := os.Getenv("XDG_STATE_HOME")
			if xdg == "" {
				xdg = filepath.Join(home, ".local", "state")
			}
			dir = filepath.Join(xdg, "gossip")
		}
	}
	return &StateDir{dir: dir}
}

// Ensure creates the directory if needed.
func (s *StateDir) Ensure() error { return os.MkdirAll(s.dir, 0o755) }

// Dir returns the resolved path.
func (s *StateDir) Dir() string { return s.dir }

// PidFile returns the daemon PID file path.
func (s *StateDir) PidFile() string { return filepath.Join(s.dir, "daemon.pid") }

// LockFile returns the startup lock file path.
func (s *StateDir) LockFile() string { return filepath.Join(s.dir, "daemon.lock") }

// StatusFile returns the daemon status JSON file path.
func (s *StateDir) StatusFile() string { return filepath.Join(s.dir, "status.json") }

// LogFile returns the combined log file path.
func (s *StateDir) LogFile() string { return filepath.Join(s.dir, "gossip.log") }

// KilledFile returns the sentinel indicating intentional shutdown.
func (s *StateDir) KilledFile() string { return filepath.Join(s.dir, "killed") }

// TuiPidFile returns the managed TUI PID file path.
func (s *StateDir) TuiPidFile() string { return filepath.Join(s.dir, "codex-tui.pid") }

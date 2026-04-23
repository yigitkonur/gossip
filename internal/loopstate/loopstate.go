// Package loopstate persists the completion-loop bookkeeping used by
// `gossip hook` handlers. Every round of the [COMPLETION]/[COMPLETED] review
// loop increments Iteration; the hook handlers serialize their read-modify-
// write cycles across hook runs (and across the Claude + Codex processes on
// the same machine) via a file-based advisory lock.
//
// The state is keyed by Claude session ID — when a fresh session starts, the
// SessionStart hook Resets the file so a stale counter from yesterday does
// not leak into today's work.
package loopstate

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"
)

// State captures one running completion loop.
type State struct {
	SessionID        string    `json:"sessionId"`
	Iteration        int       `json:"iteration"`
	MaxIterations    int       `json:"maxIterations"`
	StartedAt        time.Time `json:"startedAt"`
	LastCodexReplyID string    `json:"lastCodexReplyId,omitempty"`
	TerminatedReason string    `json:"terminatedReason,omitempty"`
}

// Reset returns a fresh State bound to sessionID with Iteration=0.
func Reset(sessionID string, maxIterations int) State {
	return State{
		SessionID:     sessionID,
		Iteration:     0,
		MaxIterations: maxIterations,
		StartedAt:     time.Now().UTC(),
	}
}

// Load reads state from path. If the file is missing or empty the zero
// State is returned with a nil error; callers decide whether to treat that
// as "no active loop" or to seed via Reset.
func Load(path string) (State, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return State{}, nil
		}
		return State{}, err
	}
	if len(b) == 0 {
		return State{}, nil
	}
	var s State
	if err := json.Unmarshal(b, &s); err != nil {
		return State{}, fmt.Errorf("parse %s: %w", path, err)
	}
	return s, nil
}

// Save writes state to path atomically (temp + rename) so partial writes
// never leave a readable-but-corrupt file on disk.
func Save(path string, s State) error {
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// WithLock acquires an advisory exclusive flock on `<path>.lock`, loads the
// current state, invokes fn, and (on nil error) saves the mutated state
// before releasing the lock. Safe to call concurrently from multiple
// processes on darwin/linux — flock serializes them per inode.
//
// If fn returns an error the state file is NOT updated.
func WithLock(path string, fn func(*State) error) error {
	lockPath := path + ".lock"
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o644)
	if err != nil {
		return fmt.Errorf("open lock %s: %w", lockPath, err)
	}
	defer f.Close()
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX); err != nil {
		return fmt.Errorf("flock %s: %w", lockPath, err)
	}
	defer syscall.Flock(int(f.Fd()), syscall.LOCK_UN)

	st, err := Load(path)
	if err != nil {
		return err
	}
	if err := fn(&st); err != nil {
		return err
	}
	return Save(path, st)
}

// Package tui models the Codex TUI connection lifecycle with a grace window.
package tui

import (
	"sync"
	"time"
)

// Options configures a State.
type Options struct {
	DisconnectGrace        time.Duration
	Logger                 func(msg string)
	OnDisconnectPersisted  func(connID int64)
	OnReconnectAfterNotice func(connID int64)
}

// State is the TUI connection state machine.
type State struct {
	opts Options

	mu                 sync.Mutex
	bridgeReady        bool
	tuiConnected       bool
	disconnectNotified bool
	disconnectTimer    *time.Timer
}

// NewState returns a new state machine.
func NewState(opts Options) *State {
	if opts.DisconnectGrace == 0 {
		opts.DisconnectGrace = 2500 * time.Millisecond
	}
	return &State{opts: opts}
}

// CanReply reports whether Claude-to-Codex replies are currently allowed.
func (s *State) CanReply() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.bridgeReady {
		return false
	}
	return s.tuiConnected || s.disconnectTimer != nil
}

// MarkBridgeReady is called when the Codex thread is fully initialized.
func (s *State) MarkBridgeReady() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bridgeReady = true
	s.disconnectNotified = false
	s.cancelTimerLocked()
}

// HandleTUIConnected is called when a TUI opens a WS connection.
func (s *State) HandleTUIConnected(connID int64) {
	s.mu.Lock()
	reconnecting := s.disconnectNotified && s.bridgeReady
	s.tuiConnected = true
	s.cancelTimerLocked()
	if reconnecting {
		s.disconnectNotified = false
	}
	cb := s.opts.OnReconnectAfterNotice
	s.mu.Unlock()
	if reconnecting && cb != nil {
		cb(connID)
	}
}

// HandleTUIDisconnected is called when a TUI closes a connection.
func (s *State) HandleTUIDisconnected(connID int64) {
	s.mu.Lock()
	s.tuiConnected = false
	if !s.bridgeReady {
		s.mu.Unlock()
		return
	}
	s.cancelTimerLocked()
	s.disconnectTimer = time.AfterFunc(s.opts.DisconnectGrace, func() {
		s.mu.Lock()
		if s.tuiConnected {
			s.mu.Unlock()
			return
		}
		s.disconnectNotified = true
		s.disconnectTimer = nil
		cb := s.opts.OnDisconnectPersisted
		s.mu.Unlock()
		if cb != nil {
			cb(connID)
		}
	})
	s.mu.Unlock()
}

// HandleCodexExit resets state when the codex process terminates.
func (s *State) HandleCodexExit() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.bridgeReady = false
	s.tuiConnected = false
	s.disconnectNotified = false
	s.cancelTimerLocked()
}

func (s *State) cancelTimerLocked() {
	if s.disconnectTimer != nil {
		s.disconnectTimer.Stop()
		s.disconnectTimer = nil
	}
}

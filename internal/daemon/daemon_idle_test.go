package daemon

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/raysonmeng/agent-bridge/internal/filter"
)

func TestDaemon_IdleShutdownTriggersWhenNoClientsRemain(t *testing.T) {
	d := New(Options{IdleShutdown: 20 * time.Millisecond, FilterMode: filter.ModeFiltered})
	var cancelled atomic.Bool
	d.runCancel = func() { cancelled.Store(true) }
	d.scheduleIdleShutdown()
	time.Sleep(60 * time.Millisecond)
	if !cancelled.Load() {
		t.Fatal("idle shutdown did not trigger")
	}
}

func TestDaemon_IdleShutdownCancelledOnClaudeAttach(t *testing.T) {
	d := New(Options{IdleShutdown: 50 * time.Millisecond, FilterMode: filter.ModeFiltered})
	var cancelled atomic.Bool
	d.runCancel = func() { cancelled.Store(true) }
	d.scheduleIdleShutdown()
	d.OnClaudeConnect()
	time.Sleep(80 * time.Millisecond)
	if cancelled.Load() {
		t.Fatal("idle shutdown fired despite attached Claude")
	}
}

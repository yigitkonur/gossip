package daemon

import (
	"sync/atomic"
	"testing"
	"time"

	"github.com/yigitkonur/gossip/internal/codex"
	"github.com/yigitkonur/gossip/internal/filter"
)

type fakeTimer struct {
	stopped atomic.Bool
	cb      func()
}

func (t *fakeTimer) Stop() bool {
	return !t.stopped.Swap(true)
}

func (t *fakeTimer) Fire() {
	if t.stopped.Load() {
		return
	}
	t.cb()
}

type fakeAfterFuncs struct {
	timers []*fakeTimer
}

func (f *fakeAfterFuncs) AfterFunc(_ time.Duration, cb func()) stopTimer {
	timer := &fakeTimer{cb: cb}
	f.timers = append(f.timers, timer)
	return timer
}

func TestDaemon_IdleShutdownCancelledOnClaudeAttach(t *testing.T) {
	d := New(Options{IdleShutdown: 30 * time.Second, FilterMode: filter.ModeFiltered})
	factory := &fakeAfterFuncs{}
	d.afterFunc = factory.AfterFunc
	var cancelled atomic.Bool
	d.runCancel = func() { cancelled.Store(true) }

	d.scheduleIdleShutdown()
	if len(factory.timers) != 1 {
		t.Fatalf("len(factory.timers) = %d, want 1", len(factory.timers))
	}
	d.OnClaudeConnect()
	factory.timers[0].Fire()
	if cancelled.Load() {
		t.Fatal("idle shutdown fired despite attached Claude")
	}
}

func TestDaemon_IdleShutdown_IgnoresStaleTimerAfterReschedule(t *testing.T) {
	d := New(Options{IdleShutdown: 30 * time.Second, FilterMode: filter.ModeFiltered})
	factory := &fakeAfterFuncs{}
	d.afterFunc = factory.AfterFunc
	var cancelled atomic.Bool
	d.runCancel = func() { cancelled.Store(true) }

	d.scheduleIdleShutdown()
	d.scheduleIdleShutdown()
	if len(factory.timers) != 2 {
		t.Fatalf("len(factory.timers) = %d, want 2", len(factory.timers))
	}
	factory.timers[0].Fire()
	if cancelled.Load() {
		t.Fatal("stale idle-shutdown timer should not cancel daemon")
	}
	factory.timers[1].Fire()
	if !cancelled.Load() {
		t.Fatal("latest idle-shutdown timer did not cancel daemon")
	}
}

func TestDaemon_ClaudeDisconnectNotification_IgnoresStaleTimerAfterReconnect(t *testing.T) {
	d := New(Options{FilterMode: filter.ModeFiltered})
	factory := &fakeAfterFuncs{}
	d.afterFunc = factory.AfterFunc
	d.codex = codex.NewClient(codex.ClientOptions{})
	d.tuiState.MarkBridgeReady()
	d.tuiState.HandleTUIConnected(1)
	d.claudeOnlineNoticeSent = true

	d.scheduleClaudeDisconnectNotification()
	if len(factory.timers) != 1 {
		t.Fatalf("len(factory.timers) = %d, want 1", len(factory.timers))
	}
	d.OnClaudeConnect()
	factory.timers[0].Fire()
	if d.claudeOfflineNoticeShown {
		t.Fatal("stale Claude disconnect timer should not mark offline notice shown")
	}
}

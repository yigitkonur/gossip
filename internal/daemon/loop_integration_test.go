package daemon

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yigitkonur/gossip/internal/codex"
	"github.com/yigitkonur/gossip/internal/control"
	"github.com/yigitkonur/gossip/internal/filter"
	"github.com/yigitkonur/gossip/internal/protocol"
)

// Build a minimally-wired Daemon for loop-integration tests: control
// server + statusBuf + a loopQueue whose sender is a recorder. Bridge is
// reported as ready by default so pump runs immediately on Enqueue.
type loopTestRig struct {
	d       *Daemon
	sends   atomic.Int32
	lastTxt atomic.Value // string
}

func newLoopRig(t *testing.T, bridgeReady bool) *loopTestRig {
	t.Helper()
	d := New(Options{FilterMode: filter.ModeFiltered})
	d.control = control.NewServer(d)
	d.statusBuf = filter.NewStatusBuffer(func(protocol.BridgeMessage) {}, filter.StatusBufferOptions{FlushTimeout: time.Hour})
	rig := &loopTestRig{d: d}
	rig.lastTxt.Store("")
	if bridgeReady {
		d.tuiState.HandleTUIConnected(1)
		d.tuiState.MarkBridgeReady()
	}
	d.loopQueue = NewLoopQueue(
		func(ctx context.Context, text string, requireReply bool) (bool, string) {
			rig.sends.Add(1)
			rig.lastTxt.Store(text)
			return true, ""
		},
		func() bool { return d.tuiState.CanReply() },
		nil,
	)
	return rig
}

func TestDaemon_AgentMessageImportantResolvesLoopQueue(t *testing.T) {
	rig := newLoopRig(t, true)
	d := rig.d

	// Start a blocking send; sender should be invoked once.
	replyCh := make(chan BlockingReply, 1)
	go func() {
		text, received, errMsg := d.OnClaudeToCodexBlocking(
			context.Background(),
			protocol.BridgeMessage{ID: "m1", Source: protocol.SourceClaude, Content: "do this"},
			true, 3_000,
		)
		replyCh <- BlockingReply{Text: text, Received: received, Error: errMsg}
	}()

	// Wait for the sender to fire before firing the event so we're
	// correlating against an active blocking send.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) && rig.sends.Load() == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if rig.sends.Load() != 1 {
		t.Fatalf("sender not invoked: sends=%d", rig.sends.Load())
	}

	// Fire an [IMPORTANT] agentMessage — should resolve.
	d.replyRequired = true
	d.handleCodexEvent(context.Background(), codex.Event{
		Kind:     codex.EventAgentMessage,
		Text:     "[IMPORTANT] approved [COMPLETED]",
		ThreadID: "t",
		TurnID:   "turn_1",
	})

	select {
	case r := <-replyCh:
		if !r.Received {
			t.Fatalf("Received = false: %+v", r)
		}
		if !strings.Contains(r.Text, "[COMPLETED]") {
			t.Errorf("reply text missing approval marker: %q", r.Text)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("blocking send did not resolve on [IMPORTANT] event")
	}
}

func TestDaemon_AgentMessageNonImportantDoesNotResolveLoopQueue(t *testing.T) {
	rig := newLoopRig(t, true)
	d := rig.d

	replyCh := make(chan BlockingReply, 1)
	go func() {
		text, received, errMsg := d.OnClaudeToCodexBlocking(
			context.Background(),
			protocol.BridgeMessage{ID: "m1", Source: protocol.SourceClaude, Content: "do this"},
			true, 200, // short timeout
		)
		replyCh <- BlockingReply{Text: text, Received: received, Error: errMsg}
	}()

	// Wait for sender.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) && rig.sends.Load() == 0 {
		time.Sleep(10 * time.Millisecond)
	}

	// Fire a [STATUS] message (non-[IMPORTANT]) — should NOT resolve loop.
	d.handleCodexEvent(context.Background(), codex.Event{
		Kind:     codex.EventAgentMessage,
		Text:     "[STATUS] still working",
		ThreadID: "t",
		TurnID:   "turn_1",
	})

	select {
	case r := <-replyCh:
		// Resolved via timeout, not via the status message. Confirm.
		if r.Received {
			t.Fatalf("[STATUS] unexpectedly resolved blocking send: %+v", r)
		}
		if !strings.Contains(r.Error, "timed out") {
			t.Errorf("expected timeout reason, got %q", r.Error)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("blocking send hung past timeout — resolver never fired")
	}
}

func TestDaemon_TurnCompletedWithoutReplyResolvesLoopQueue(t *testing.T) {
	rig := newLoopRig(t, true)
	d := rig.d

	replyCh := make(chan BlockingReply, 1)
	go func() {
		text, received, errMsg := d.OnClaudeToCodexBlocking(
			context.Background(),
			protocol.BridgeMessage{ID: "m1", Source: protocol.SourceClaude, Content: "do this"},
			true, 5_000,
		)
		replyCh <- BlockingReply{Text: text, Received: received, Error: errMsg}
	}()

	// Wait for sender to fire.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) && rig.sends.Load() == 0 {
		time.Sleep(10 * time.Millisecond)
	}

	// Simulate a turn that completed with replyRequired but no agentMessage.
	d.replyRequired = true
	d.replyReceivedDuringTurn = false
	d.handleCodexEvent(context.Background(), codex.Event{Kind: codex.EventTurnCompleted, ThreadID: "t", TurnID: "turn_1"})

	select {
	case r := <-replyCh:
		if r.Received {
			t.Fatalf("Received = true, want false on turn-completed-without-reply")
		}
		if !strings.Contains(r.Error, "turn completed") {
			t.Errorf("Error should mention turn completed: %q", r.Error)
		}
	case <-time.After(500 * time.Millisecond):
		t.Fatal("blocking send did not resolve on turn-completed-without-reply")
	}
}

func TestDaemon_ThreadReadyDrainsLoopQueue(t *testing.T) {
	rig := newLoopRig(t, false) // bridge NOT ready initially
	d := rig.d

	// Enqueue while bridge is not ready — sender should not be invoked yet.
	replyCh := make(chan BlockingReply, 1)
	go func() {
		text, received, errMsg := d.OnClaudeToCodexBlocking(
			context.Background(),
			protocol.BridgeMessage{ID: "m1", Source: protocol.SourceClaude, Content: "do this"},
			true, 5_000,
		)
		replyCh <- BlockingReply{Text: text, Received: received, Error: errMsg}
	}()
	time.Sleep(50 * time.Millisecond)
	if rig.sends.Load() != 0 {
		t.Fatalf("sender invoked before bridge ready: %d", rig.sends.Load())
	}

	// Mark TUI state ready THEN fire EventThreadReady → drain.
	d.tuiState.HandleTUIConnected(1)
	d.tuiState.MarkBridgeReady()
	d.handleCodexEvent(context.Background(), codex.Event{Kind: codex.EventThreadReady, ThreadID: "t"})

	// Give pump a chance to fire.
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) && rig.sends.Load() == 0 {
		time.Sleep(10 * time.Millisecond)
	}
	if rig.sends.Load() != 1 {
		t.Fatalf("drain did not invoke sender: sends=%d", rig.sends.Load())
	}

	// Clean up the hanging blocking goroutine.
	d.loopQueue.OnAgentMessage("[IMPORTANT] done [COMPLETED]")
	<-replyCh
}

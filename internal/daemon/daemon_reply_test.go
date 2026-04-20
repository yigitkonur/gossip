package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/yigitkonur/gossip/internal/codex"
	"github.com/yigitkonur/gossip/internal/control"
	"github.com/yigitkonur/gossip/internal/filter"
	"github.com/yigitkonur/gossip/internal/protocol"
)

func TestDaemon_OnClaudeToCodex_RejectsWhenNotReady(t *testing.T) {
	d := New(Options{FilterMode: filter.ModeFiltered})
	ok, reason := d.OnClaudeToCodex(context.Background(), protocol.BridgeMessage{Content: "hello"}, false)
	if ok {
		t.Fatal("expected reply to be rejected when bridge is not ready")
	}
	if reason == "" {
		t.Fatal("expected rejection reason")
	}
}

func TestDaemon_ReplyRequired_ForcesForwardAndClearsRequirement(t *testing.T) {
	d := New(Options{FilterMode: filter.ModeFiltered})
	d.control = control.NewServer(d)
	d.statusBuf = filter.NewStatusBuffer(func(protocol.BridgeMessage) {}, filter.StatusBufferOptions{FlushTimeout: time.Hour})

	d.replyRequired = true
	d.replyReceivedDuringTurn = false

	d.handleCodexEvent(context.Background(), codex.Event{Kind: codex.EventAgentMessage, Text: "[FYI] background", ThreadID: "t", TurnID: "turn_1"})
	if d.control.QueuedCount() != 1 {
		t.Fatalf("expected reply-required FYI message to force-forward, queued=%d", d.control.QueuedCount())
	}
	d.handleCodexEvent(context.Background(), codex.Event{Kind: codex.EventTurnCompleted, ThreadID: "t", TurnID: "turn_1"})
	if d.replyRequired {
		t.Fatal("replyRequired should reset after turn completion")
	}
	if d.control.QueuedCount() != 2 {
		t.Fatalf("expected forwarded reply plus turn completion notice, queued=%d", d.control.QueuedCount())
	}
}

func TestDaemon_ReplyRequired_EmitsMissingReplyNoticeWhenTurnCompletesSilent(t *testing.T) {
	d := New(Options{FilterMode: filter.ModeFiltered})
	d.control = control.NewServer(d)
	d.statusBuf = filter.NewStatusBuffer(func(protocol.BridgeMessage) {}, filter.StatusBufferOptions{FlushTimeout: time.Hour})
	d.replyRequired = true
	d.replyReceivedDuringTurn = false

	d.handleCodexEvent(context.Background(), codex.Event{Kind: codex.EventTurnCompleted, ThreadID: "t", TurnID: "turn_1"})
	if d.replyRequired {
		t.Fatal("replyRequired should reset after turn completion")
	}
	if d.control.QueuedCount() != 2 {
		t.Fatalf("expected missing-reply warning plus turn completion notice, queued=%d", d.control.QueuedCount())
	}
}

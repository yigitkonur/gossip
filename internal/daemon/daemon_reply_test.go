package daemon

import (
	"context"
	"testing"
	"time"

	"github.com/raysonmeng/agent-bridge/internal/codex"
	"github.com/raysonmeng/agent-bridge/internal/control"
	"github.com/raysonmeng/agent-bridge/internal/filter"
	"github.com/raysonmeng/agent-bridge/internal/protocol"
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

func TestDaemon_ReplyRequired_ForcesForwardAndMissingReplyNotice(t *testing.T) {
	d := New(Options{FilterMode: filter.ModeFiltered})
	d.control = control.NewServer(d)
	d.statusBuf = filter.NewStatusBuffer(func(protocol.BridgeMessage) {}, filter.StatusBufferOptions{FlushTimeout: time.Hour})
	d.replyRequired = true
	d.replyReceivedDuringTurn = false

	d.handleCodexEvent(context.Background(), codex.Event{Kind: codex.EventAgentMessage, Text: "[FYI] background", ThreadID: "t", TurnID: "turn_1"})
	if d.control.QueuedCount() != 1 {
		t.Fatalf("expected forced-forwarded message to be buffered for bridge, got queued=%d", d.control.QueuedCount())
	}

	d.handleCodexEvent(context.Background(), codex.Event{Kind: codex.EventTurnCompleted, ThreadID: "t", TurnID: "turn_1"})
	if d.replyRequired {
		t.Fatal("replyRequired should reset after turn completion")
	}
}

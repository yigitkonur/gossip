package codex

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/yigitkonur/gossip/internal/jsonrpc"
	"github.com/yigitkonur/gossip/internal/protocol"
)

func TestClient_DispatchNotification_AgentMessageBuffering(t *testing.T) {
	c := NewClient(ClientOptions{})
	c.threadID.Store("thread_1")

	delta := func(text string) jsonrpc.Notification {
		params := protocol.AgentMessageDeltaParams{
			ThreadID: "thread_1",
			TurnID:   "turn_1",
			ItemID:   "item_1",
			Delta:    text,
		}
		b, _ := json.Marshal(params)
		return jsonrpc.Notification{Method: protocol.MethodItemAgentMessageDelta, Params: b}
	}

	c.dispatchNotification(delta("Hello "))
	c.dispatchNotification(delta("world"))

	completed := protocol.ItemCompletedParams{
		ThreadID: "thread_1",
		TurnID:   "turn_1",
		Item:     protocol.Item{ID: "item_1"},
	}
	b, _ := json.Marshal(completed)
	c.dispatchNotification(jsonrpc.Notification{Method: protocol.MethodItemCompleted, Params: b})

	select {
	case ev := <-c.Events():
		if ev.Kind != EventAgentMessage {
			t.Fatalf("Kind = %v", ev.Kind)
		}
		if ev.Text != "Hello world" {
			t.Fatalf("Text = %q", ev.Text)
		}
	default:
		t.Fatal("no EventAgentMessage emitted")
	}
}

func TestClient_DispatchNotification_UsesCompletedItemContentFallback(t *testing.T) {
	c := NewClient(ClientOptions{})
	c.threadID.Store("thread_1")

	completed := protocol.ItemCompletedParams{
		ThreadID: "thread_1",
		TurnID:   "turn_1",
		Item: protocol.Item{
			ID:      "item_1",
			Type:    "agentMessage",
			Content: []protocol.ItemContent{{Type: "text", Text: "fallback content"}},
		},
	}
	b, _ := json.Marshal(completed)
	c.dispatchNotification(jsonrpc.Notification{Method: protocol.MethodItemCompleted, Params: b})

	select {
	case ev := <-c.Events():
		if ev.Kind != EventAgentMessage {
			t.Fatalf("Kind = %v", ev.Kind)
		}
		if ev.Text != "fallback content" {
			t.Fatalf("Text = %q", ev.Text)
		}
	default:
		t.Fatal("no EventAgentMessage emitted")
	}
}

func TestClient_TurnInProgress_TracksNestedTurns(t *testing.T) {
	c := NewClient(ClientOptions{})
	started1, _ := json.Marshal(protocol.TurnStartedParams{ThreadID: "thread_1", TurnID: "turn_1"})
	started2, _ := json.Marshal(protocol.TurnStartedParams{ThreadID: "thread_1", TurnID: "turn_2"})
	completed1, _ := json.Marshal(protocol.TurnCompletedParams{ThreadID: "thread_1", TurnID: "turn_1"})
	completed2, _ := json.Marshal(protocol.TurnCompletedParams{ThreadID: "thread_1", TurnID: "turn_2"})

	c.dispatchNotification(jsonrpc.Notification{Method: protocol.MethodTurnStarted, Params: started1})
	c.dispatchNotification(jsonrpc.Notification{Method: protocol.MethodTurnStarted, Params: started2})
	c.dispatchNotification(jsonrpc.Notification{Method: protocol.MethodTurnCompleted, Params: completed1})
	if !c.TurnInProgress() {
		t.Fatal("turn should still be in progress after only one nested turn completed")
	}
	c.dispatchNotification(jsonrpc.Notification{Method: protocol.MethodTurnCompleted, Params: completed2})
	if c.TurnInProgress() {
		t.Fatal("turn should not be in progress after all nested turns complete")
	}
}

func TestClient_WatchProcessExit_ClearsReadinessState(t *testing.T) {
	c := NewClient(ClientOptions{})
	c.threadID.Store("thread_1")
	c.turnInProgress.Store(true)
	c.turnMu.Lock()
	c.activeTurnIDs["turn_1"] = struct{}{}
	c.turnMu.Unlock()
	c.agentMessageMu.Lock()
	c.agentMessageBufs["turn_1_item_1"] = &strings.Builder{}
	c.agentMessageBufs["turn_1_item_1"].WriteString("partial")
	c.agentMessageMu.Unlock()
	c.proc = &Process{done: make(chan struct{})}

	done := make(chan struct{})
	go func() {
		c.watchProcessExit()
		close(done)
	}()
	close(c.proc.done)

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("watchProcessExit did not return")
	}

	if got := c.ActiveThreadID(); got != "" {
		t.Fatalf("ActiveThreadID() = %q, want empty", got)
	}
	if c.TurnInProgress() {
		t.Fatal("TurnInProgress() should be false after process exit")
	}
	c.agentMessageMu.Lock()
	defer c.agentMessageMu.Unlock()
	if len(c.agentMessageBufs) != 0 {
		t.Fatalf("agent message buffers were not cleared: %+v", c.agentMessageBufs)
	}
}
